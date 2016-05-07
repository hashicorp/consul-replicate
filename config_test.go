package main

import (
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-template/test"
	"github.com/hashicorp/consul-template/watch"
)

// Test that an empty config does nothing
func TestMerge_emptyConfig(t *testing.T) {
	consul := "consul.io:8500"
	config := &Config{Consul: consul}
	config.Merge(&Config{})

	if config.Consul != consul {
		t.Fatalf("expected %q to equal %q", config.Consul, consul)
	}
}

// Test that simple values are merged
func TestMerge_simpleConfig(t *testing.T) {
	config, newConsul := &Config{Consul: "consul.io:8500"}, "packer.io:7300"
	config.Merge(&Config{Consul: newConsul})

	if config.Consul != newConsul {
		t.Fatalf("expected %q to equal %q", config.Consul, newConsul)
	}
}

// Test that the flags for HTTPS are properly merged
func TestMerge_HttpsOptions(t *testing.T) {
	config := &Config{
		SSL: &SSL{
			Enabled: false,
			Verify:  false,
		},
	}
	otherConfig := &Config{
		SSL: &SSL{
			Enabled: true,
			Verify:  true,
		},
	}
	config.Merge(otherConfig)

	if config.SSL.Enabled != true {
		t.Errorf("expected enabled to be true")
	}

	if config.SSL.Verify != true {
		t.Errorf("expected SSL verify to be true")
	}

	config = &Config{
		SSL: &SSL{
			Enabled: true,
			Verify:  true,
		},
	}
	otherConfig = &Config{
		SSL: &SSL{
			Enabled: false,
			Verify:  false,
		},
	}
	config.Merge(otherConfig)

	if config.SSL.Enabled != false {
		t.Errorf("expected enabled to be false")
	}

	if config.SSL.Verify != false {
		t.Errorf("expected SSL verify to be false")
	}
}

func TestMerge_Prefixes(t *testing.T) {
	global := &Prefix{SourceRaw: "global/config"}
	redis := &Prefix{SourceRaw: "redis/config"}

	config := &Config{Prefixes: []*Prefix{global}}
	otherConfig := &Config{Prefixes: []*Prefix{redis}}
	config.Merge(otherConfig)

	expected := []*Prefix{global, redis}
	if !reflect.DeepEqual(config.Prefixes, expected) {
		t.Errorf("expected %#v to be %#v", config.Prefixes, expected)
	}
}

func TestMerge_AuthOptions(t *testing.T) {
	config := &Config{
		Auth: &Auth{Username: "user", Password: "pass"},
	}
	otherConfig := &Config{
		Auth: &Auth{Username: "newUser", Password: ""},
	}
	config.Merge(otherConfig)

	if config.Auth.Username != "newUser" {
		t.Errorf("expected %q to be %q", config.Auth.Username, "newUser")
	}
}

func TestMerge_SyslogOptions(t *testing.T) {
	config := &Config{
		Syslog: &Syslog{Enabled: false, Facility: "LOCAL0"},
	}
	otherConfig := &Config{
		Syslog: &Syslog{Enabled: true, Facility: "LOCAL1"},
	}
	config.Merge(otherConfig)

	if config.Syslog.Enabled != true {
		t.Errorf("expected %t to be %t", config.Syslog.Enabled, true)
	}

	if config.Syslog.Facility != "LOCAL1" {
		t.Errorf("expected %q to be %q", config.Syslog.Facility, "LOCAL1")
	}
}

// Test that file read errors are propagated up
func TestParseConfig_readFileError(t *testing.T) {
	_, err := ParseConfig(path.Join(os.TempDir(), "config.json"))
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expectedErr := "no such file or directory"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q to contain %q", err.Error(), expectedErr)
	}
}

// Test that parser errors are propagated up
func TestParseConfig_parseFileError(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
    invalid
  `), t)
	defer test.DeleteTempfile(configFile, t)

	_, err := ParseConfig(configFile.Name())
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}
}

// Test that mapstructure errors are propagated up
func TestParseConfig_mapstructureError(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
    consul = true
  `), t)
	defer test.DeleteTempfile(configFile, t)

	_, err := ParseConfig(configFile.Name())
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expectedErr := "nconvertible type 'bool'"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q to contain %q", err.Error(), expectedErr)
	}
}

// Test that the config is parsed correctly
func TestParseConfig_correctValues(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
    consul = "nyc1.demo.consul.io"
    max_stale = "5s"
    token = "abcd1234"
    wait = "5s:10s"
    retry = "10s"
    log_level = "warn"

    status_path = "global/statuses/replicators"

    auth {
    	enabled = true
    	username = "test"
    	password = "test"
    }

    ssl {
    	enabled = true
    	verify = false
    }

    syslog {
    	enabled = true
    	facility = "LOCAL5"
    }
  `), t)
	defer test.DeleteTempfile(configFile, t)

	config, err := ParseConfig(configFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	expected := &Config{
		Path:        configFile.Name(),
		Consul:      "nyc1.demo.consul.io",
		Token:       "abcd1234",
		MaxStale:    time.Second * 5,
		MaxStaleRaw: "5s",
		Auth: &Auth{
			Enabled:  true,
			Username: "test",
			Password: "test",
		},
		AuthRaw: []*Auth{
			&Auth{
				Enabled:  true,
				Username: "test",
				Password: "test",
			},
		},
		SSL: &SSL{
			Enabled: true,
			Verify:  false,
		},
		SSLRaw: []*SSL{
			&SSL{
				Enabled: true,
				Verify:  false,
			},
		},
		Syslog: &Syslog{
			Enabled:  true,
			Facility: "LOCAL5",
		},
		SyslogRaw: []*Syslog{
			&Syslog{
				Enabled:  true,
				Facility: "LOCAL5",
			},
		},
		Wait: &watch.Wait{
			Min: time.Second * 5,
			Max: time.Second * 10,
		},
		WaitRaw:   "5s:10s",
		Retry:     10 * time.Second,
		RetryRaw:  "10s",
		LogLevel:  "warn",
		StatusDir: "global/statuses/replicators",
	}

	if !reflect.DeepEqual(config, expected) {
		t.Fatalf("expected \n%#v\n\n, got \n\n%#v\n\n", expected, config)
	}
}

func TestParseConfig_parseStoreKeyPrefixError(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
    prefix {
    	source = "@*(#42"
  	}
  `), t)
	defer test.DeleteTempfile(configFile, t)

	_, err := ParseConfig(configFile.Name())
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expectedErr := "invalid key prefix dependency format"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q to contain %q", err.Error(), expectedErr)
	}
}

func TestParseConfig_parseRetryError(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
    retry = "bacon pants"
  `), t)
	defer test.DeleteTempfile(configFile, t)

	_, err := ParseConfig(configFile.Name())
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expectedErr := "retry invalid"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q to contain %q", err.Error(), expectedErr)
	}
}

func TestParseConfig_parseWaitError(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
    wait = "not_valid:duration"
  `), t)
	defer test.DeleteTempfile(configFile, t)

	_, err := ParseConfig(configFile.Name())
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expectedErr := "wait invalid"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q to contain %q", err.Error(), expectedErr)
	}
}

func TestParsePrefix_emptyStringArgs(t *testing.T) {
	_, err := ParsePrefix("")
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expectedErr := "cannot specify empty prefix declaration"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q to contain %q", err.Error(), expectedErr)
	}
}

func TestParsePrefix_stringWithSpacesArgs(t *testing.T) {
	_, err := ParsePrefix("  ")
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expectedErr := "cannot specify empty prefix declaration"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q to contain %q", err.Error(), expectedErr)
	}
}

func TestParsePrefix_tooManyArgs(t *testing.T) {
	_, err := ParsePrefix("foo:bar:blitz:baz")
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expectedErr := "invalid prefix declaration format"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error %q to contain %q", err.Error(), expectedErr)
	}
}

func TestParsePrefix_source(t *testing.T) {
	source := "global"
	prefix, err := ParsePrefix(source)
	if err != nil {
		t.Fatal(err)
	}

	if prefix.SourceRaw != source {
		t.Errorf("expected %q to be %q", prefix.SourceRaw, source)
	}
	if prefix.Source.Prefix != source {
		t.Errorf("expected %q to be %q", prefix.Source.Prefix, source)
	}

	// if destination is not explicitly specified, source will be copied to destination
	// destination may not exist, so the destination folder must end with a slash
	expectedDestination := "global/"
	if prefix.Destination != expectedDestination {
		t.Errorf("expected %q to be %q", prefix.Destination, expectedDestination)
	}
}

func TestParsePrefix_sourceSlash(t *testing.T) {
	source := "/global"
	prefix, err := ParsePrefix(source)
	if err != nil {
		t.Fatal(err)
	}

	expected := "global"
	if prefix.SourceRaw != expected {
		t.Errorf("expected %q to be %q", prefix.SourceRaw, expected)
	}
	if prefix.Source.Prefix != expected {
		t.Errorf("expected %q to be %q", prefix.Source.Prefix, expected)
	}
}

func TestParsePrefix_destination(t *testing.T) {
	source, destination := "global@nyc4", "backup"
	prefix, err := ParsePrefix(fmt.Sprintf("%s:%s", source, destination))
	if err != nil {
		t.Fatal(err)
	}

	if prefix.SourceRaw != "global@nyc4" {
		t.Errorf("expected %q to be %q", prefix.SourceRaw, "global@nyc4")
	}
	// destination must have a slash appended to it
	expectedDestination := "backup/"
	if prefix.Destination != expectedDestination {
		t.Errorf("expected %q to be %q", prefix.Destination, expectedDestination)
	}
	if prefix.Source.Prefix != "global" {
		t.Errorf("expected %q to be %q", prefix.Source.Prefix, "global")
	}
}
