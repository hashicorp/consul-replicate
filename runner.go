// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"sync"
	"time"

	"strings"

	"github.com/hashicorp/consul-template/config"
	dep "github.com/hashicorp/consul-template/dependency"
	"github.com/hashicorp/consul-template/watch"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-multierror"
)

// Regexp for invalid characters in keys
var InvalidRegexp = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// Status is an internal struct that is responsible for marshaling and
// unmarshaling JSON responses into keys.
type Status struct {
	// LastReplicated is the last time the replication occurred.
	LastReplicated uint64

	// Source and Destination are the given and final destination.
	Source, Destination string
}

type Runner struct {
	sync.RWMutex

	// // Prefix is the KeyPrefixDependency associated with this Runner.
	// Prefix *dependency.StoreKeyPrefix

	// ErrCh and DoneCh are channels where errors and finish notifications occur.
	ErrCh  chan error
	DoneCh chan struct{}

	// config is the Config that created this Runner. It is used internally to
	// construct other objects and pass data.
	config *Config

	// client is the consul/api client.
	clients *dep.ClientSet

	// data is the internal storage engine for this runner with the key being the
	// String() for the dependency and the result being the view that holds the
	// data.
	data map[string]*watch.View

	// once indicates the runner should get data exactly one time and then stop.
	once bool

	// minTimer and maxTimer are used for quiescence.
	minTimer, maxTimer <-chan time.Time

	// outStream and errStream are the io.Writer streams where the runner will
	// write information.
	outStream, errStream io.Writer

	// watcher is the watcher this runner is using.
	watcher *watch.Watcher
}

// NewRunner accepts a config, command, and boolean value for once mode.
func NewRunner(config *Config, once bool) (*Runner, error) {
	log.Printf("[INFO] (runner) creating new runner (once: %v)", once)

	runner := &Runner{
		config: config,
		once:   once,
	}

	if err := runner.init(); err != nil {
		return nil, err
	}

	return runner, nil
}

// Start creates a new runner and begins watching dependencies and quiescence
// timers. This is the main event loop and will block until finished.
func (r *Runner) Start() {
	log.Printf("[INFO] (runner) starting")

	// Create the pid before doing anything.
	if err := r.storePid(); err != nil {
		r.ErrCh <- err
		return
	}

	// Add the dependencies to the watcher
	for _, prefix := range *r.config.Prefixes {
		if _, err := r.watcher.Add(prefix.Dependency); err != nil {
			log.Printf("ERR (runner) failed to add watch: %v", err)
		}
	}

	// If once mode is on, wait until we get data back from all the views before proceeding
	onceCh := make(chan struct{}, 1)
	if r.once {
		for i := 0; i < len(*r.config.Prefixes); i++ {
			select {
			case view := <-r.watcher.DataCh():
				r.Receive(view)
			case err := <-r.watcher.ErrCh():
				r.ErrCh <- err
				return
			}
		}
		onceCh <- struct{}{}
	}

	for {
		select {
		case view := <-r.watcher.DataCh():
			r.Receive(view)

			// Drain all views that have data
		OUTER:
			for {
				select {
				case view = <-r.watcher.DataCh():
					r.Receive(view)
				default:
					break OUTER
				}
			}

			// If we are waiting for quiescence, setup the timers
			if *r.config.Wait.Min != 0 && *r.config.Wait.Max != 0 {
				log.Printf("[INFO] (runner) quiescence timers starting")
				r.minTimer = time.After(*r.config.Wait.Min)
				if r.maxTimer == nil {
					r.maxTimer = time.After(*r.config.Wait.Max)
				}
				continue
			}
		case <-r.minTimer:
			log.Printf("[INFO] (runner) quiescence minTimer fired")
			r.minTimer, r.maxTimer = nil, nil
		case <-r.maxTimer:
			log.Printf("[INFO] (runner) quiescence maxTimer fired")
			r.minTimer, r.maxTimer = nil, nil
		case err := <-r.watcher.ErrCh():
			log.Printf("[ERR] (runner) watcher reported error: %s", err)
			r.ErrCh <- err
		case <-r.DoneCh:
			log.Printf("[INFO] (runner) received finish")
			return
		case <-onceCh:
		}

		// If we got this far, that means we got new data or one of the timers
		// fired, so attempt to run.
		if err := r.Run(); err != nil {
			r.ErrCh <- err
			return
		}

		if r.once {
			log.Printf("[INFO] (runner) run finished and -once is set, exiting")
			r.DoneCh <- struct{}{}
			return
		}
	}
}

// Stop halts the execution of this runner and its subprocesses.
func (r *Runner) Stop() {
	log.Printf("[INFO] (runner) stopping")
	r.watcher.Stop()
	if err := r.deletePid(); err != nil {
		log.Printf("[WARN] (runner) could not remove pid at %q: %s",
			*r.config.PidFile, err)
	}
	close(r.DoneCh)
}

// Receive accepts data from Consul and maps that data to the prefix.
func (r *Runner) Receive(view *watch.View) {
	r.Lock()
	defer r.Unlock()
	r.data[view.Dependency().String()] = view
}

// Run invokes a single pass of the runner.
func (r *Runner) Run() error {
	log.Printf("[INFO] (runner) running")

	prefixes := *r.config.Prefixes
	doneCh := make(chan struct{}, len(prefixes))
	errCh := make(chan error, len(prefixes))

	// Replicate each prefix in a goroutine
	for _, prefix := range prefixes {
		go r.replicate(prefix, r.config.Excludes, doneCh, errCh)
	}

	var errs *multierror.Error
	for i := 0; i < len(prefixes); i++ {
		select {
		case <-doneCh:
			// OK
		case err := <-errCh:
			errs = multierror.Append(errs, err)
		}
	}

	return errs.ErrorOrNil()
}

// init creates the Runner's underlying data structures and returns an error if
// any problems occur.
func (r *Runner) init() error {
	// Ensure default configuration values
	r.config = DefaultConfig().Merge(r.config)
	r.config.Finalize()

	// Print the final config for debugging
	result, err := json.MarshalIndent(r.config, "", "  ")
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] (runner) final config (tokens suppressed):\n\n%s\n\n",
		result)

	// Create the client
	clients, err := newClientSet(r.config)
	if err != nil {
		return fmt.Errorf("runner: %s", err)
	}
	r.clients = clients

	// Create the watcher
	r.watcher = newWatcher(r.config, clients, r.once)

	r.data = make(map[string]*watch.View)

	r.outStream = os.Stdout
	r.errStream = os.Stderr

	r.ErrCh = make(chan error)
	r.DoneCh = make(chan struct{})

	return nil
}

// get returns the data for a particular view in the watcher.
func (r *Runner) get(prefix *PrefixConfig) (*watch.View, bool) {
	r.RLock()
	defer r.RUnlock()
	result, ok := r.data[prefix.Dependency.String()]
	return result, ok
}

// replicate performs replication into the current datacenter from the given
// prefix. This function is designed to be called via a goroutine since it is
// expensive and needs to be parallelized.
func (r *Runner) replicate(prefix *PrefixConfig, excludes *ExcludeConfigs, doneCh chan struct{}, errCh chan error) {
	// Ensure we are not self-replicating
	info, err := r.clients.Consul().Agent().Self()
	if err != nil {
		errCh <- fmt.Errorf("failed to query agent: %s", err)
		return
	}
	localDatacenter := info["Config"]["Datacenter"].(string)
	if localDatacenter == config.StringVal(prefix.Datacenter) {
		errCh <- fmt.Errorf("local datacenter cannot be the source datacenter")
		return
	}

	// Get the last status
	status, err := r.getStatus(prefix)
	if err != nil {
		errCh <- fmt.Errorf("failed to read replication status: %s", err)
		return
	}

	// Get the prefix data
	view, ok := r.get(prefix)
	if !ok {
		log.Printf("[INFO] (runner) no data for %q", prefix.Dependency)
		doneCh <- struct{}{}
		return
	}

	// Get the data from the view
	data, lastIndex := view.DataAndLastIndex()
	pairs, ok := data.([]*dep.KeyPair)
	if !ok {
		errCh <- fmt.Errorf("could not convert watch data")
		return
	}

	kv := r.clients.Consul().KV()

	// Update keys to the most recent versions
	updates := 0
	usedKeys := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		key := config.StringVal(prefix.Destination) +
			strings.TrimPrefix(pair.Path, config.StringVal(prefix.Source))
		usedKeys[key] = struct{}{}

		// Ignore if the key falls under an excluded prefix
		if len(*excludes) > 0 {
			excluded := false
			for _, exclude := range *excludes {
				if strings.HasPrefix(pair.Path, config.StringVal(exclude.Source)) {
					log.Printf("[DEBUG] (runner) key %q has prefix %q, excluding",
						pair.Path, config.StringVal(exclude.Source))
					excluded = true
				}
			}

			if excluded {
				continue
			}
		}

		// Ignore if the modify index is old
		if pair.ModifyIndex <= status.LastReplicated {
			log.Printf("[DEBUG] (runner) skipping because %q is already "+
				"replicated", key)
			continue
		}

		// Check if lock
		if pair.Flags == api.SemaphoreFlagValue {
			log.Printf("[WARN] (runner) lock in use at %q, but sessions cannot be "+
				"replicated across datacenters", key)
		}

		// Check if semaphore
		if pair.Flags == api.LockFlagValue {
			log.Printf("[WARN] (runner) semaphore in use at %q, but sessions cannot "+
				"be replicated across datacenters", key)
		}

		// Check if session attached
		if pair.Session != "" {
			log.Printf("[WARN] (runner) %q has attached session, but sessions "+
				"cannot be replicated across datacenters", key)
		}

		if _, err := kv.Put(&api.KVPair{
			Key:   key,
			Flags: pair.Flags,
			Value: []byte(pair.Value),
		}, nil); err != nil {
			errCh <- fmt.Errorf("failed to write %q: %s", key, err)
			return
		}
		log.Printf("[DEBUG] (runner) updated key %q", key)
		updates++
	}

	// Handle deletes
	deletes := 0
	localKeys, _, err := kv.Keys(config.StringVal(prefix.Destination), "", nil)
	if err != nil {
		errCh <- fmt.Errorf("failed to list keys: %s", err)
		return
	}
	for _, key := range localKeys {
		excluded := false

		// Ignore if the key falls under an excluded prefix
		if len(*excludes) > 0 {
			sourceKey := strings.Replace(key, config.StringVal(prefix.Destination), config.StringVal(prefix.Source), -1)
			for _, exclude := range *excludes {
				if strings.HasPrefix(sourceKey, config.StringVal(exclude.Source)) {
					log.Printf("[DEBUG] (runner) key %q has prefix %q, excluding from deletes",
						sourceKey, *exclude.Source)
					excluded = true
				}
			}
		}

		if _, ok := usedKeys[key]; !ok && !excluded {
			if _, err := kv.Delete(key, nil); err != nil {
				errCh <- fmt.Errorf("failed to delete %q: %s", key, err)
				return
			}
			log.Printf("[DEBUG] (runner) deleted %q", key)
			deletes++
		}
	}

	// Update our status
	status.LastReplicated = lastIndex
	status.Source = config.StringVal(prefix.Source)
	status.Destination = config.StringVal(prefix.Destination)
	if err := r.setStatus(prefix, status); err != nil {
		errCh <- fmt.Errorf("failed to checkpoint status: %s", err)
		return
	}

	if updates > 0 || deletes > 0 {
		log.Printf("[INFO] (runner) replicated %d updates, %d deletes", updates, deletes)
	}

	// We are done!
	doneCh <- struct{}{}
}

// getStatus is used to read the last replication status.
func (r *Runner) getStatus(prefix *PrefixConfig) (*Status, error) {
	kv := r.clients.Consul().KV()
	pair, _, err := kv.Get(r.statusPath(prefix), nil)
	if err != nil {
		return nil, err
	}

	status := &Status{}
	if pair != nil {
		if err := json.Unmarshal(pair.Value, &status); err != nil {
			return nil, err
		}
	}
	return status, nil
}

// setStatus is used to update the last replication status.
func (r *Runner) setStatus(prefix *PrefixConfig, status *Status) error {
	// Encode the JSON as pretty so operators can easily view it in the Consul UI.
	enc, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}

	// Put the key to Consul.
	kv := r.clients.Consul().KV()
	_, err = kv.Put(&api.KVPair{
		Key:   r.statusPath(prefix),
		Value: enc,
	}, nil)
	return err
}

func (r *Runner) statusPath(prefix *PrefixConfig) string {
	plain := fmt.Sprintf("%s-%s", config.StringVal(prefix.Source), config.StringVal(prefix.Destination))
	hash := md5.Sum([]byte(plain))
	enc := hex.EncodeToString(hash[:])
	return strings.TrimRight(config.StringVal(r.config.StatusDir), "/") + "/" + enc
}

// storePid is used to write out a PID file to disk.
func (r *Runner) storePid() error {
	path := config.StringVal(r.config.PidFile)
	if path == "" {
		return nil
	}

	log.Printf("[INFO] creating pid file at %q", path)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("runner: could not open pid file: %s", err)
	}
	defer f.Close()

	pid := os.Getpid()
	_, err = f.WriteString(fmt.Sprintf("%d", pid))
	if err != nil {
		return fmt.Errorf("runner: could not write to pid file: %s", err)
	}
	return nil
}

// deletePid is used to remove the PID on exit.
func (r *Runner) deletePid() error {
	path := config.StringVal(r.config.PidFile)
	if path == "" {
		return nil
	}

	log.Printf("[DEBUG] removing pid file at %q", path)

	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("runner: could not remove pid file: %s", err)
	}
	if stat.IsDir() {
		return fmt.Errorf("runner: specified pid file path is directory")
	}

	err = os.Remove(path)
	if err != nil {
		return fmt.Errorf("runner: could not remove pid file: %s", err)
	}
	return nil
}

// newClientSet creates a new client set from the given config.
func newClientSet(c *Config) (*dep.ClientSet, error) {
	clients := dep.NewClientSet()

	if err := clients.CreateConsulClient(&dep.CreateConsulClientInput{
		Address:                      config.StringVal(c.Consul.Address),
		Token:                        config.StringVal(c.Consul.Token),
		AuthEnabled:                  config.BoolVal(c.Consul.Auth.Enabled),
		AuthUsername:                 config.StringVal(c.Consul.Auth.Username),
		AuthPassword:                 config.StringVal(c.Consul.Auth.Password),
		SSLEnabled:                   config.BoolVal(c.Consul.SSL.Enabled),
		SSLVerify:                    config.BoolVal(c.Consul.SSL.Verify),
		SSLCert:                      config.StringVal(c.Consul.SSL.Cert),
		SSLKey:                       config.StringVal(c.Consul.SSL.Key),
		SSLCACert:                    config.StringVal(c.Consul.SSL.CaCert),
		SSLCAPath:                    config.StringVal(c.Consul.SSL.CaPath),
		ServerName:                   config.StringVal(c.Consul.SSL.ServerName),
		TransportDialKeepAlive:       config.TimeDurationVal(c.Consul.Transport.DialKeepAlive),
		TransportDialTimeout:         config.TimeDurationVal(c.Consul.Transport.DialTimeout),
		TransportDisableKeepAlives:   config.BoolVal(c.Consul.Transport.DisableKeepAlives),
		TransportIdleConnTimeout:     config.TimeDurationVal(c.Consul.Transport.IdleConnTimeout),
		TransportMaxIdleConns:        config.IntVal(c.Consul.Transport.MaxIdleConns),
		TransportMaxIdleConnsPerHost: config.IntVal(c.Consul.Transport.MaxIdleConnsPerHost),
		TransportTLSHandshakeTimeout: config.TimeDurationVal(c.Consul.Transport.TLSHandshakeTimeout),
	}); err != nil {
		return nil, fmt.Errorf("runner: %s", err)
	}

	return clients, nil
}

// newWatcher creates a new watcher.
func newWatcher(c *Config, clients *dep.ClientSet, once bool) *watch.Watcher {
	log.Printf("[INFO] (runner) creating watcher")

	return watch.NewWatcher(&watch.NewWatcherInput{
		Clients:          clients,
		MaxStale:         config.TimeDurationVal(c.MaxStale),
		Once:             once,
		RetryFuncConsul:  watch.RetryFunc(c.Consul.Retry.RetryFunc()),
		RetryFuncDefault: nil,
	})
}
