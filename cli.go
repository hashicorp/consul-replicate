// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/hashicorp/consul-replicate/version"
	"github.com/hashicorp/consul-template/config"
	"github.com/hashicorp/consul-template/logging"
	"github.com/hashicorp/consul-template/manager"
	"github.com/hashicorp/consul-template/signals"
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

	// signalCh is the channel where the cli receives signals.
	signalCh chan os.Signal

	// stopCh is an internal channel used to trigger a shutdown of the CLI.
	stopCh chan struct{}
}

func NewCLI(out, err io.Writer) *CLI {
	return &CLI{
		outStream: out,
		errStream: err,
		signalCh:  make(chan os.Signal, 1),
		stopCh:    make(chan struct{}),
	}
}

// Run accepts a slice of arguments and returns an int representing the exit
// status from the command.
func (cli *CLI) Run(args []string) int {
	// Parse the flags and args
	cfg, paths, once, isVersion, err := cli.ParseFlags(args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintf(cli.errStream, usage, version.Name)
			return 0
		}
		fmt.Fprintln(cli.errStream, err.Error())
		return ExitCodeParseFlagsError
	}

	// Save original config (defaults + parsed flags) for handling reloads
	cliConfig := cfg.Copy()

	// Load configuration paths, with CLI taking precendence
	cfg, err = loadConfigs(paths, cliConfig)
	if err != nil {
		return logError(err, ExitCodeConfigError)
	}

	cfg.Finalize()

	// Setup the config and logging
	cfg, err = cli.setup(cfg)
	if err != nil {
		return logError(err, ExitCodeConfigError)
	}

	// Print version information for debugging
	log.Printf("[INFO] %s", version.HumanVersion)

	// If the version was requested, return an "error" containing the version
	// information. This might sound weird, but most *nix applications actually
	// print their version on stderr anyway.
	if isVersion {
		log.Printf("[DEBUG] (cli) version flag was given, exiting now")
		fmt.Fprintf(cli.errStream, "%s\n", version.HumanVersion)
		return ExitCodeOK
	}

	// Initial runner
	runner, err := NewRunner(cfg, once)
	if err != nil {
		return logError(err, ExitCodeRunnerError)
	}
	go runner.Start()

	// Listen for signals
	signal.Notify(cli.signalCh)

	for {
		select {
		case err := <-runner.ErrCh:
			// Check if the runner's error returned a specific exit status, and return
			// that value. If no value was given, return a generic exit status.
			code := ExitCodeRunnerError
			if typed, ok := err.(manager.ErrExitable); ok {
				code = typed.ExitStatus()
			}
			return logError(err, code)
		case <-runner.DoneCh:
			return ExitCodeOK
		case s := <-cli.signalCh:
			log.Printf("[DEBUG] (cli) receiving signal %q", s)

			switch s {
			case *cfg.ReloadSignal:
				fmt.Fprintf(cli.errStream, "Reloading configuration...\n")
				runner.Stop()

				// Re-parse any configuration files or paths
				cfg, err = loadConfigs(paths, cliConfig)
				if err != nil {
					return logError(err, ExitCodeConfigError)
				}
				cfg.Finalize()

				// Load the new configuration from disk
				cfg, err = cli.setup(cfg)
				if err != nil {
					return logError(err, ExitCodeConfigError)
				}

				runner, err = NewRunner(cfg, once)
				if err != nil {
					return logError(err, ExitCodeRunnerError)
				}
				go runner.Start()
			case *cfg.KillSignal:
				fmt.Fprintf(cli.errStream, "Cleaning up...\n")
				runner.Stop()
				return ExitCodeInterrupt
			case signals.SignalLookup["SIGCHLD"]:
				// The SIGCHLD signal is sent to the parent of a child process when it
				// exits, is interrupted, or resumes after being interrupted. We ignore
				// this signal because the child process is monitored on its own.
				//
				// Also, the reason we do a lookup instead of a direct syscall.SIGCHLD
				// is because that isn't defined on Windows.
			default:
				// Do nothing
			}
		case <-cli.stopCh:
			return ExitCodeOK
		}
	}
}

// ParseFlags is a helper function for parsing command line flags using Go's
// Flag library. This is extracted into a helper to keep the main function
// small, but it also makes writing tests for parsing command line arguments
// much easier and cleaner.
func (cli *CLI) ParseFlags(args []string) (*Config, []string, bool, bool, error) {
	var once, isVersion bool
	var c = DefaultConfig()

	// configPaths stores the list of configuration paths on disk
	configPaths := make([]string, 0, 6)

	// Parse the flags and options
	flags := flag.NewFlagSet(version.Name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	flags.Var((funcVar)(func(s string) error {
		configPaths = append(configPaths, s)
		return nil
	}), "config", "")

	flags.Var((funcVar)(func(s string) error {
		c.Consul.Address = config.String(s)
		return nil
	}), "consul-addr", "")

	flags.Var((funcVar)(func(s string) error {
		a, err := config.ParseAuthConfig(s)
		if err != nil {
			return err
		}
		c.Consul.Auth = a
		return nil
	}), "consul-auth", "")

	flags.Var((funcBoolVar)(func(b bool) error {
		c.Consul.Retry.Enabled = config.Bool(b)
		return nil
	}), "consul-retry", "")

	flags.Var((funcIntVar)(func(i int) error {
		c.Consul.Retry.Attempts = config.Int(i)
		return nil
	}), "consul-retry-attempts", "")

	flags.Var((funcDurationVar)(func(d time.Duration) error {
		c.Consul.Retry.Backoff = config.TimeDuration(d)
		return nil
	}), "consul-retry-backoff", "")

	flags.Var((funcDurationVar)(func(d time.Duration) error {
		c.Consul.Retry.MaxBackoff = config.TimeDuration(d)
		return nil
	}), "consul-retry-max-backoff", "")

	flags.Var((funcBoolVar)(func(b bool) error {
		c.Consul.SSL.Enabled = config.Bool(b)
		return nil
	}), "consul-ssl", "")

	flags.Var((funcVar)(func(s string) error {
		c.Consul.SSL.CaCert = config.String(s)
		return nil
	}), "consul-ssl-ca-cert", "")

	flags.Var((funcVar)(func(s string) error {
		c.Consul.SSL.CaPath = config.String(s)
		return nil
	}), "consul-ssl-ca-path", "")

	flags.Var((funcVar)(func(s string) error {
		c.Consul.SSL.Cert = config.String(s)
		return nil
	}), "consul-ssl-cert", "")

	flags.Var((funcVar)(func(s string) error {
		c.Consul.SSL.Key = config.String(s)
		return nil
	}), "consul-ssl-key", "")

	flags.Var((funcVar)(func(s string) error {
		c.Consul.SSL.ServerName = config.String(s)
		return nil
	}), "consul-ssl-server-name", "")

	flags.Var((funcBoolVar)(func(b bool) error {
		c.Consul.SSL.Verify = config.Bool(b)
		return nil
	}), "consul-ssl-verify", "")

	flags.Var((funcVar)(func(s string) error {
		c.Consul.Token = config.String(s)
		return nil
	}), "consul-token", "")

	flags.Var((funcDurationVar)(func(d time.Duration) error {
		c.Consul.Transport.DialKeepAlive = config.TimeDuration(d)
		return nil
	}), "consul-transport-dial-keep-alive", "")

	flags.Var((funcDurationVar)(func(d time.Duration) error {
		c.Consul.Transport.DialTimeout = config.TimeDuration(d)
		return nil
	}), "consul-transport-dial-timeout", "")

	flags.Var((funcBoolVar)(func(b bool) error {
		c.Consul.Transport.DisableKeepAlives = config.Bool(b)
		return nil
	}), "consul-transport-disable-keep-alives", "")

	flags.Var((funcIntVar)(func(i int) error {
		c.Consul.Transport.MaxIdleConnsPerHost = config.Int(i)
		return nil
	}), "consul-transport-max-idle-conns-per-host", "")

	flags.Var((funcDurationVar)(func(d time.Duration) error {
		c.Consul.Transport.TLSHandshakeTimeout = config.TimeDuration(d)
		return nil
	}), "consul-transport-tls-handshake-timeout", "")

	flags.Var((funcVar)(func(s string) error {
		e, err := ParseExcludeConfig(s)
		if err != nil {
			return err
		}
		*c.Excludes = append(*c.Excludes, e)
		return nil
	}), "exclude", "")

	flags.Var((funcVar)(func(s string) error {
		sig, err := signals.Parse(s)
		if err != nil {
			return err
		}
		c.KillSignal = config.Signal(sig)
		return nil
	}), "kill-signal", "")

	flags.Var((funcVar)(func(s string) error {
		c.LogLevel = config.String(s)
		return nil
	}), "log-level", "")

	flags.Var((funcDurationVar)(func(d time.Duration) error {
		c.MaxStale = config.TimeDuration(d)
		return nil
	}), "max-stale", "")

	flags.BoolVar(&once, "once", false, "")

	flags.Var((funcVar)(func(s string) error {
		c.PidFile = config.String(s)
		return nil
	}), "pid-file", "")

	flags.Var((funcVar)(func(s string) error {
		p, err := ParsePrefixConfig(s)
		if err != nil {
			return err
		}
		*c.Prefixes = append(*c.Prefixes, p)
		return nil
	}), "prefix", "")

	flags.Var((funcVar)(func(s string) error {
		sig, err := signals.Parse(s)
		if err != nil {
			return err
		}
		c.ReloadSignal = config.Signal(sig)
		return nil
	}), "reload-signal", "")

	flags.Var((funcVar)(func(s string) error {
		c.StatusDir = config.String(s)
		return nil
	}), "status-dir", "")

	flags.Var((funcBoolVar)(func(b bool) error {
		c.Syslog.Enabled = config.Bool(b)
		return nil
	}), "syslog", "")

	flags.Var((funcVar)(func(s string) error {
		c.Syslog.Facility = config.String(s)
		return nil
	}), "syslog-facility", "")

	flags.Var((funcVar)(func(s string) error {
		w, err := config.ParseWaitConfig(s)
		if err != nil {
			return err
		}
		c.Wait = w
		return nil
	}), "wait", "")

	flags.Var((funcBoolVar)(func(b bool) error {
		c.DeleteKey = config.Bool(b)
		return nil
	}), "delete", "")

	flags.BoolVar(&isVersion, "v", false, "")
	flags.BoolVar(&isVersion, "version", false, "")

	// Deprecations
	// TODO remove in 0.5.0
	flags.Var((funcVar)(func(s string) error {
		log.Printf("[WARN] -auth is now -consul-auth")
		a, err := config.ParseAuthConfig(s)
		if err != nil {
			return err
		}
		c.Consul.Auth = a
		return nil
	}), "auth", "")
	flags.Var((funcVar)(func(s string) error {
		log.Printf("[WARN] -consul is now -consul-addr")
		c.Consul.Address = config.String(s)
		return nil
	}), "consul", "")
	flags.Var((funcDurationVar)(func(d time.Duration) error {
		log.Printf("[WARN] -retry is now -consul-retry-*")
		c.Consul.Retry.Backoff = config.TimeDuration(d)
		c.Consul.Retry.MaxBackoff = config.TimeDuration(d)
		return nil
	}), "retry", "")
	flags.Var((funcBoolVar)(func(b bool) error {
		log.Printf("[WARN] -ssl is now -consul-ssl-*")
		c.Consul.SSL.Enabled = config.Bool(b)
		return nil
	}), "ssl", "")
	flags.Var((funcBoolVar)(func(b bool) error {
		log.Printf("[WARN] -ssl-verify is now -consul-ssl-verify")
		c.Consul.SSL.Verify = config.Bool(b)
		return nil
	}), "ssl-verify", "")
	flags.Var((funcVar)(func(s string) error {
		log.Printf("[WARN] -ssl-ca-cert is now -consul-ssl-ca-cert")
		c.Consul.SSL.CaCert = config.String(s)
		return nil
	}), "ssl-ca-cert", "")
	flags.Var((funcVar)(func(s string) error {
		log.Printf("[WARN] -ssl-ca-path is now -consul-ssl-ca-path")
		c.Consul.SSL.CaPath = config.String(s)
		return nil
	}), "ssl-ca-path", "")
	flags.Var((funcVar)(func(s string) error {
		log.Printf("[WARN] -ssl-cert is now -consul-ssl-cert")
		c.Consul.SSL.Cert = config.String(s)
		return nil
	}), "ssl-cert", "")
	flags.Var((funcVar)(func(s string) error {
		log.Printf("[WARN] -ssl-server-name is now -consul-ssl-server-name")
		c.Consul.SSL.ServerName = config.String(s)
		return nil
	}), "ssl-server-name", "")
	flags.Var((funcVar)(func(s string) error {
		log.Printf("[WARN] -token is now -consul-token")
		c.Consul.Token = config.String(s)
		return nil
	}), "token", "")
	// End deprecations
	// TODO remove in 0.5.0

	// If there was a parser error, stop
	if err := flags.Parse(args); err != nil {
		return nil, nil, false, false, err
	}

	// Error if extra arguments are present
	args = flags.Args()
	if len(args) > 0 {
		return nil, nil, false, false, fmt.Errorf("cli: extra argument(s): %q",
			args)
	}

	return c, configPaths, once, isVersion, nil
}

// handleError outputs the given error's Error() to the errStream and returns
// loadConfigs loads the configuration from the list of paths. The optional
// configuration is the list of overrides to apply at the very end, taking
// precendence over any configurations that were loaded from the paths. If any
// errors occur when reading or parsing those sub-configs, it is returned.
func loadConfigs(paths []string, o *Config) (*Config, error) {
	finalC := DefaultConfig()

	for _, path := range paths {
		c, err := FromPath(path)
		if err != nil {
			return nil, err
		}

		finalC = finalC.Merge(c)
	}

	finalC = finalC.Merge(o)
	finalC.Finalize()
	return finalC, nil
}

// logError logs an error message and then returns the given status.
func logError(err error, status int) int {
	log.Printf("[ERR] (cli) %s", err)
	return status
}

func (cli *CLI) setup(conf *Config) (*Config, error) {
	if err := logging.Setup(&logging.Config{
		SyslogName:     version.Name,
		Level:          config.StringVal(conf.LogLevel),
		Syslog:         config.BoolVal(conf.Syslog.Enabled),
		SyslogFacility: config.StringVal(conf.Syslog.Facility),
		Writer:         cli.errStream,
	}); err != nil {
		return nil, err
	}

	return conf, nil
}

const usage = `Usage: %s [options]

  Replicates key-value data from a source datacenter to the datacenter(s) of a
  Consul agent.

Options:

  -config=<path>
      Sets the path to a configuration file or folder on disk. This can be
      specified multiple times to load multiple files or folders. If multiple
      values are given, they are merged left-to-right, and CLI arguments take
      the top-most precedence.

  -consul-addr=<address>
      Sets the address of the Consul instance

  -consul-auth=<username[:password]>
      Set the basic authentication username and password for communicating
      with Consul.

  -consul-retry
      Use retry logic when communication with Consul fails

  -consul-retry-attempts=<int>
      The number of attempts to use when retrying failed communications

  -consul-retry-backoff=<duration>
      The base amount to use for the backoff duration. This number will be
      increased exponentially for each retry attempt.

  -consul-retry-max-backoff=<duration>
      The maximum limit of the retry backoff duration. Default is one minute.
      0 means infinite. The backoff will increase exponentially until given value.

  -consul-ssl
      Use SSL when connecting to Consul

  -consul-ssl-ca-cert=<string>
      Validate server certificate against this CA certificate file list

  -consul-ssl-ca-path=<string>
      Sets the path to the CA to use for TLS verification

  -consul-ssl-cert=<string>
      SSL client certificate to send to server

  -consul-ssl-key=<string>
      SSL/TLS private key for use in client authentication key exchange

  -consul-ssl-server-name=<string>
      Sets the name of the server to use when validating TLS.

  -consul-ssl-verify
      Verify certificates when connecting via SSL

  -consul-token=<token>
      Sets the Consul API token

  -consul-transport-dial-keep-alive=<duration>
      Sets the amount of time to use for keep-alives

  -consul-transport-dial-timeout=<duration>
      Sets the amount of time to wait to establish a connection

  -consul-transport-disable-keep-alives
      Disables keep-alives (this will impact performance)

  -consul-transport-max-idle-conns-per-host=<int>
      Sets the maximum number of idle connections to permit per host

  -consul-transport-tls-handshake-timeout=<duration>
      Sets the handshake timeout

  -exclude=<src>
      Provides a prefix to exclude from replication.

  -kill-signal=<signal>
      Signal to listen to gracefully terminate the process

  -log-level=<level>
      Set the logging level - values are "debug", "info", "warn", and "err"

  -max-stale=<duration>
      Set the maximum staleness and allow stale queries to Consul which will
      distribute work among all servers instead of just the leader

  -once
      Do not run the process as a daemon

  -pid-file=<path>
      Path on disk to write the PID of the process

  -prefix=<prefix>
      Provides the source prefix in the replicating datacenter and optionally
      the destination prefix in the destination datacenters. If the destination
      is omitted, it is assumed to be the same as the source.

  -reload-signal=<signal>
      Signal to listen to reload configuration

  -status-dir=<path>
      Sets the path in the KV store that is used to store the replication
      status, which defaults to "service/consul-replicate/statuses".

  -syslog
      Send the output to syslog instead of standard error and standard out. The
      syslog facility defaults to LOCAL0 and can be changed using a
      configuration file

  -syslog-facility=<facility>
      Set the facility where syslog should log - if this attribute is supplied,
      the -syslog flag must also be supplied

  -wait=<duration>
      Sets the 'min(:max)' amount of time to wait before writing a template (and
      triggering a command)

  -delete=<boolean>
      Enables deletion of keys in the destination datacenter that do not exist
      in the source datacenter. Defaults to true

  -v, -version
      Print the version of this daemon
`
