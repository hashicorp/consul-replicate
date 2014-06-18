package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/armon/consul-api"
)

type ReplicationConfig struct {
	SourceDC      string
	DestinationDC string

	SourcePrefix      string
	DestinationPrefix string

	Lock    string
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
	flag.StringVar(&replConf.DestinationDC, "dst", "", "destination datacenter, defaults to local")
	flag.StringVar(&replConf.SourcePrefix, "prefix", "global/", "source prefix")
	flag.StringVar(&replConf.DestinationPrefix, "dst-prefix", "", "destination prefix, defaults to source prefix")
	flag.StringVar(&consulConf.Address, "addr", "127.0.0.1:8500", "consul HTTP API address with port")
	flag.StringVar(&replConf.Lock, "lock", "service/consul-replicate/leader", "Lock used for coordination")
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

	// Fill in the defaults
	if replConf.DestinationPrefix == "" {
		replConf.DestinationPrefix = replConf.SourcePrefix
	}
	if replConf.DestinationDC == "" {
		replConf.DestinationDC = info["Config"]["Datacenter"].(string)
	}

	// Sanity check config
	if replConf.SourceDC == replConf.DestinationDC {
		log.Printf("[ERR] Destination DC cannot be the source DC")
		return 1
	}

	// Log what we are about to do
	log.Printf("[INFO] Attempting to replicate from DC %s (%s) to %s (%s)",
		replConf.SourceDC, replConf.SourcePrefix,
		replConf.DestinationDC, replConf.DestinationPrefix)
	return 0
}

func usage() {
	cmd := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, strings.TrimSpace(helpText)+"\n\n", cmd)
}

const helpText = `
Usage: %s [options]

  Replicates K/V data from a source datacenter to a target
  datacenter.

Options:

  -addr=127.0.0.1:8500  Provides the HTTP address of a Consul agent.
  -src=dc1              Provides the source destination to replicate from
  -dst=dc2              Provides the destination datacenter. Defaults to the
                        datacenter of the agent being used.
  -prefix=global/       Provides the prefix which is the root of replicated keys
                        in the source datacenter
  -dst-prefix=global/   Provides the prefix which is the root of replicated keys
                        in the destination datacenter. Defaults to match source.
  -lock=path            Lock is used to provide the path in the KV store used to
                        perform leader election for the replicators. This ensures
                        a single replicator running per-DC in a high-availability
                        setup. Defaults to "service/consul-replicate/leader"
  -service              Service sets the name of the service that is registered
                        in the catalog. Defaults to "consul-replicate"
`
