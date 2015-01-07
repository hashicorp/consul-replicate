package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	consulapi "github.com/hashicorp/consul/api"
)

type ReplicationConfig struct {
	Name string
	Pid  int

	SourceDC string

	SourcePrefix      string
	DestinationPrefix string
	Token             string

	Lock    string
	Status  string
	Service string
}

func main() {
	os.Exit(realMain())
}

func realMain() int {
	consulConf := consulapi.DefaultConfig()
	replConf := &ReplicationConfig{}
	flag.Usage = usage
	flag.StringVar(&replConf.SourceDC, "src", "", "source datacenter")
	flag.StringVar(&replConf.SourcePrefix, "prefix", "global/", "source prefix")
	flag.StringVar(&replConf.DestinationPrefix, "dst-prefix", "", "destination prefix, defaults to source prefix")
	flag.StringVar(&consulConf.Address, "addr", "127.0.0.1:8500", "consul HTTP API address with port")
	flag.StringVar(&consulConf.Token, "token", "", "ACL token to use")
	flag.StringVar(&replConf.Lock, "lock", "service/consul-replicate/leader", "Lock used for coordination")
	flag.StringVar(&replConf.Status, "status", "service/consul-replicate/status", "Status file used for state")
	flag.StringVar(&replConf.Service, "service", "consul-replicate", "Service used for registration")
	flag.Parse()

	// Ensure we have a source dc
	if replConf.SourceDC == "" {
		log.Printf("[ERR] Must provide source datacenter")
		return 1
	}

	// Create a client
	client, err := consulapi.NewClient(consulConf)
	if err != nil {
		log.Printf("[ERR] Failed to create a client: %v", err)
		return 1
	}

	// Get the local agent info
	info, err := client.Agent().Self()
	if err != nil {
		log.Printf("[ERR] Failed to query agent: %v", err)
		return 1
	}
	localDC := info["Config"]["Datacenter"].(string)

	// Set our name and pid
	replConf.Name = info["Config"]["NodeName"].(string)
	replConf.Pid = syscall.Getpid()

	// Fill in the defaults
	if replConf.DestinationPrefix == "" {
		replConf.DestinationPrefix = replConf.SourcePrefix
	}

	// Sanity check config
	if replConf.SourceDC == localDC {
		log.Printf("[ERR] Local DC cannot be the source DC")
		return 1
	}

	// Log what we are about to do
	log.Printf("[INFO] Attempting to replicate from DC %s (%s) to %s (%s)",
		replConf.SourceDC, replConf.SourcePrefix,
		localDC, replConf.DestinationPrefix)

	// Start replication
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	repl := &Replicator{
		conf:   replConf,
		client: client,
		stopCh: stopCh,
		doneCh: doneCh,
	}
	go repl.run()

	// Wait for termination
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt)
	select {
	case <-signalCh:
		close(stopCh)
		log.Printf("[WARN] Received signal, stopping replication")
	case <-doneCh:
		close(stopCh)
		log.Printf("[WARN] Replication terminated")
		return 1
	}

	// Wait for clean termination, or timeout
	select {
	case <-doneCh:
	case <-time.After(60 * time.Second):
		log.Printf("[WARN] Timed out waiting for replication to stop")
	}
	return 0
}

func usage() {
	cmd := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, strings.TrimSpace(helpText)+"\n\n", cmd)
}

const helpText = `
Usage: %s [options]

  Replicates K/V data from a source datacenter to the datacenter of
  a Consul agent.

Options:

  -addr=127.0.0.1:8500  Provides the HTTP address of a Consul agent.
  -dst-prefix=global/   Provides the prefix which is the root of replicated keys
                        in the destination datacenter. Defaults to match source.
  -lock=path            Lock is used to provide the path in the KV store used to
                        perform leader election for the replicators. This ensures
                        a single replicator running per-DC in a high-availability
                        setup. Defaults to "service/consul-replicate/leader"
  -prefix=global/       Provides the prefix which is the root of replicated keys
                        in the source datacenter
  -service=name         Service sets the name of the service that is registered
                        in the catalog. Defaults to "consul-replicate"
  -src=dc               Provides the source destination to replicate from
  -status=path          Status is used to provide the path in the KV store used to
                        store our replication status. This is to checkpoint replication
                        periodically. Defaults to "service/consul-replicate/status"
  -token=""             Optional ACL token to use when reading and writing keys.
`
