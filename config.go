package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
	"time"

	dep "github.com/hashicorp/consul-template/dependency"
	"github.com/hashicorp/consul-template/watch"
	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/hcl"
	"github.com/mitchellh/mapstructure"
)

// Config is used to configure Consul Replicate
type Config struct {
	// Path is the path to this configuration file on disk. This value is not
	// read from disk by rather dynamically populated by the code so the Config
	// has a reference to the path to the file on disk that created it.
	Path string `mapstructure:"-"`

	// Consul is the location of the Consul instance to query (may be an IP
	// address or FQDN) with port.
	Consul string `mapstructure:"consul"`

	// Token is the Consul API token.
	Token string `mapstructure:"token"`

	// Prefixes is the list of key prefix dependencies.
	Prefixes []*Prefix `mapstructure:"prefix"`

	// Auth is the HTTP basic authentication for communicating with Consul.
	Auth    *Auth   `mapstructure:"-"`
	AuthRaw []*Auth `mapstructure:"auth"`

	// SSL indicates we should use a secure connection while talking to
	// Consul. This requires Consul to be configured to serve HTTPS.
	SSL    *SSL   `mapstructure:"-"`
	SSLRaw []*SSL `mapstructure:"ssl"`

	// Syslog is the configuration for syslog.
	Syslog    *Syslog   `mapstructure:"-"`
	SyslogRaw []*Syslog `mapstructure:"syslog"`

	// MaxStale is the maximum amount of time for staleness from Consul as given
	// by LastContact. If supplied, Consul Replicate will query all servers
	// instead of just the leader.
	MaxStale    time.Duration `mapstructure:"-"`
	MaxStaleRaw string        `mapstructure:"max_stale"`

	// Retry is the duration of time to wait between Consul failures.
	Retry    time.Duration `mapstructure:"-"`
	RetryRaw string        `mapstructure:"retry"`

	// Wait is the quiescence timers.
	Wait    *watch.Wait `mapstructure:"-"`
	WaitRaw string      `mapstructure:"wait"`

	// LogLevel is the level with which to log for this config.
	LogLevel string `mapstructure:"log_level"`

	// StatusDir is the path in the KV store that is used to store the
	// replication statuses (default: "service/consul-replicate/statuses").
	StatusDir string `mapstructure:"status_path"`
}

// Merge merges the values in config into this config object. Values in the
// config object overwrite the values in c.
func (c *Config) Merge(config *Config) {
	if config.Consul != "" {
		c.Consul = config.Consul
	}

	if config.Token != "" {
		c.Token = config.Token
	}

	if config.Prefixes != nil {
		if c.Prefixes == nil {
			c.Prefixes = make([]*Prefix, 0)
		}

		for _, prefix := range config.Prefixes {
			c.Prefixes = append(c.Prefixes, &Prefix{
				Source:      prefix.Source,
				SourceRaw:   prefix.SourceRaw,
				Destination: prefix.Destination,
			})
		}
	}

	if config.Auth != nil {
		c.Auth = &Auth{
			Enabled:  config.Auth.Enabled,
			Username: config.Auth.Username,
			Password: config.Auth.Password,
		}
	}

	if config.SSL != nil {
		c.SSL = &SSL{
			Enabled: config.SSL.Enabled,
			Verify:  config.SSL.Verify,
		}
	}

	if config.Syslog != nil {
		c.Syslog = &Syslog{
			Enabled:  config.Syslog.Enabled,
			Facility: config.Syslog.Facility,
		}
	}

	if config.MaxStale != 0 {
		c.MaxStale = config.MaxStale
		c.MaxStaleRaw = config.MaxStaleRaw
	}

	if config.Retry != 0 {
		c.Retry = config.Retry
		c.RetryRaw = config.RetryRaw
	}

	if config.Wait != nil {
		c.Wait = &watch.Wait{
			Min: config.Wait.Min,
			Max: config.Wait.Max,
		}
		c.WaitRaw = config.WaitRaw
	}

	if config.LogLevel != "" {
		c.LogLevel = config.LogLevel
	}

	if config.StatusDir != "" {
		c.StatusDir = config.StatusDir
	}
}

// ParseConfig reads the configuration file at the given path and returns a new
// Config struct with the data populated.
func ParseConfig(path string) (*Config, error) {
	var errs *multierror.Error

	// Read the contents of the file
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		errs = multierror.Append(errs, err)
		return nil, errs.ErrorOrNil()
	}

	// Parse the file (could be HCL or JSON)
	var parsed interface{}
	if err := hcl.Decode(&parsed, string(contents)); err != nil {
		errs = multierror.Append(errs, err)
		return nil, errs.ErrorOrNil()
	}

	// Create a new, empty config
	config := &Config{}

	// Use mapstructure to populate the basic config fields
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		ErrorUnused: true,
		Metadata:    nil,
		Result:      config,
	})
	if err != nil {
		errs = multierror.Append(errs, err)
		return nil, errs.ErrorOrNil()
	}

	if err := decoder.Decode(parsed); err != nil {
		errs = multierror.Append(errs, err)
		return nil, errs.ErrorOrNil()
	}

	// Store a reference to the path where this config was read from
	config.Path = path

	// Parse the prefix sources
	for _, prefix := range config.Prefixes {
		parsed, err := dep.ParseStoreKeyPrefix(prefix.SourceRaw)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		prefix.Source = parsed

		// If no destination was given, default to the prefix
		if prefix.Destination == "" {
			prefix.Destination = parsed.Prefix
		}
	}

	// Parse the MaxStale component
	if raw := config.MaxStaleRaw; raw != "" {
		stale, err := time.ParseDuration(raw)

		if err == nil {
			config.MaxStale = stale
		} else {
			errs = multierror.Append(errs, fmt.Errorf("max_stale invalid: %v", err))
		}
	}

	// Extract the last Auth block
	if len(config.AuthRaw) > 0 {
		config.Auth = config.AuthRaw[len(config.AuthRaw)-1]
	}

	// Extract the last SSL block
	if len(config.SSLRaw) > 0 {
		config.SSL = config.SSLRaw[len(config.SSLRaw)-1]
	}

	// Extract the last Syslog block
	if len(config.SyslogRaw) > 0 {
		config.Syslog = config.SyslogRaw[len(config.SyslogRaw)-1]
	}

	// Parse the Retry component
	if raw := config.RetryRaw; raw != "" {
		retry, err := time.ParseDuration(raw)

		if err == nil {
			config.Retry = retry
		} else {
			errs = multierror.Append(errs, fmt.Errorf("retry invalid: %v", err))
		}
	}

	// Parse the Wait component
	if raw := config.WaitRaw; raw != "" {
		wait, err := watch.ParseWait(raw)

		if err == nil {
			config.Wait = wait
		} else {
			errs = multierror.Append(errs, fmt.Errorf("wait invalid: %v", err))
		}
	}

	return config, errs.ErrorOrNil()
}

// DefaultConfig returns the default configuration struct.
func DefaultConfig() *Config {
	logLevel := os.Getenv("CONSUL_REPLICATE_LOG")
	if logLevel == "" {
		logLevel = "WARN"
	}

	return &Config{
		Auth: &Auth{
			Enabled: false,
		},
		SSL: &SSL{
			Enabled: false,
			Verify:  true,
		},
		Syslog: &Syslog{
			Enabled:  false,
			Facility: "LOCAL0",
		},
		Prefixes: []*Prefix{},
		Retry:    5 * time.Second,
		Wait: &watch.Wait{
			Min: 150 * time.Millisecond,
			Max: 400 * time.Millisecond,
		},
		LogLevel:  logLevel,
		StatusDir: "service/consul-replicate/statuses",
	}
}

// Auth is the HTTP basic authentication data.
type Auth struct {
	Enabled  bool   `mapstructure:"enabled"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

// SSL is the configuration for SSL.
type SSL struct {
	Enabled bool `mapstructure:"enabled"`
	Verify  bool `mapstructure:"verify"`
}

// Syslog is the configuration for syslog.
type Syslog struct {
	Enabled  bool   `mapstructure:"enabled"`
	Facility string `mapstructure:"facility"`
}

// The pattern to split the prefix syntax on
var prefixRe = regexp.MustCompile("([a-zA-Z]:)?([^:]+)")

// Prefix is the representation of a key prefix.
type Prefix struct {
	Source      *dep.StoreKeyPrefix `mapstructure:"-"`
	SourceRaw   string              `mapstructure:"source"`
	Destination string              `mapstructure:"destination"`
}

// ParsePrefix parses a prefix of the format "source@dc:destination" into the
// Prefix component.
func ParsePrefix(s string) (*Prefix, error) {
	if len(strings.TrimSpace(s)) < 1 {
		return nil, fmt.Errorf("cannot specify empty prefix declaration")
	}

	var sourceRaw, destination string
	parts := prefixRe.FindAllString(s, -1)

	switch len(parts) {
	case 1:
		sourceRaw = parts[0]
	case 2:
		sourceRaw, destination = parts[0], parts[1]
	default:
		return nil, fmt.Errorf("invalid prefix declaration format")
	}

	source, err := dep.ParseStoreKeyPrefix(sourceRaw)
	if err != nil {
		return nil, err
	}

	if destination == "" {
		destination = source.Prefix
	}

	return &Prefix{
		Source:      source,
		SourceRaw:   sourceRaw,
		Destination: destination,
	}, nil
}
