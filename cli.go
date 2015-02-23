package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hashicorp/consul-template/logging"
	"github.com/hashicorp/consul-template/watch"
)

// Exit codes are int values that represent an exit code for a particular error.
// Sub-systems may check this unique error to determine the cause of an error
// without parsing the output or help text.
//
// Errors start at 10
const (
	ExitCodeOK int = 0

	ExitCodeError = 10 + iota
	ExitCodeParseFlagsError
	ExitCodeLoggingError
	ExitCodeRunnerError
	ExitCodeInterrupt
)

/// ------------------------- ///

// CLI is the main entry point for Consul Replicate.
type CLI struct {
	sync.Mutex

	// outSteam and errStream are the standard out and standard error streams to
	// write messages from the CLI.
	outStream, errStream io.Writer

	// stopCh is an internal channel used to trigger a shutdown of the CLI.
	stopCh  chan struct{}
	stopped bool
}

func NewCLI(out, err io.Writer) *CLI {
	return &CLI{
		outStream: out,
		errStream: err,
		stopCh:    make(chan struct{}),
	}
}

// Run accepts a slice of arguments and returns an int representing the exit
// status from the command.
func (cli *CLI) Run(args []string) int {
	// Parse the flags
	config, once, version, err := cli.parseFlags(args[1:])
	if err != nil {
		return cli.handleError(err, ExitCodeParseFlagsError)
	}

	// Setup the logging
	if err := logging.Setup(&logging.Config{
		Name:           Name,
		Level:          config.LogLevel,
		Syslog:         config.Syslog.Enabled,
		SyslogFacility: config.Syslog.Facility,
		Writer:         cli.errStream,
	}); err != nil {
		return cli.handleError(err, ExitCodeLoggingError)
	}

	// If the version was requested, return an "error" containing the version
	// information. This might sound weird, but most *nix applications actually
	// print their version on stderr anyway.
	if version {
		log.Printf("[DEBUG] (cli) version flag was given, exiting now")
		fmt.Fprintf(cli.errStream, "%s v%s\n", Name, Version)
		return ExitCodeOK
	}

	// Initial runner
	runner, err := NewRunner(config, once)
	if err != nil {
		return cli.handleError(err, ExitCodeRunnerError)
	}
	go runner.Start()

	// Listen for signals
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	for {
		select {
		case err := <-runner.ErrCh:
			return cli.handleError(err, ExitCodeRunnerError)
		case <-runner.DoneCh:
			return ExitCodeOK
		case s := <-signalCh:
			switch s {
			case syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
				fmt.Fprintf(cli.errStream, "Received interrupt, cleaning up...\n")
				runner.Stop()
				return ExitCodeInterrupt
			case syscall.SIGHUP:
				fmt.Fprintf(cli.errStream, "Received HUP, reloading configuration...\n")
				runner.Stop()
				runner, err = NewRunner(config, once)
				if err != nil {
					return cli.handleError(err, ExitCodeRunnerError)
				}
				go runner.Start()
			}
		case <-cli.stopCh:
			return ExitCodeOK
		}
	}
}

// stop is used internally to shutdown a running CLI
func (cli *CLI) stop() {
	cli.Lock()
	defer cli.Unlock()

	if cli.stopped {
		return
	}

	close(cli.stopCh)
	cli.stopped = true
}

// parseFlags is a helper function for parsing command line flags using Go's
// Flag library. This is extracted into a helper to keep the main function
// small, but it also makes writing tests for parsing command line arguments
// much easier and cleaner.
func (cli *CLI) parseFlags(args []string) (*Config, bool, bool, error) {
	var once, version bool
	var config = DefaultConfig()

	// Parse the flags and options
	flags := flag.NewFlagSet(Name, flag.ContinueOnError)
	flags.SetOutput(cli.errStream)
	flags.Usage = func() {
		fmt.Fprintf(cli.errStream, usage, Name)
	}
	flags.StringVar(&config.Consul, "consul", config.Consul, "")
	flags.StringVar(&config.Token, "token", config.Token, "")
	flags.Var((*authVar)(config.Auth), "auth", "")
	flags.BoolVar(&config.SSL.Enabled, "ssl", config.SSL.Enabled, "")
	flags.BoolVar(&config.SSL.Verify, "ssl-verify", config.SSL.Verify, "")
	flags.DurationVar(&config.MaxStale, "max-stale", config.MaxStale, "")
	flags.BoolVar(&config.Syslog.Enabled, "syslog", config.Syslog.Enabled, "")
	flags.StringVar(&config.Syslog.Facility, "syslog-facility", config.Syslog.Facility, "")
	flags.Var((*prefixVar)(&config.Prefixes), "prefix", "")
	flags.Var((*watch.WaitVar)(config.Wait), "wait", "")
	flags.DurationVar(&config.Retry, "retry", config.Retry, "")
	flags.StringVar(&config.Path, "config", config.Path, "")
	flags.StringVar(&config.LogLevel, "log-level", config.LogLevel, "")
	flags.BoolVar(&once, "once", false, "")
	flags.BoolVar(&version, "version", false, "")

	// Deprecated options
	var deprecatedAddr string
	flags.StringVar(&deprecatedAddr, "addr", config.Consul, "")
	var deprecatedDest string
	flags.StringVar(&deprecatedDest, "dst-prefix", "", "")
	var deprecatedSrc string
	flags.StringVar(&deprecatedSrc, "src", "", "")

	// If there was a parser error, stop
	if err := flags.Parse(args); err != nil {
		return nil, false, false, err
	}

	// Handle deprecations
	if deprecatedAddr != "" {
		log.Printf("[WARN] -addr is deprecated - please use -consul=<...> instead")
		config.Consul = deprecatedAddr
	}
	if deprecatedDest != "" {
		log.Printf("[WARN] -dst-prefix is deprecated - please use -prefix=<source:destination> instead")

		// If there are no prefixes, we cannot reasonably continue
		if len(config.Prefixes) < 1 {
			return nil, false, false, fmt.Errorf("must specify at least one prefix")
		}

		config.Prefixes[0].Destination = deprecatedDest
	}
	if deprecatedSrc != "" {
		log.Printf("[WARN] -src is deprecated - please use -prefix=<source:destination> instead")

		// If there are no prefixes, we cannot reasonably continue
		if len(config.Prefixes) < 1 {
			return nil, false, false, fmt.Errorf("must specify at least one prefix")
		}

		// This is pretty jank, but build the thing into a string so we can convert
		// it back into a prefix. Good times. Good times.
		prefix := config.Prefixes[0]
		raw := fmt.Sprintf("%s@%s:%s", prefix.Source.Prefix, deprecatedSrc, prefix.Destination)
		newPrefix, err := ParsePrefix(raw)
		if err != nil {
			return nil, false, false, fmt.Errorf("error parsing source datacenter: %s", err)
		}

		config.Prefixes[0] = newPrefix
	}

	return config, once, version, nil
}

// handleError outputs the given error's Error() to the errStream and returns
// the given exit status.
func (cli *CLI) handleError(err error, status int) int {
	fmt.Fprintf(cli.errStream, "Consul Replicate returned errors:\n%s", err)
	return status
}

const usage = `
Usage: %s [options]

  Replicates key-value data from a source datacenter to the datacenter(s) of a
  Consul agent.

Options:

  -auth=<user[:pass]>      Set the basic authentication username (and password)
  -consul=<address>        Sets the address of the Consul instance
  -max-stale=<duration>    Set the maximum staleness and allow stale queries to
                           Consul which will distribute work among all servers
                           instead of just the leader
  -ssl                     Use SSL when connecting to Consul
  -ssl-verify              Verify certificates when connecting via SSL
  -token=<token>           Sets the Consul API token

  -syslog                  Send the output to syslog instead of standard error
                           and standard out. The syslog facility defaults to
                           LOCAL0 and can be changed using a configuration file
  -syslog-facility=<f>     Set the facility where syslog should log. If this
                           attribute is supplied, the -syslog flag must also be
                           supplied.

  -prefix=<src[:dest]>     Provides the source prefix in the replicating
                           datacenter and optionally the destination prefix in
                           the destination datacenters - if the destination is
                           omitted, it is assumed to be the same as the source
  -wait=<duration>         Sets the 'minumum(:maximum)' amount of time to wait
                           for stability before replicating
  -retry=<duration>        The amount of time to wait if Consul returns an
                           error when communicating with the API

  -config=<path>           Sets the path to a configuration file on disk

  -log-level=<level>       Set the logging level - valid values are "debug",
                           "info", "warn" (default), and "err"

  -once                    Do not run the process as a daemon
  -version                 Print the version of this daemon

Advanced Options:

  -lock=<path>             Sets the path in the KV store that is used to perform
                           leader election for the replicators (default:
                           "service/consul-replicate/leader")
  -status=<path>           Sets the path in the KV store that is used to store
                           the replication status (default:
                           "service/consul-replicate/status")
  -service=<name>          Sets the name of the service that is registered in
                           Consul's catalog (default: "consul-replicate")
`

// TODO: Handle deprecations/updates for the following options:
//
// -addr                 Replace with -consul
// -dst-prefix=global/   Provides the prefix which is the root of replicated keys
//                       in the destination datacenter. Defaults to match source.
// -prefix=global/       Provides the prefix which is the root of replicated keys
//                       in the source datacenter
// -src=dc               Provides the source destination to replicate from
