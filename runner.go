package main

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

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
		case <-r.watcher.FinishCh:
			log.Printf("[INFO] (runner) watcher reported finish")
			return
		case <-r.DoneCh:
			log.Printf("[INFO] (runner) received finish")
			return
		}

		// If we got this far, that means we got new data or one of the timers
		// fired, so attempt to run.
		if err := r.Run(); err != nil {
			r.ErrCh <- err
			return
		}
	}
}

// Stop halts the execution of this runner and its subprocesses.
func (r *Runner) Stop() {
	log.Printf("[INFO] (runner) stopping")
	r.watcher.Stop()
	close(r.DoneCh)
}

// Receive accepts data from Consul and maps that data to the prefix.
func (r *Runner) Receive(view *watch.View) {
	r.Lock()
	defer r.Unlock()
	r.data[view.Dependency.HashCode()] = view
}

// Run invokes a single pass of the runner.
func (r *Runner) Run() error {
	log.Printf("[INFO] (runner) running")

	prefixes := r.config.Prefixes
	doneCh := make(chan struct{}, len(prefixes))
	errCh := make(chan error, len(prefixes))

	// Replicate each prefix in a goroutine
	for _, prefix := range prefixes {
		go r.replicate(prefix, doneCh, errCh)
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
	// Merge multiple configs if given
	if r.config.Path != "" {
		err := buildConfig(r.config, r.config.Path)
		if err != nil {
			return fmt.Errorf("runner: %s", err)
		}
	}

	// Add default values for the config
	r.config.Merge(DefaultConfig())

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
func (r *Runner) replicate(prefix *Prefix, doneCh chan struct{}, errCh chan error) {
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
		doneCh <- struct{}{}
		return
	}

	// Get the data from the view
	pairs, ok := view.Data.([]*dep.KeyPair)
	if !ok {
		errCh <- fmt.Errorf("could not convert watch data")
		return
	}

	kv := r.client.KV()

	// Update keys to the most recent versions
	updates := 0
	usedKeys := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		if prefix.Source.Prefix != prefix.Destination {
			pair.Key = strings.Replace(pair.Key, prefix.Source.Prefix, prefix.Destination, 1)
		}
		usedKeys[pair.Key] = struct{}{}

		// Ignore if the modify index is old
		if pair.ModifyIndex <= status.LastReplicated {
			log.Printf("[DEBUG] (runner) skipping because %q is already "+
				"replicated", pair.Key)
			continue
		}

		// Check if lock
		if pair.Flags == api.SemaphoreFlagValue {
			log.Printf("[WARN] (runner) lock in use at %q, but sessions cannot be "+
				"replicated across datacenters", pair.Key)
			pair.Session = ""
		}

		// Check if semaphor
		if pair.Flags == api.LockFlagValue {
			log.Printf("[WARN] (runner) semaphore in use at %q, but sessions cannot "+
				"be replicated across datacenters", pair.Key)
			pair.Session = ""
		}

		// Check if session attached
		if pair.Session != "" {
			log.Printf("[WARN] (runner) %q has attached session, but sessions "+
				"cannot be replicated across datacenters", pair.Key)
			pair.Session = ""
		}

		if _, err := kv.Put(&api.KVPair{
			Key:     pair.Key,
			Flags:   pair.Flags,
			Value:   []byte(pair.Value),
			Session: pair.Session,
		}, nil); err != nil {
			errCh <- fmt.Errorf("failed to write %q: %s", pair.Key, err)
			return
		}
		log.Printf("[DEBUG] (runner) updated key %q", pair.Key)
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
		if _, ok := usedKeys[key]; !ok {
			if _, err := kv.Delete(key, nil); err != nil {
				errCh <- fmt.Errorf("failed to delete %q: %s", key, err)
				return
			}
			log.Printf("[DEBUG] (runner) deleted %q", key)
			deletes++
		}
	}

	// Update our status
	status.LastReplicated = view.LastIndex
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
	doneCh <- struct{}{}
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

	watcher, err := watch.NewWatcher(&watch.WatcherConfig{
		Client:   client,
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

// buildConfig iterates and merges all configuration files in a given directory.
// The config parameter will be modified and merged with subsequent configs
// found in the directory.
func buildConfig(config *Config, path string) error {
	log.Printf("[DEBUG] merging with config at %s", path)

	// Ensure the given filepath exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("config: missing file/folder: %s", path)
	}

	// Check if a file was given or a path to a directory
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("config: error stating file: %s", err)
	}

	// Recursively parse directories, single load files
	if stat.Mode().IsDir() {
		// Ensure the given filepath has at least one config file
		files, err := ioutil.ReadDir(path)
		if err != nil {
			return fmt.Errorf("config: error listing directory: %s", err)
		}
		if len(files) == 0 {
			return fmt.Errorf("config: must contain at least one configuration file")
		}

		// Potential bug: Walk does not follow symlinks!
		err = filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			// If WalkFunc had an error, just return it
			if err != nil {
				return err
			}

			// Do nothing for directories
			if info.IsDir() {
				return nil
			}

			// Parse and merge the config
			newConfig, err := ParseConfig(path)
			if err != nil {
				return err
			}
			config.Merge(newConfig)

			return nil
		})

		if err != nil {
			return fmt.Errorf("config: walk error: %s", err)
		}
	} else if stat.Mode().IsRegular() {
		newConfig, err := ParseConfig(path)
		if err != nil {
			return err
		}
		config.Merge(newConfig)
	} else {
		return fmt.Errorf("config: unknown filetype: %s", stat.Mode().String())
	}

	return nil
}
