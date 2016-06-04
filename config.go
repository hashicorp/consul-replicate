package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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
	Auth *AuthConfig `mapstructure:"auth"`

	// PidFile is the path on disk where a PID file should be written containing
	// this processes PID.
	PidFile string `mapstructure:"pid_file"`

	// SSL indicates we should use a secure connection while talking to
	// Consul. This requires Consul to be configured to serve HTTPS.
	SSL *SSLConfig `mapstructure:"ssl"`

	// Syslog is the configuration for syslog.
	Syslog *SyslogConfig `mapstructure:"syslog"`

	// MaxStale is the maximum amount of time for staleness from Consul as given
	// by LastContact. If supplied, Consul Replicate will query all servers
	// instead of just the leader.
	MaxStale time.Duration `mapstructure:"max_stale"`

	// Retry is the duration of time to wait between Consul failures.
	Retry time.Duration `mapstructure:"retry"`

	// Wait is the quiescence timers.
	Wait *watch.Wait `mapstructure:"wait"`

	// LogLevel is the level with which to log for this config.
	LogLevel string `mapstructure:"log_level"`

	// StatusDir is the path in the KV store that is used to store the
	// replication statuses (default: "service/consul-replicate/statuses").
	StatusDir string `mapstructure:"status_dir"`

	// setKeys is the list of config keys that were set by the user.
	setKeys map[string]struct{}
}

// Copy returns a deep copy of the current configuration. This is useful because
// the nested data structures may be shared.
func (c *Config) Copy() *Config {
	config := new(Config)
	config.Path = c.Path
	config.Consul = c.Consul
	config.Token = c.Token

	if c.Auth != nil {
		config.Auth = &AuthConfig{
			Enabled:  c.Auth.Enabled,
			Username: c.Auth.Username,
			Password: c.Auth.Password,
		}
	}

	config.PidFile = c.PidFile

	if c.SSL != nil {
		config.SSL = &SSLConfig{
			Enabled: c.SSL.Enabled,
			Verify:  c.SSL.Verify,
			Cert:    c.SSL.Cert,
			Key:     c.SSL.Key,
			CaCert:  c.SSL.CaCert,
		}
	}

	if c.Syslog != nil {
		config.Syslog = &SyslogConfig{
			Enabled:  c.Syslog.Enabled,
			Facility: c.Syslog.Facility,
		}
	}

	config.MaxStale = c.MaxStale

	config.Prefixes = make([]*Prefix, len(c.Prefixes))
	for i, p := range c.Prefixes {
		config.Prefixes[i] = &Prefix{
			Source:      p.Source,
			SourceRaw:   p.SourceRaw,
			Destination: p.Destination,
		}
	}

	config.Retry = c.Retry

	if c.Wait != nil {
		config.Wait = &watch.Wait{
			Min: c.Wait.Min,
			Max: c.Wait.Max,
		}
	}

	config.LogLevel = c.LogLevel
	config.StatusDir = c.StatusDir

	config.setKeys = c.setKeys

	return config
}

// Merge merges the values in config into this config object. Values in the
// config object overwrite the values in c.
func (c *Config) Merge(config *Config) {
	if config.WasSet("path") {
		c.Path = config.Path
	}

	if config.WasSet("consul") {
		c.Consul = config.Consul
	}

	if config.WasSet("token") {
		c.Token = config.Token
	}

	if config.WasSet("auth") {
		if c.Auth == nil {
			c.Auth = &AuthConfig{}
		}
		if config.WasSet("auth.username") {
			c.Auth.Username = config.Auth.Username
			c.Auth.Enabled = true
		}
		if config.WasSet("auth.password") {
			c.Auth.Password = config.Auth.Password
			c.Auth.Enabled = true
		}
		if config.WasSet("auth.enabled") {
			c.Auth.Enabled = config.Auth.Enabled
		}
	}

	if config.WasSet("pid_file") {
		c.PidFile = config.PidFile
	}

	if config.WasSet("ssl") {
		if c.SSL == nil {
			c.SSL = &SSLConfig{}
		}
		if config.WasSet("ssl.verify") {
			c.SSL.Verify = config.SSL.Verify
			c.SSL.Enabled = true
		}
		if config.WasSet("ssl.cert") {
			c.SSL.Cert = config.SSL.Cert
			c.SSL.Enabled = true
		}
		if config.WasSet("ssl.key") {
			c.SSL.Key = config.SSL.Key
			c.SSL.Enabled = true
		}
		if config.WasSet("ssl.ca_cert") {
			c.SSL.CaCert = config.SSL.CaCert
			c.SSL.Enabled = true
		}
		if config.WasSet("ssl.enabled") {
			c.SSL.Enabled = config.SSL.Enabled
		}
	}

	if config.WasSet("syslog") {
		if c.Syslog == nil {
			c.Syslog = &SyslogConfig{}
		}
		if config.WasSet("syslog.facility") {
			c.Syslog.Facility = config.Syslog.Facility
			c.Syslog.Enabled = true
		}
		if config.WasSet("syslog.enabled") {
			c.Syslog.Enabled = config.Syslog.Enabled
		}
	}

	if config.WasSet("max_stale") {
		c.MaxStale = config.MaxStale
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

	if config.WasSet("retry") {
		c.Retry = config.Retry
	}

	if config.WasSet("wait") {
		c.Wait = &watch.Wait{
			Min: config.Wait.Min,
			Max: config.Wait.Max,
		}
	}

	if config.WasSet("log_level") {
		c.LogLevel = config.LogLevel
	}

	if config.WasSet("status_dir") {
		c.StatusDir = config.StatusDir
	}

	if c.setKeys == nil {
		c.setKeys = make(map[string]struct{})
	}

	for k := range config.setKeys {
		if _, ok := c.setKeys[k]; !ok {
			c.setKeys[k] = struct{}{}
		}
	}
}

// WasSet determines if the given key was set in the config (as opposed to just
// having the default value).
func (c *Config) WasSet(key string) bool {
	if _, ok := c.setKeys[key]; ok {
		return true
	}
	return false
}

// set is a helper function for marking a key as set.
func (c *Config) set(key string) {
	if _, ok := c.setKeys[key]; !ok {
		c.setKeys[key] = struct{}{}
	}
}

// g reads the configuration file at the given path and returns a new
// Config struct with the data populated.
func ParseConfig(path string) (*Config, error) {
	var errs *multierror.Error

	// Read the contents of the file
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading config at %q: %s", path, err)
	}

	// Parse the file (could be HCL or JSON)
	var shadow interface{}
	if err := hcl.Decode(&shadow, string(contents)); err != nil {
		return nil, fmt.Errorf("error decoding config at %q: %s", path, err)
	}

	// Convert to a map and flatten the keys we want to flatten
	parsed, ok := shadow.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("error converting config at %q", path)
	}
	flattenKeys(parsed, []string{"auth", "ssl", "syslog"})

	// Create a new, empty config
	config := new(Config)

	// Use mapstructure to populate the basic config fields
	metadata := new(mapstructure.Metadata)
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			watch.StringToWaitDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
			mapstructure.StringToTimeDurationHookFunc(),
		),
		ErrorUnused: true,
		Metadata:    metadata,
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

	// Update the list of set keys
	if config.setKeys == nil {
		config.setKeys = make(map[string]struct{})
	}
	for _, key := range metadata.Keys {
		if _, ok := config.setKeys[key]; !ok {
			config.setKeys[key] = struct{}{}
		}
	}
	config.setKeys["path"] = struct{}{}

	d := DefaultConfig()
	d.Merge(config)
	config = d

	return config, errs.ErrorOrNil()
}

// DefaultConfig returns the default configuration struct.
func DefaultConfig() *Config {
	logLevel := os.Getenv("CONSUL_REPLICATE_LOG")
	if logLevel == "" {
		logLevel = "WARN"
	}

	return &Config{
		Auth: &AuthConfig{
			Enabled: false,
		},
		SSL: &SSLConfig{
			Enabled: false,
			Verify:  true,
		},
		Syslog: &SyslogConfig{
			Enabled:  false,
			Facility: "LOCAL0",
		},
		LogLevel:  logLevel,
		Prefixes:  []*Prefix{},
		Retry:     5 * time.Second,
		StatusDir: "service/consul-replicate/statuses",
		Wait: &watch.Wait{
			Min: 150 * time.Millisecond,
			Max: 400 * time.Millisecond,
		},
		setKeys: make(map[string]struct{}),
	}
}

// ConfigFromPath iterates and merges all configuration files in a given
// directory, returning the resulting config.
func ConfigFromPath(path string) (*Config, error) {
	// Ensure the given filepath exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config: missing file/folder: %s", path)
	}

	// Check if a file was given or a path to a directory
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("config: error stating file: %s", err)
	}

	// Recursively parse directories, single load files
	if stat.Mode().IsDir() {
		// Ensure the given filepath has at least one config file
		_, err := ioutil.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("config: error listing directory: %s", err)
		}

		// Create a blank config to merge off of
		config := DefaultConfig()

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
			return nil, fmt.Errorf("config: walk error: %s", err)
		}

		return config, nil
	} else if stat.Mode().IsRegular() {
		return ParseConfig(path)
	}

	return nil, fmt.Errorf("config: unknown filetype: %q", stat.Mode().String())
}

// AuthConfig is the HTTP basic authentication data.
type AuthConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

// SSLConfig is the configuration for SSL.
type SSLConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Verify  bool   `mapstructure:"verify"`
	Cert    string `mapstructure:"cert"`
	Key     string `mapstructure:"key"`
	CaCert  string `mapstructure:"ca_cert"`
}

// SyslogConfig is the configuration for syslog.
type SyslogConfig struct {
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
	// ensure destination prefix ends with "/"
	destination = strings.TrimSuffix(destination, "/") + "/"

	return &Prefix{
		Source:      source,
		SourceRaw:   sourceRaw,
		Destination: destination,
	}, nil
}

// flattenKeys is a function that takes a map[string]interface{} and recursively
// flattens any keys that are a []map[string]interface{} where the key is in the
// given list of keys.
func flattenKeys(m map[string]interface{}, keys []string) {
	keyMap := make(map[string]struct{})
	for _, key := range keys {
		keyMap[key] = struct{}{}
	}

	var flatten func(map[string]interface{})
	flatten = func(m map[string]interface{}) {
		for k, v := range m {
			if _, ok := keyMap[k]; !ok {
				continue
			}

			switch typed := v.(type) {
			case []map[string]interface{}:
				if len(typed) > 0 {
					last := typed[len(typed)-1]
					flatten(last)
					m[k] = last
				} else {
					m[k] = nil
				}
			case map[string]interface{}:
				flatten(typed)
				m[k] = typed
			default:
				m[k] = v
			}
		}
	}

	flatten(m)
}
