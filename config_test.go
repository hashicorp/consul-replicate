// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/hashicorp/consul-template/config"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		i    string
		e    *Config
		err  bool
	}{
		// Deprecations
		// TODO: remove this in 0.5.0
		{
			"auth",
			`auth {
				enabled  = true
				username = "foo"
				password = "bar"
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Auth: &config.AuthConfig{
						Enabled:  config.Bool(true),
						Username: config.String("foo"),
						Password: config.String("bar"),
					},
				},
			},
			false,
		},
		{
			"consul_top_level",
			`consul = "127.0.0.1:8500"`,
			&Config{
				Consul: &config.ConsulConfig{
					Address: config.String("127.0.0.1:8500"),
				},
			},
			false,
		},
		{
			"path_top_level",
			`path = "/foo/bar"`,
			&Config{},
			false,
		},
		{
			"retry_top_level",
			`retry = "5s"`,
			&Config{
				Consul: &config.ConsulConfig{
					Retry: &config.RetryConfig{
						Backoff:    config.TimeDuration(5 * time.Second),
						MaxBackoff: config.TimeDuration(5 * time.Second),
					},
				},
			},
			false,
		},
		{
			"retry_top_level_int",
			`retry = 5`,
			&Config{
				Consul: &config.ConsulConfig{
					Retry: &config.RetryConfig{
						Backoff:    config.TimeDuration(5 * time.Nanosecond),
						MaxBackoff: config.TimeDuration(5 * time.Nanosecond),
					},
				},
			},
			false,
		},
		{
			"ssl",
			`ssl {
				enabled = true
				verify  = false
				cert    = "cert"
				key     = "key"
				ca_cert = "ca_cert"
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					SSL: &config.SSLConfig{
						Enabled: config.Bool(true),
						Verify:  config.Bool(false),
						CaCert:  config.String("ca_cert"),
						Cert:    config.String("cert"),
						Key:     config.String("key"),
					},
				},
			},
			false,
		},
		{
			"token_top_level",
			`token = "abcd1234"`,
			&Config{
				Consul: &config.ConsulConfig{
					Token: config.String("abcd1234"),
				},
			},
			false,
		},
		// End Depreations
		// TODO remove in 0.5.0

		{
			"consul_address",
			`consul {
				address = "1.2.3.4"
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Address: config.String("1.2.3.4"),
				},
			},
			false,
		},
		{
			"consul_auth",
			`consul {
				auth {
					username = "username"
					password = "password"
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Auth: &config.AuthConfig{
						Username: config.String("username"),
						Password: config.String("password"),
					},
				},
			},
			false,
		},
		{
			"consul_retry",
			`consul {
				retry {
					backoff  = "2s"
					attempts = 10
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Retry: &config.RetryConfig{
						Attempts: config.Int(10),
						Backoff:  config.TimeDuration(2 * time.Second),
					},
				},
			},
			false,
		},
		{
			"consul_ssl",
			`consul {
				ssl {}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					SSL: &config.SSLConfig{},
				},
			},
			false,
		},
		{
			"consul_ssl_enabled",
			`consul {
				ssl {
					enabled = true
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					SSL: &config.SSLConfig{
						Enabled: config.Bool(true),
					},
				},
			},
			false,
		},
		{
			"consul_ssl_verify",
			`consul {
				ssl {
					verify = true
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					SSL: &config.SSLConfig{
						Verify: config.Bool(true),
					},
				},
			},
			false,
		},
		{
			"consul_ssl_cert",
			`consul {
				ssl {
					cert = "cert"
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					SSL: &config.SSLConfig{
						Cert: config.String("cert"),
					},
				},
			},
			false,
		},
		{
			"consul_ssl_key",
			`consul {
				ssl {
					key = "key"
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					SSL: &config.SSLConfig{
						Key: config.String("key"),
					},
				},
			},
			false,
		},
		{
			"consul_ssl_ca_cert",
			`consul {
				ssl {
					ca_cert = "ca_cert"
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					SSL: &config.SSLConfig{
						CaCert: config.String("ca_cert"),
					},
				},
			},
			false,
		},
		{
			"consul_ssl_ca_path",
			`consul {
				ssl {
					ca_path = "ca_path"
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					SSL: &config.SSLConfig{
						CaPath: config.String("ca_path"),
					},
				},
			},
			false,
		},
		{
			"consul_ssl_server_name",
			`consul {
				ssl {
					server_name = "server_name"
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					SSL: &config.SSLConfig{
						ServerName: config.String("server_name"),
					},
				},
			},
			false,
		},
		{
			"consul_token",
			`consul {
				token = "token"
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Token: config.String("token"),
				},
			},
			false,
		},
		{
			"consul_transport_dial_keep_alive",
			`consul {
				transport {
					dial_keep_alive = "10s"
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Transport: &config.TransportConfig{
						DialKeepAlive: config.TimeDuration(10 * time.Second),
					},
				},
			},
			false,
		},
		{
			"consul_transport_dial_timeout",
			`consul {
				transport {
					dial_timeout = "10s"
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Transport: &config.TransportConfig{
						DialTimeout: config.TimeDuration(10 * time.Second),
					},
				},
			},
			false,
		},
		{
			"consul_transport_disable_keep_alives",
			`consul {
				transport {
					disable_keep_alives = true
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Transport: &config.TransportConfig{
						DisableKeepAlives: config.Bool(true),
					},
				},
			},
			false,
		},
		{
			"consul_transport_max_idle_conns_per_host",
			`consul {
				transport {
					max_idle_conns_per_host = 100
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Transport: &config.TransportConfig{
						MaxIdleConnsPerHost: config.Int(100),
					},
				},
			},
			false,
		},
		{
			"consul_transport_tls_handshake_timeout",
			`consul {
				transport {
					tls_handshake_timeout = "30s"
				}
			}`,
			&Config{
				Consul: &config.ConsulConfig{
					Transport: &config.TransportConfig{
						TLSHandshakeTimeout: config.TimeDuration(30 * time.Second),
					},
				},
			},
			false,
		},
		{
			"exclude",
			`exclude {
				source = "foo/bar"
			}`,
			&Config{
				Excludes: &ExcludeConfigs{
					&ExcludeConfig{
						Source: config.String("foo/bar"),
					},
				},
			},
			false,
		},
		{
			"exclude_multi",
			`exclude {
				source = "foo/bar"
			}

			exclude {
				source = "zip/zap"
			}`,
			&Config{
				Excludes: &ExcludeConfigs{
					&ExcludeConfig{
						Source: config.String("foo/bar"),
					},
					&ExcludeConfig{
						Source: config.String("zip/zap"),
					},
				},
			},
			false,
		},
		{
			"exclude_string",
			`exclude = "foo/bar"`,
			&Config{
				Excludes: &ExcludeConfigs{
					&ExcludeConfig{
						Source: config.String("foo/bar"),
					},
				},
			},
			false,
		},
		{
			"kill_signal",
			`kill_signal = "SIGUSR1"`,
			&Config{
				KillSignal: config.Signal(syscall.SIGUSR1),
			},
			false,
		},
		{
			"log_level",
			`log_level = "WARN"`,
			&Config{
				LogLevel: config.String("WARN"),
			},
			false,
		},
		{
			"max_stale",
			`max_stale = "10s"`,
			&Config{
				MaxStale: config.TimeDuration(10 * time.Second),
			},
			false,
		},
		{
			"pid_file",
			`pid_file = "/var/pid"`,
			&Config{
				PidFile: config.String("/var/pid"),
			},
			false,
		},
		{
			"prefix",
			`prefix {}`,
			&Config{
				Prefixes: &PrefixConfigs{
					&PrefixConfig{},
				},
			},
			false,
		},
		{
			"prefix_multi",
			`prefix {}
			prefix{}`,
			&Config{
				Prefixes: &PrefixConfigs{
					&PrefixConfig{},
					&PrefixConfig{},
				},
			},
			false,
		},
		{
			"prefix_string",
			`prefix = "foo/bar@dc"`,
			&Config{
				Prefixes: &PrefixConfigs{
					&PrefixConfig{
						Datacenter:  config.String("dc"),
						Destination: config.String("foo/bar"),
						Source:      config.String("foo/bar"),
					},
				},
			},
			false,
		},
		{
			"prefix_stanza",
			`prefix {
				source = "foo/bar@dc"
				destination = "default"
			}`,
			&Config{
				Prefixes: &PrefixConfigs{
					&PrefixConfig{
						Datacenter:  config.String("dc"),
						Destination: config.String("default"),
						Source:      config.String("foo/bar"),
					},
				},
			},
			false,
		},
		{
			"prefix_stanza_inline",
			`prefix {
				source = "foo/bar@dc:default"
			}`,
			&Config{
				Prefixes: &PrefixConfigs{
					&PrefixConfig{
						Datacenter:  config.String("dc"),
						Destination: config.String("default"),
						Source:      config.String("foo/bar"),
					},
				},
			},
			false,
		},
		{
			"prefix_stanza_datacenter",
			`prefix {
				source = "foo/bar"
				datacenter = "dc"
			}`,
			&Config{
				Prefixes: &PrefixConfigs{
					&PrefixConfig{
						Datacenter:  config.String("dc"),
						Destination: config.String("foo/bar"),
						Source:      config.String("foo/bar"),
					},
				},
			},
			false,
		},
		{
			"reload_signal",
			`reload_signal = "SIGUSR1"`,
			&Config{
				ReloadSignal: config.Signal(syscall.SIGUSR1),
			},
			false,
		},
		{
			"status_dir",
			`status_dir = "foo/bar/baz"`,
			&Config{
				StatusDir: config.String("foo/bar/baz"),
			},
			false,
		},
		{
			"syslog",
			`syslog {}`,
			&Config{
				Syslog: &config.SyslogConfig{},
			},
			false,
		},
		{
			"syslog_enabled",
			`syslog {
				enabled = true
			}`,
			&Config{
				Syslog: &config.SyslogConfig{
					Enabled: config.Bool(true),
				},
			},
			false,
		},
		{
			"syslog_facility",
			`syslog {
				facility = "facility"
			}`,
			&Config{
				Syslog: &config.SyslogConfig{
					Facility: config.String("facility"),
				},
			},
			false,
		},
		{
			"wait",
			`wait {
				min = "10s"
				max = "20s"
			}`,
			&Config{
				Wait: &config.WaitConfig{
					Min: config.TimeDuration(10 * time.Second),
					Max: config.TimeDuration(20 * time.Second),
				},
			},
			false,
		},
		{
			// Previous wait declarations used this syntax, but now use the stanza
			// syntax. Keep this around for backwards-compat.
			"wait_as_string",
			`wait = "10s:20s"`,
			&Config{
				Wait: &config.WaitConfig{
					Min: config.TimeDuration(10 * time.Second),
					Max: config.TimeDuration(20 * time.Second),
				},
			},
			false,
		},

		// General validation
		{
			"invalid_key",
			`not_a_valid_key = "hello"`,
			nil,
			true,
		},
		{
			"invalid_stanza",
			`not_a_valid_stanza {
				a = "b"
			}`,
			nil,
			true,
		},
		{
			"mapstructure_error",
			`consul = true`,
			nil,
			true,
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.name), func(t *testing.T) {
			c, err := Parse(tc.i)
			if (err != nil) != tc.err {
				t.Fatal(err)
			}

			if c != nil && c.Prefixes != nil {
				for _, p := range *c.Prefixes {
					p.Dependency = nil
				}
			}

			if !reflect.DeepEqual(tc.e, c) {
				t.Errorf("\nexp: %#v\nact: %#v", tc.e, c)
			}
		})
	}
}

func TestConfig_Merge(t *testing.T) {
	cases := []struct {
		name string
		a    *Config
		b    *Config
		r    *Config
	}{
		{
			"nil_a",
			nil,
			&Config{},
			&Config{},
		},
		{
			"nil_b",
			&Config{},
			nil,
			&Config{},
		},
		{
			"nil_both",
			nil,
			nil,
			nil,
		},
		{
			"empty",
			&Config{},
			&Config{},
			&Config{},
		},
		{
			"consul",
			&Config{
				Consul: &config.ConsulConfig{
					Address: config.String("consul"),
				},
			},
			&Config{
				Consul: &config.ConsulConfig{
					Address: config.String("consul-diff"),
				},
			},
			&Config{
				Consul: &config.ConsulConfig{
					Address: config.String("consul-diff"),
				},
			},
		},
		{
			"exclude",
			&Config{
				Excludes: &ExcludeConfigs{
					&ExcludeConfig{
						Source: config.String("foo/bar"),
					},
				},
			},
			&Config{
				Excludes: &ExcludeConfigs{
					&ExcludeConfig{
						Source: config.String("zip/zap"),
					},
				},
			},
			&Config{
				Excludes: &ExcludeConfigs{
					&ExcludeConfig{
						Source: config.String("foo/bar"),
					},
					&ExcludeConfig{
						Source: config.String("zip/zap"),
					},
				},
			},
		},
		{
			"kill_signal",
			&Config{
				KillSignal: config.Signal(syscall.SIGUSR1),
			},
			&Config{
				KillSignal: config.Signal(syscall.SIGUSR2),
			},
			&Config{
				KillSignal: config.Signal(syscall.SIGUSR2),
			},
		},
		{
			"log_level",
			&Config{
				LogLevel: config.String("log_level"),
			},
			&Config{
				LogLevel: config.String("log_level-diff"),
			},
			&Config{
				LogLevel: config.String("log_level-diff"),
			},
		},
		{
			"max_stale",
			&Config{
				MaxStale: config.TimeDuration(10 * time.Second),
			},
			&Config{
				MaxStale: config.TimeDuration(20 * time.Second),
			},
			&Config{
				MaxStale: config.TimeDuration(20 * time.Second),
			},
		},
		{
			"pid_file",
			&Config{
				PidFile: config.String("pid_file"),
			},
			&Config{
				PidFile: config.String("pid_file-diff"),
			},
			&Config{
				PidFile: config.String("pid_file-diff"),
			},
		},
		{
			"prefix",
			&Config{
				Prefixes: &PrefixConfigs{
					&PrefixConfig{
						Source: config.String("foo/bar"),
					},
				},
			},
			&Config{
				Prefixes: &PrefixConfigs{
					&PrefixConfig{
						Source: config.String("zip/zap"),
					},
				},
			},
			&Config{
				Prefixes: &PrefixConfigs{
					&PrefixConfig{
						Source: config.String("foo/bar"),
					},
					&PrefixConfig{
						Source: config.String("zip/zap"),
					},
				},
			},
		},
		{
			"reload_signal",
			&Config{
				ReloadSignal: config.Signal(syscall.SIGUSR1),
			},
			&Config{
				ReloadSignal: config.Signal(syscall.SIGUSR2),
			},
			&Config{
				ReloadSignal: config.Signal(syscall.SIGUSR2),
			},
		},
		{
			"status_dir",
			&Config{
				StatusDir: config.String("foo"),
			},
			&Config{
				StatusDir: config.String("bar"),
			},
			&Config{
				StatusDir: config.String("bar"),
			},
		},
		{
			"syslog",
			&Config{
				Syslog: &config.SyslogConfig{
					Enabled: config.Bool(true),
				},
			},
			&Config{
				Syslog: &config.SyslogConfig{
					Enabled: config.Bool(false),
				},
			},
			&Config{
				Syslog: &config.SyslogConfig{
					Enabled: config.Bool(false),
				},
			},
		},
		{
			"wait",
			&Config{
				Wait: &config.WaitConfig{
					Min: config.TimeDuration(10 * time.Second),
					Max: config.TimeDuration(20 * time.Second),
				},
			},
			&Config{
				Wait: &config.WaitConfig{
					Min: config.TimeDuration(20 * time.Second),
					Max: config.TimeDuration(50 * time.Second),
				},
			},
			&Config{
				Wait: &config.WaitConfig{
					Min: config.TimeDuration(20 * time.Second),
					Max: config.TimeDuration(50 * time.Second),
				},
			},
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.name), func(t *testing.T) {
			r := tc.a.Merge(tc.b)
			if !reflect.DeepEqual(tc.r, r) {
				t.Errorf("\nexp: %#v\nact: %#v", tc.r, r)
			}
		})
	}
}

func TestFromPath(t *testing.T) {
	f, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	emptyDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(emptyDir)

	configDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(configDir)
	cf1, err := ioutil.TempFile(configDir, "")
	if err != nil {
		t.Fatal(err)
	}
	d := []byte(`
		consul {
			address = "1.2.3.4"
		}
	`)
	if err = ioutil.WriteFile(cf1.Name(), d, 0644); err != nil {
		t.Fatal(err)
	}
	cf2, err := ioutil.TempFile(configDir, "")
	if err != nil {
		t.Fatal(err)
	}
	d = []byte(`
		consul {
			token = "token"
		}
	`)
	if err := ioutil.WriteFile(cf2.Name(), d, 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		e    *Config
		err  bool
	}{
		{
			"missing_dir",
			"/not/a/real/dir",
			nil,
			true,
		},
		{
			"file",
			f.Name(),
			&Config{},
			false,
		},
		{
			"empty_dir",
			emptyDir,
			nil,
			false,
		},
		{
			"config_dir",
			configDir,
			&Config{
				Consul: &config.ConsulConfig{
					Address: config.String("1.2.3.4"),
					Token:   config.String("token"),
				},
			},
			false,
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.name), func(t *testing.T) {
			c, err := FromPath(tc.path)
			if (err != nil) != tc.err {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(tc.e, c) {
				t.Errorf("\nexp: %#v\nact: %#v", tc.e, c)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cases := []struct {
		env string
		val string
		e   *Config
		err bool
	}{
		{
			"CONSUL_HTTP_ADDR",
			"1.2.3.4",
			&Config{
				Consul: &config.ConsulConfig{
					Address: config.String("1.2.3.4"),
				},
			},
			false,
		},
		{
			"CONSUL_REPLICATE_LOG",
			"DEBUG",
			&Config{
				LogLevel: config.String("DEBUG"),
			},
			false,
		},
		{
			"CR_LOG",
			"DEBUG",
			&Config{
				LogLevel: config.String("DEBUG"),
			},
			false,
		},
		{
			"CONSUL_TOKEN",
			"token",
			&Config{
				Consul: &config.ConsulConfig{
					Token: config.String("token"),
				},
			},
			false,
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.env), func(t *testing.T) {
			if err := os.Setenv(tc.env, tc.val); err != nil {
				t.Fatal(err)
			}
			defer os.Unsetenv(tc.env)

			r := DefaultConfig()
			r.Merge(tc.e)

			c := DefaultConfig()
			if !reflect.DeepEqual(r, c) {
				t.Errorf("\nexp: %#v\nact: %#v", r, c)
			}
		})
	}
}
