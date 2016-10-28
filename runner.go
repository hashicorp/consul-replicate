package main

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	dep "github.com/hashicorp/consul-template/dependency"
	"github.com/hashicorp/consul-template/watch"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-multierror"
	"strings"
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
	client *api.Client

	// data is the internal storage engine for this runner with the key being the
	// HashCode() for the dependency and the result being the view that holds the
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

	// configuration settings override the defaults
	mergedDefaultConfig := DefaultConfig()
	mergedDefaultConfig.Merge(config)

	runner := &Runner{
		config: mergedDefaultConfig,
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
	for _, prefix := range r.config.Prefixes {
		r.watcher.Add(prefix.Source)
	}

	for {
		select {
		case view := <-r.watcher.DataCh:
			r.Receive(view)

			// Drain all views that have data
		OUTER:
			for {
				select {
				case view = <-r.watcher.DataCh:
					r.Receive(view)
				default:
					break OUTER
				}
			}

			// If we are waiting for quiescence, setup the timers
			if r.config.Wait.Min != 0 && r.config.Wait.Max != 0 {
				log.Printf("[INFO] (runner) quiescence timers starting")
				r.minTimer = time.After(r.config.Wait.Min)
				if r.maxTimer == nil {
					r.maxTimer = time.After(r.config.Wait.Max)
				}
				continue
			}
		case <-r.minTimer:
			log.Printf("[INFO] (runner) quiescence minTimer fired")
			r.minTimer, r.maxTimer = nil, nil
		case <-r.maxTimer:
			log.Printf("[INFO] (runner) quiescence maxTimer fired")
			r.minTimer, r.maxTimer = nil, nil
		case err := <-r.watcher.ErrCh:
			// Intentionally do not send the error back up to the runner. Eventually,
			// once Consul API implements errwrap and multierror, we can check the
			// "type" of error and conditionally alert back.
			//
			// if err.Contains(Something) {
			//   errCh <- err
			// }
			log.Printf("[ERR] (runner) watcher reported error: %s", err)
		case <-r.DoneCh:
			log.Printf("[INFO] (runner) received finish")
			return
		}

		// If we got this far, that means we got new data or one of the timers
		// fired, so attempt to run.
		received, err := r.Run()
		if err != nil {
			r.ErrCh <- err
			return
		}
		if r.once && received == r.watcher.Size() {
			log.Printf("[INFO] (runner) %v/%v watchers done", received, r.watcher.Size())
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
			r.config.PidFile, err)
	}
	close(r.DoneCh)
}

// Receive accepts data from Consul and maps that data to the prefix.
func (r *Runner) Receive(view *watch.View) {
	r.Lock()
	defer r.Unlock()
	r.data[view.Dependency.HashCode()] = view
}

// Run invokes a single pass of the runner.
func (r *Runner) Run() (count int, err error) {
	log.Printf("[INFO] (runner) running")

	prefixes := r.config.Prefixes
	doneCh := make(chan bool, len(prefixes))
	errCh := make(chan error, len(prefixes))

	// Replicate each prefix in a goroutine
	for _, prefix := range prefixes {
		go r.replicate(prefix, r.config.Excludes, doneCh, errCh)
	}

	var errs *multierror.Error
	for i := 0; i < len(prefixes); i++ {
		select {
		case gotit := <-doneCh:
			if gotit {
				count++
			}
			// OK
		case err := <-errCh:
			errs = multierror.Append(errs, err)
		}
	}
	return count, errs.ErrorOrNil()
}

// init creates the Runner's underlying data structures and returns an error if
// any problems occur.
func (r *Runner) init() error {
	// Ensure we have default vaults
	config := DefaultConfig()
	config.Merge(r.config)
	r.config = config

	// Print the final config for debugging
	result, err := json.MarshalIndent(r.config, "", "  ")
	if err != nil {
		return err
	}
	log.Printf("[DEBUG] (runner) final config (tokens suppressed):\n\n%s\n\n",
		result)

	// Create the client
	client, err := newAPIClient(r.config)
	if err != nil {
		return fmt.Errorf("runner: %s", err)
	}
	r.client = client

	// Create the watcher
	watcher, err := newWatcher(r.config, client, r.once)
	if err != nil {
		return fmt.Errorf("runner: %s", err)
	}
	r.watcher = watcher

	r.data = make(map[string]*watch.View)

	r.outStream = os.Stdout
	r.errStream = os.Stderr

	r.ErrCh = make(chan error)
	r.DoneCh = make(chan struct{})

	return nil
}

// get returns the data for a particular view in the watcher.
func (r *Runner) get(prefix *Prefix) (*watch.View, bool) {
	r.RLock()
	defer r.RUnlock()
	result, ok := r.data[prefix.Source.HashCode()]
	return result, ok
}

// replicate performs replication into the current datacenter from the given
// prefix. This function is designed to be called via a goroutine since it is
// expensive and needs to be parallelized.
func (r *Runner) replicate(prefix *Prefix, excludes []*Exclude, doneCh chan bool, errCh chan error) {
	// Ensure we are not self-replicating
	info, err := r.client.Agent().Self()
	if err != nil {
		errCh <- fmt.Errorf("failed to query agent: %s", err)
		return
	}
	localDatacenter := info["Config"]["Datacenter"].(string)
	if localDatacenter == prefix.Source.DataCenter {
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
		log.Printf("[INFO] (runner) no data for %q", prefix.Source.Display())
		doneCh <- false
		return
	}

	// Get the data from the view
	data, lastIndex := view.DataAndLastIndex()
	pairs, ok := data.([]*dep.KeyPair)
	if !ok {
		errCh <- fmt.Errorf("could not convert watch data")
		return
	}

	kv := r.client.KV()

	// Update keys to the most recent versions
	updates := 0
	usedKeys := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		key := prefix.Destination + pair.Key
		usedKeys[key] = struct{}{}

		// Ignore if the key falls under an excluded prefix
		if len(excludes) > 0 {
			excluded := false
			for _, exclude := range excludes {
				if strings.HasPrefix(pair.Path, exclude.Source) {
					log.Printf("[DEBUG] (runner) key %q has prefix %q, excluding",
						pair.Path, exclude.Source)
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
	localKeys, _, err := kv.Keys(prefix.Destination, "", nil)
	if err != nil {
		errCh <- fmt.Errorf("failed to list keys: %s", err)
		return
	}
	for _, key := range localKeys {
		excluded := false

		// Ignore if the key falls under an excluded prefix
		if len(excludes) > 0 {
			sourceKey := strings.Replace(key, prefix.Destination, prefix.Source.Prefix, -1)
			for _, exclude := range excludes {
				if strings.HasPrefix(sourceKey, exclude.Source) {
					log.Printf("[DEBUG] (runner) key %q has prefix %q, excluding from deletes",
						sourceKey, exclude.Source)
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
	status.Source = prefix.Source.Prefix
	status.Destination = prefix.Destination
	if err := r.setStatus(prefix, status); err != nil {
		errCh <- fmt.Errorf("failed to checkpoint status: %s", err)
		return
	}

	if updates > 0 || deletes > 0 {
		log.Printf("[INFO] (runner) replicated %d updates, %d deletes", updates, deletes)
	}

	// We are done!
	doneCh <- true
}

// getStatus is used to read the last replication status.
func (r *Runner) getStatus(prefix *Prefix) (*Status, error) {
	kv := r.client.KV()
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
func (r *Runner) setStatus(prefix *Prefix, status *Status) error {
	// Encode the JSON as pretty so operators can easily view it in the Consul UI.
	enc, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}

	// Put the key to Consul.
	kv := r.client.KV()
	_, err = kv.Put(&api.KVPair{
		Key:   r.statusPath(prefix),
		Value: enc,
	}, nil)
	return err
}

func (r *Runner) statusPath(prefix *Prefix) string {
	plain := fmt.Sprintf("%s-%s", prefix.Source.Prefix, prefix.Destination)
	hash := md5.Sum([]byte(plain))
	enc := hex.EncodeToString(hash[:])
	return filepath.Join(r.config.StatusDir, enc)
}

// storePid is used to write out a PID file to disk.
func (r *Runner) storePid() error {
	path := r.config.PidFile
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
	path := r.config.PidFile
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

// newAPIClient creates a new API client from the given config and
func newAPIClient(config *Config) (*api.Client, error) {
	log.Printf("[INFO] (runner) creating consul/api client")

	consulConfig := api.DefaultConfig()

	if config.Consul != "" {
		log.Printf("[DEBUG] (runner) setting address to %s", config.Consul)
		consulConfig.Address = config.Consul
	}

	if config.Token != "" {
		log.Printf("[DEBUG] (runner) setting token to %s", config.Token)
		consulConfig.Token = config.Token
	}

	if config.SSL.Enabled {
		log.Printf("[DEBUG] (runner) enabling SSL")
		consulConfig.Scheme = "https"
	}

	if !config.SSL.Verify {
		log.Printf("[WARN] (runner) disabling SSL verification")
		consulConfig.HttpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	if config.Auth != nil {
		log.Printf("[DEBUG] (runner) setting basic auth")
		consulConfig.HttpAuth = &api.HttpBasicAuth{
			Username: config.Auth.Username,
			Password: config.Auth.Password,
		}
	}

	client, err := api.NewClient(consulConfig)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// newWatcher creates a new watcher.
func newWatcher(config *Config, client *api.Client, once bool) (*watch.Watcher, error) {
	log.Printf("[INFO] (runner) creating Watcher")

	clients := dep.NewClientSet()
	if err := clients.CreateConsulClient(&dep.CreateConsulClientInput{
		Address:      config.Consul,
		Token:        config.Token,
		AuthEnabled:  config.Auth.Enabled,
		AuthUsername: config.Auth.Username,
		AuthPassword: config.Auth.Password,
		SSLEnabled:   config.SSL.Enabled,
		SSLVerify:    config.SSL.Verify,
	}); err != nil {
		return nil, err
	}

	watcher, err := watch.NewWatcher(&watch.WatcherConfig{
		Clients:  clients,
		Once:     once,
		MaxStale: config.MaxStale,
		RetryFunc: func(current time.Duration) time.Duration {
			return config.Retry
		},
	})
	if err != nil {
		return nil, err
	}

	return watcher, err
}
