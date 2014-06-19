package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/armon/consul-api"
	"log"
	"strings"
	"time"
)

const (
	// statusCheckpoint controls how often we write back out status
	statusCheckpoint = 5 * time.Second
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

// replicate is a long running routine that manages replication.
// The stopCh is used to signal we should terminate, and the doneCh is closed
// when we finish
func replicate(conf *ReplicationConfig, client *consulapi.Client, stopCh, doneCh chan struct{}) {
	defer func() {
		// Cleanup the service entry
		if err := removeService(conf, client); err != nil {
			log.Printf("[ERR] Failed to cleanup %s service: %v", conf.Service, err)
		}
		close(doneCh)
	}()

	// Ensure the service is setup on the local agent
	if err := setupService(conf, client); err != nil {
		log.Printf("[ERR] Failed to setup %s service: %v", conf.Service, err)
		return
	}

	// Keep our health check alive
	if err := maintainCheck(conf, client, doneCh); err != nil {
		log.Printf("[ERR] Failed to update check TTL: %v", err)
		return
	}

ACQUIRE:
	// Re-check if we should exit
	if shouldQuit(stopCh) {
		return
	}

	// Acquire leadership for this
	leaderCh, err := acquireLeadership(conf, client, stopCh)
	if err != nil {
		log.Printf("[ERR] Failed to acquire leadership: %v", err)
		return
	}

	// Replicate now that we are the leader
	if err := replicateKeys(conf, client, leaderCh, stopCh); err != nil {
		log.Printf("[ERR] Failed to replicate keys: %v", err)
	}
	goto ACQUIRE
}

// serviceID generates an ID for the service
func serviceID(conf *ReplicationConfig) string {
	return fmt.Sprintf("%s-%d", conf.Service, conf.Pid)
}

// checkID generates an ID for the service check
func checkID(conf *ReplicationConfig) string {
	return fmt.Sprintf("service:%s", serviceID(conf))
}

// setupService is used to create the local service entries with the agent
func setupService(conf *ReplicationConfig, client *consulapi.Client) error {
	reg := &consulapi.AgentServiceRegistration{
		ID:   serviceID(conf),
		Name: conf.Service,
		Check: &consulapi.AgentServiceCheck{
			TTL: "5s",
		},
	}
	agent := client.Agent()
	return agent.ServiceRegister(reg)
}

// removeService is used to cleanup the service entry we created
func removeService(conf *ReplicationConfig, client *consulapi.Client) error {
	agent := client.Agent()
	return agent.ServiceDeregister(serviceID(conf))
}

// maintainCheck periodically hits our TTL check to keep it alive
func maintainCheck(conf *ReplicationConfig, client *consulapi.Client, doneCh chan struct{}) error {
	// Try to update the TTL immediately
	agent := client.Agent()
	checkID := checkID(conf)
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
			case <-doneCh:
				return
			}
		}
	}()
	return nil
}

// lockValue generates a value to set for the key lock
func lockValue(conf *ReplicationConfig) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.Encode(conf)
	return buf.Bytes()
}

// acquireLeadership is used to wait until we are the leader in the cluster
func acquireLeadership(conf *ReplicationConfig, client *consulapi.Client, stopCh chan struct{}) (chan struct{}, error) {
	// Create a new session
	kv := client.KV()
	session := client.Session()
	se := &consulapi.SessionEntry{
		Name:   fmt.Sprintf("Lock for %s service", conf.Service),
		Checks: []string{checkID(conf), "serfHealth"},
	}
	id, _, err := session.Create(se, nil)
	if err != nil {
		return nil, err
	}

	// Construct a key to lock
	p := &consulapi.KVPair{
		Key:     conf.Lock,
		Value:   lockValue(conf),
		Session: id,
	}

	opts := &consulapi.QueryOptions{
		WaitTime: 30 * time.Second,
	}
WAIT:
	if shouldQuit(stopCh) {
		return nil, fmt.Errorf("Lock acquisition aborted")
	}

	// Look for an existing lock, blocking until not taken
	pair, meta, err := kv.Get(conf.Lock, opts)
	if err != nil {
		return nil, err
	}
	if pair != nil && pair.Session != "" {
		opts.WaitIndex = meta.LastIndex
		goto WAIT
	}

	// Try to acquire the lock
	locked, _, err := kv.Acquire(p, nil)
	if err != nil {
		return nil, err
	}
	if !locked {
		log.Printf("[WARN] Failed to acquire lock on %s, sleeping", conf.Lock)
		select {
		case <-time.After(5 * time.Second):
			goto WAIT
		case <-stopCh:
			return nil, fmt.Errorf("Lock acquisition aborted")
		}
	}

	// Watch to ensure we maintain leadership
	leaderCh := make(chan struct{})
	go watchLock(conf, client, id, leaderCh)

	// Locked! All done
	log.Printf("[INFO] Lock %s acquired", conf.Lock)
	return leaderCh, nil
}

// watchLock watches the given key to ensure we are still the leader
func watchLock(conf *ReplicationConfig, client *consulapi.Client, session string, stopCh chan struct{}) {
	kv := client.KV()
	opts := &consulapi.QueryOptions{}
WAIT:
	pair, meta, err := kv.Get(conf.Lock, opts)
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
func replicateKeys(conf *ReplicationConfig, client *consulapi.Client, leaderCh, stopCh chan struct{}) error {
	// Read our last status
	status, err := readStatus(conf, client)
	if err != nil {
		return fmt.Errorf("failed to read replication status: %v", err)
	}

	kv := client.KV()
	opts := &consulapi.QueryOptions{
		Datacenter: conf.SourceDC,
		WaitTime:   30 * time.Second,
	}
WAIT:
	if shouldQuit(leaderCh) || shouldQuit(stopCh) {
		return nil
	}
	opts.WaitIndex = status.LastReplicated
	pairs, qm, err := kv.List(conf.SourcePrefix, opts)
	if err != nil {
		return err
	}
	if qm.LastIndex == status.LastReplicated {
		goto WAIT
	}
	if shouldQuit(leaderCh) || shouldQuit(stopCh) {
		return nil
	}

	// Update any key that recently updated
	updates := 0
	keys := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		if conf.SourcePrefix != conf.DestinationPrefix {
			pair.Key = strings.Replace(pair.Key, conf.SourcePrefix, conf.DestinationPrefix, 1)
		}
		keys[pair.Key] = struct{}{}

		// Ignore if the modify index is old
		if pair.ModifyIndex <= status.LastReplicated {
			continue
		}
		if _, err := kv.Put(pair, nil); err != nil {
			return fmt.Errorf("failed to write key %s: %v", pair.Key, err)
		}
		updates++
	}

	// Handle any deletes
	localKeys, _, err := kv.Keys(conf.DestinationPrefix, "", nil)
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
		deletes++
	}

	// Update our status
	status.LastReplicated = qm.LastIndex
	if err := writeStatus(conf, client, status); err != nil {
		log.Printf("[ERR] Failed to checkpoint status: %v", err)
	}
	log.Printf("[INFO] Synced %d updates, %d deletes", updates, deletes)
	goto WAIT
}

// readStatus is used to read the last replication status
func readStatus(conf *ReplicationConfig, client *consulapi.Client) (*status, error) {
	kv := client.KV()
	pair, _, err := kv.Get(conf.Status, nil)
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
		Key:   conf.Status,
		Value: buf.Bytes(),
	}
	kv := client.KV()
	_, err := kv.Put(pair, nil)
	return err
}
