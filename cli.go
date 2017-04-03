package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/consul-template/config"
	"github.com/hashicorp/consul-template/logging"
)

// Exit codes are int values that represent an exit code for a particular error.
// Sub-systems may check this unique error to determine the cause of an error
// without parsing the output or help text.
//
// Errors start at 10
const (
	ExitCodeOK int = 0

	ExitCodeError = 10 + iota
	ExitCodeInterrupt
	ExitCodeParseFlagsError
	ExitCodeRunnerError
	ExitCodeConfigError
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
	c, once, version, err := cli.parseFlags(args[1:])
	if err != nil {
		return cli.handleError(err, ExitCodeParseFlagsError)
	}

	// Save original config (defaults + parsed flags) for handling reloads
	baseConfig := c.Copy()

	// Setup the config and logging
	c, err = cli.setup(c)
	if err != nil {
		return cli.handleError(err, ExitCodeConfigError)
	}

	// Print version information for debugging
	log.Printf("[INFO] %s", humanVersion)

	// If the version was requested, return an "error" containing the version
	// information. This might sound weird, but most *nix applications actually
	// print their version on stderr anyway.
	if version {
		log.Printf("[DEBUG] (cli) version flag was given, exiting now")
		fmt.Fprintf(cli.errStream, "%s\n", humanVersion)
		return ExitCodeOK
	}

	// Initial runner
	runner, err := NewRunner(c, once)
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

				// Load the new configuration from disk
				c, err = cli.setup(baseConfig)
				if err != nil {
					return cli.handleError(err, ExitCodeConfigError)
				}

				runner, err = NewRunner(c, once)
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
	c := DefaultConfig()

	// Parse the flags and options
	flags := flag.NewFlagSet(Name, flag.ContinueOnError)
	flags.SetOutput(cli.errStream)
	flags.Usage = func() { fmt.Fprintf(cli.errStream, usage, Name) }

	flags.Var((funcVar)(func(s string) error {
		c.Consul = s
		c.set("consul")
		return nil
	}), "consul", "")

	flags.Var((funcVar)(func(s string) error {
		c.Token = s
		c.set("token")
		return nil
	}), "token", "")

	flags.Var((funcVar)(func(s string) error {
		c.Auth.Enabled = true
		c.set("auth.enabled")
		if strings.Contains(s, ":") {
			split := strings.SplitN(s, ":", 2)
			c.Auth.Username = split[0]
			c.set("auth.username")
			c.Auth.Password = split[1]
			c.set("auth.password")
		} else {
			c.Auth.Username = s
			c.set("auth.username")
		}
		return nil
	}), "auth", "")

	flags.Var((funcBoolVar)(func(b bool) error {
		c.SSL.Enabled = b
		c.set("ssl")
		c.set("ssl.enabled")
		return nil
	}), "ssl", "")

	flags.Var((funcBoolVar)(func(b bool) error {
		c.SSL.Verify = b
		c.set("ssl")
		c.set("ssl.verify")
		return nil
	}), "ssl-verify", "")

	flags.Var((funcVar)(func(s string) error {
		c.SSL.Cert = s
		c.set("ssl")
		c.set("ssl.cert")
		return nil
	}), "ssl-cert", "")

	flags.Var((funcVar)(func(s string) error {
		c.SSL.Key = s
		c.set("ssl")
		c.set("ssl.key")
		return nil
	}), "ssl-key", "")

	flags.Var((funcVar)(func(s string) error {
		c.SSL.CaCert = s
		c.set("ssl")
		c.set("ssl.ca_cert")
		return nil
	}), "ssl-ca-cert", "")

	flags.Var((funcVar)(func(s string) error {
		c.SSL.CaPath = s
		c.set("ssl")
		c.set("ssl.ca_path")
		return nil
	}), "ssl-ca-path", "")

	flags.Var((funcVar)(func(s string) error {
		c.SSL.ServerName = s
		c.set("ssl")
		c.set("ssl.server_name")
		return nil
	}), "ssl-server-name", "")

	flags.Var((funcDurationVar)(func(d time.Duration) error {
		c.MaxStale = d
		c.set("max_stale")
		return nil
	}), "max-stale", "")

	flags.Var((funcVar)(func(s string) error {
		p, err := ParsePrefix(s)
		if err != nil {
			return err
		}
		if c.Prefixes == nil {
			c.Prefixes = make([]*Prefix, 0, 1)
		}
		c.Prefixes = append(c.Prefixes, p)
		return nil
	}), "prefix", "")

	flags.Var((funcVar)(func(s string) error {
		if c.Excludes == nil {
			c.Excludes = make([]*Exclude, 0, 1)
		}
		c.Excludes = append(c.Excludes, &Exclude{Source: s})
		return nil
	}), "exclude", "")

  flags.Var((funcVar)(func(s string) error {
		if c.ExcludeMatches == nil {
			c.ExcludeMatches = make([]*ExcludeMatch, 0, 1)
		}
		c.ExcludeMatches = append(c.ExcludeMatches, &ExcludeMatch{Source: s})
		return nil
	}), "excludematch", "")

	flags.Var((funcBoolVar)(func(b bool) error {
		c.Syslog.Enabled = b
		c.set("syslog")
		c.set("syslog.enabled")
		return nil
	}), "syslog", "")

	flags.Var((funcVar)(func(s string) error {
		c.Syslog.Facility = s
		c.set("syslog.facility")
		return nil
	}), "syslog-facility", "")

	flags.Var((funcVar)(func(s string) error {
		w, err := config.ParseWaitConfig(s)
		if err != nil {
			return err
		}
		c.Wait.Min = w.Min
		c.Wait.Max = w.Max
		c.set("wait")
		return nil
	}), "wait", "")

	flags.Var((funcDurationVar)(func(d time.Duration) error {
		c.Retry = d
		c.set("retry")
		return nil
	}), "retry", "")

	flags.Var((funcVar)(func(s string) error {
		c.Path = s
		c.set("path")
		return nil
	}), "config", "")

	flags.Var((funcVar)(func(s string) error {
		c.PidFile = s
		c.set("pid_file")
		return nil
	}), "pid-file", "")

	flags.Var((funcVar)(func(s string) error {
		c.StatusDir = s
		c.set("status_dir")
		return nil
	}), "status-dir", "")

	flags.Var((funcVar)(func(s string) error {
		c.LogLevel = s
		c.set("log_level")
		return nil
	}), "log-level", "")

	flags.BoolVar(&once, "once", false, "")
	flags.BoolVar(&version, "v", false, "")
	flags.BoolVar(&version, "version", false, "")

	// If there was a parser error, stop
	if err := flags.Parse(args); err != nil {
		return nil, false, false, err
	}

	// Error if extra arguments are present
	args = flags.Args()
	if len(args) > 0 {
		return nil, false, false, fmt.Errorf("cli: extra argument(s): %q",
			args)
	}

	return c, once, version, nil
}

// handleError outputs the given error's Error() to the errStream and returns
// the given exit status.
func (cli *CLI) handleError(err error, status int) int {
	fmt.Fprintf(cli.errStream, "Consul Replicate returned errors:\n%s", err)
	return status
}

// setup initializes the CLI.
func (cli *CLI) setup(c *Config) (*Config, error) {
	if c.Path != "" {
		newConfig, err := ConfigFromPath(c.Path)
		if err != nil {
			return nil, err
		}

		// Merge ensuring that the CLI options still take precedence
		newConfig.Merge(c)
		c = newConfig
	}

	// Setup the logging
	if err := logging.Setup(&logging.Config{
		Name:           Name,
		Level:          c.LogLevel,
		Syslog:         c.Syslog.Enabled,
		SyslogFacility: c.Syslog.Facility,
		Writer:         cli.errStream,
	}); err != nil {
		return nil, err
	}

	return c, nil
}

const usage = `
Usage: %s [options]

  Replicates key-value data from a source datacenter to the datacenter(s) of a
  Consul agent.

Options:

  -auth=<user[:pass]>      Set the basic authentication username (and password)
  -consul=<address>        Sets the address of the Consul instance
  -token=<token>           Sets the Consul API token
  -max-stale=<duration>    Set the maximum staleness and allow stale queries to
                           Consul which will distribute work among all servers
                           instead of just the leader

  -ssl                     Use SSL when connecting to Consul
  -ssl-verify              Verify certificates when connecting via SSL
  -ssl-cert                SSL client certificate to send to server
  -ssl-key                 SSL/TLS private key for use in client authentication
                           key exchange
  -ssl-ca-cert             Validate server certificate against this CA
                           certificate file list
  -ssl-ca-path             Sets the path to the CA to use for TLS verification
  -ssl-server-name         Sets the name of the server to use when validating
                           TLS.


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
  -exclude=<src>           Provides a prefix to exclude from replication

  -excludematch=<src>      Provides a path match to exclude from replication

  -wait=<duration>         Sets the 'minumum(:maximum)' amount of time to wait
                           before replicating
  -retry=<duration>        The amount of time to wait if Consul returns an
                           error when communicating with the API

  -config=<path>           Sets the path to a configuration file on disk


  -pid-file=<path>         Path on disk to write the PID of the process
  -log-level=<level>       Set the logging level - valid values are "debug",
                           "info", "warn" (default), and "err"

  -once                    Do not run the process as a daemon

  -v, -version             Print the version of this daemon

Advanced Options:

  -status-dir=<path>       Sets the path in the KV store that is used to store
                           the replication status (default:
                           "service/consul-replicate/statuses")
`
