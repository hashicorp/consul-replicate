package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	consulapi "github.com/hashicorp/consul/api"
)

const (
	// statusCheckpoint controls how often we write back out status
	statusCheckpoint = 5 * time.Second

	// retryInterval controls how often we retry on an error
	retryInterval = 30 * time.Second
)

// status struct is used to checkpoint our status
type status struct {
	LastReplicated uint64
}

// shouldQuit is used to check if a stop channel is closed
func shouldQuit(stopCh chan struct{}) bool {
	select {
	case <-stopCh:
		return true
	default:
		return false
	}
}

// Replicator is used to do the actual replication
type Replicator struct {
	conf   *ReplicationConfig
	client *consulapi.Client
	stopCh chan struct{}
	doneCh chan struct{}
}

// replicate is a long running routine that manages replication.
// The stopCh is used to signal we should terminate, and the doneCh is closed
// when we finish
func (r *Replicator) run() {
	defer func() {
		// Cleanup the service entry
		if err := r.removeService(); err != nil {
			log.Printf("[ERR] Failed to cleanup %s service: %v", r.conf.Service, err)
		}
		close(r.doneCh)
	}()

	// Ensure the service is setup on the local agent
	if err := r.setupService(); err != nil {
		log.Printf("[ERR] Failed to setup %s service: %v", r.conf.Service, err)
		return
	}

	// Keep our health check alive
	if err := r.maintainCheck(); err != nil {
		log.Printf("[ERR] Failed to update check TTL: %v", err)
		return
	}

ACQUIRE:
	// Re-check if we should exit
	if shouldQuit(r.stopCh) {
		return
	}

	// Acquire leadership for this
	leaderCh, err := r.acquireLeadership()
	if err != nil {
		log.Printf("[ERR] Failed to acquire leadership: %v", err)
		return
	}

	// Replicate now that we are the leader
REPLICATE:
	if err := r.replicateKeys(leaderCh); err != nil {
		log.Printf("[ERR] Failed to replicate keys: %v", err)
	}

	// Check if we are still the leader
	if shouldQuit(leaderCh) {
		goto ACQUIRE
	}

	// Some error, back-off and retry
	log.Printf("[INFO] Replication paused for %v", retryInterval)
	select {
	case <-time.After(retryInterval):
		goto REPLICATE
	case <-leaderCh:
		goto ACQUIRE
	case <-r.stopCh:
		return
	}
}

// serviceID generates an ID for the service
func (r *Replicator) serviceID() string {
	return fmt.Sprintf("%s-%d", r.conf.Service, r.conf.Pid)
}

// checkID generates an ID for the service check
func (r *Replicator) checkID() string {
	return fmt.Sprintf("service:%s", r.serviceID())
}

// setupService is used to create the local service entries with the agent
func (r *Replicator) setupService() error {
	reg := &consulapi.AgentServiceRegistration{
		ID:   r.serviceID(),
		Name: r.conf.Service,
		Check: &consulapi.AgentServiceCheck{
			TTL: "5s",
		},
	}
	agent := r.client.Agent()
	return agent.ServiceRegister(reg)
}

// removeService is used to cleanup the service entry we created
func (r *Replicator) removeService() error {
	agent := r.client.Agent()
	return agent.ServiceDeregister(r.serviceID())
}

// maintainCheck periodically hits our TTL check to keep it alive
func (r *Replicator) maintainCheck() error {
	// Try to update the TTL immediately
	agent := r.client.Agent()
	checkID := r.checkID()
	if err := agent.PassTTL(checkID, ""); err != nil {
		return err
	}

	// Run the update in the background
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := agent.PassTTL(checkID, ""); err != nil {
					log.Printf("[ERR] Failed to update check TTL: %v", err)
				}
			case <-r.doneCh:
				return
			}
		}
	}()
	return nil
}

// acquireLeadership is used to wait until we are the leader in the cluster
func (r *Replicator) acquireLeadership() (chan struct{}, error) {
	// Create a new session
	kv := r.client.KV()
	session := r.client.Session()
	se := &consulapi.SessionEntry{
		Name:   fmt.Sprintf("Lock for %s service", r.conf.Service),
		Checks: []string{r.checkID(), "serfHealth"},
	}
	id, _, err := session.Create(se, nil)
	if err != nil {
		return nil, err
	}

	// Construct a key to lock
	p := &consulapi.KVPair{
		Key:     r.conf.Lock,
		Value:   lockValue(r.conf),
		Session: id,
	}

	opts := &consulapi.QueryOptions{
		WaitTime: 30 * time.Second,
	}
WAIT:
	if shouldQuit(r.stopCh) {
		return nil, fmt.Errorf("Lock acquisition aborted")
	}

	// Look for an existing lock, blocking until not taken
	pair, meta, err := kv.Get(r.conf.Lock, opts)
	if err != nil {
		return nil, err
	}
	if pair != nil && pair.Session != "" {
		opts.WaitIndex = meta.LastIndex
		log.Printf("[INFO] Lock already held, watching leader")
		goto WAIT
	}

	// Try to acquire the lock
	locked, _, err := kv.Acquire(p, nil)
	if err != nil {
		return nil, err
	}
	if !locked {
		log.Printf("[WARN] Failed to acquire lock on %s, sleeping", r.conf.Lock)
		select {
		case <-time.After(5 * time.Second):
			goto WAIT
		case <-r.stopCh:
			return nil, fmt.Errorf("Lock acquisition aborted")
		}
	}

	// Watch to ensure we maintain leadership
	leaderCh := make(chan struct{})
	go r.watchLock(id, leaderCh)

	// Locked! All done
	log.Printf("[INFO] Lock %s acquired", r.conf.Lock)
	return leaderCh, nil
}

// watchLock watches the given key to ensure we are still the leader
func (r *Replicator) watchLock(session string, stopCh chan struct{}) {
	defer func() {
		if _, err := r.client.Session().Destroy(session, nil); err != nil {
			log.Printf("[ERR] Failed to destroy session: %v", err)
		}
	}()
	kv := r.client.KV()
	opts := &consulapi.QueryOptions{RequireConsistent: true}
WAIT:
	pair, meta, err := kv.Get(r.conf.Lock, opts)
	if err != nil {
		close(stopCh)
		log.Printf("[ERR] Stepping down, failed to watch lock: %v", err)
		return
	}
	if pair != nil && pair.Session == session {
		opts.WaitIndex = meta.LastIndex
		goto WAIT
	}
	close(stopCh)
	log.Printf("[ERR] Stepping down, session invalidated")
	return
}

// replicateKeys is used to actively replicate once we are the leader
func (r *Replicator) replicateKeys(leaderCh chan struct{}) error {
	// Read our last status
	status, err := readStatus(r.conf, r.client)
	if err != nil {
		return fmt.Errorf("failed to read replication status: %v", err)
	}

	kv := r.client.KV()
	opts := &consulapi.QueryOptions{
		Datacenter: r.conf.SourceDC,
		WaitTime:   30 * time.Second,
	}
	log.Printf("[INFO] Watching for changes")
WAIT:
	if shouldQuit(leaderCh) || shouldQuit(r.stopCh) {
		return nil
	}
	opts.WaitIndex = status.LastReplicated
	pairs, qm, err := kv.List(r.conf.Prefixes[0].SourcePrefix, opts)
	if err != nil {
		return err
	}
	if shouldQuit(leaderCh) || shouldQuit(r.stopCh) {
		return nil
	}

	// Update any key that recently updated
	updates := 0
	keys := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		if r.conf.Prefixes[0].SourcePrefix != r.conf.Prefixes[0].DestinationPrefix {
			pair.Key = strings.Replace(pair.Key, r.conf.Prefixes[0].SourcePrefix, r.conf.Prefixes[0].DestinationPrefix, 1)
		}
		keys[pair.Key] = struct{}{}

		// Ignore if the modify index is old
		if pair.ModifyIndex <= status.LastReplicated {
			continue
		}
		if _, err := kv.Put(pair, nil); err != nil {
			return fmt.Errorf("failed to write key %s: %v", pair.Key, err)
		}
		log.Printf("[DEBUG] Updated key: %s", pair.Key)
		updates++
	}

	// Handle any deletes
	localKeys, _, err := kv.Keys(r.conf.Prefixes[0].DestinationPrefix, "", nil)
	if err != nil {
		return err
	}
	deletes := 0
	for _, key := range localKeys {
		if _, ok := keys[key]; ok {
			continue
		}
		if _, err := kv.Delete(key, nil); err != nil {
			return fmt.Errorf("failed to delete key %s: %v", key, err)
		}
		log.Printf("[DEBUG] Deleted key: %s", key)
		deletes++
	}

	// Update our status
	status.LastReplicated = qm.LastIndex
	if err := writeStatus(r.conf, r.client, status); err != nil {
		log.Printf("[ERR] Failed to checkpoint status: %v", err)
	}
	if updates > 0 || deletes > 0 {
		log.Printf("[INFO] Synced %d updates, %d deletes", updates, deletes)
	}
	goto WAIT
}

// lockValue generates a value to set for the key lock
func lockValue(conf *ReplicationConfig) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.Encode(conf)
	return buf.Bytes()
}

// readStatus is used to read the last replication status
func readStatus(conf *ReplicationConfig, client *consulapi.Client) (*status, error) {
	kv := client.KV()
	pair, _, err := kv.Get(conf.Prefixes[0].Status, nil)
	if err != nil {
		return nil, err
	}
	status := new(status)
	if pair != nil {
		dec := json.NewDecoder(bytes.NewReader(pair.Value))
		if err := dec.Decode(status); err != nil {
			return nil, err
		}
	}
	return status, nil
}

// writeStatus is used to update the last replication status
func writeStatus(conf *ReplicationConfig, client *consulapi.Client, status *status) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.Encode(status)

	// Create the KVPair
	pair := &consulapi.KVPair{
		Key:   conf.Prefixes[0].Status,
		Value: buf.Bytes(),
	}
	kv := client.KV()
	_, err := kv.Put(pair, nil)
	return err
}
