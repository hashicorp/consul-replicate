package main

import (
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-template/test"
	"github.com/hashicorp/consul-template/watch"
)

func testConfig(contents string, t *testing.T) *Config {
	f, err := ioutil.TempFile(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.Write([]byte(contents))
	if err != nil {
		t.Fatal(err)
	}

	config, err := ParseConfig(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func TestMerge_emptyConfig(t *testing.T) {
	config := DefaultConfig()
	config.Merge(&Config{})

	expected := DefaultConfig()
	if !reflect.DeepEqual(config, expected) {
		t.Errorf("expected \n\n%#v\n\n to be \n\n%#v\n\n", config, expected)
	}
}

func TestMerge_topLevel(t *testing.T) {
	config1 := testConfig(`
		consul = "consul-1"
		token = "token-1"
		max_stale = "1s"
		retry = "1s"
		wait = "1s"
		pid_file = "/pid-1"
		status_dir = "service/consul/foo"
		log_level = "log_level-1"
	`, t)
	config2 := testConfig(`
		consul = "consul-2"
		token = "token-2"
		max_stale = "2s"
		retry = "2s"
		wait = "2s"
		pid_file = "/pid-2"
		status_dir = "service/consul/bar"
		log_level = "log_level-2"
	`, t)
	config1.Merge(config2)

	if !reflect.DeepEqual(config1, config2) {
		t.Errorf("expected \n\n%#v\n\n to be \n\n%#v\n\n", config1, config2)
	}
}

func TestMerge_auth(t *testing.T) {
	config := testConfig(`
		auth {
			enabled = true
			username = "1"
			password = "1"
		}
	`, t)
	config.Merge(testConfig(`
		auth {
			password = "2"
		}
	`, t))

	expected := &AuthConfig{
		Enabled:  true,
		Username: "1",
		Password: "2",
	}

	if !reflect.DeepEqual(config.Auth, expected) {
		t.Errorf("expected \n\n%#v\n\n to be \n\n%#v\n\n", config.Auth, expected)
	}
}

func TestMerge_SSL(t *testing.T) {
	config := testConfig(`
		ssl {
			enabled = true
			verify = true
			cert = "1.pem"
			ca_cert = "ca-1.pem"
		}
	`, t)
	config.Merge(testConfig(`
		ssl {
			enabled = false
		}
	`, t))

	expected := &SSLConfig{
		Enabled: false,
		Verify:  true,
		Cert:    "1.pem",
		CaCert:  "ca-1.pem",
	}

	if !reflect.DeepEqual(config.SSL, expected) {
		t.Errorf("expected \n\n%#v\n\n to be \n\n%#v\n\n", config.SSL, expected)
	}
}

func TestMerge_syslog(t *testing.T) {
	config := testConfig(`
		syslog {
			enabled = true
			facility = "1"
		}
	`, t)
	config.Merge(testConfig(`
		syslog {
			facility = "2"
		}
	`, t))

	expected := &SyslogConfig{
		Enabled:  true,
		Facility: "2",
	}

	if !reflect.DeepEqual(config.Syslog, expected) {
		t.Errorf("expected \n\n%#v\n\n to be \n\n%#v\n\n", config.Syslog, expected)
	}
}

func TestMerge_Prefixes(t *testing.T) {
	config1 := testConfig(`
		prefix {
			source = "foo"
			destination = "bar"
		}
	`, t)
	config2 := testConfig(`
		prefix {
			source = "foo-2"
			destination = "bar-2"
		}
	`, t)
	config1.Merge(config2)

	if len(config1.Prefixes) != 2 {
		t.Fatalf("bad prefixes %d", len(config1.Prefixes))
	}

	if config1.Prefixes[0].Source == nil {
		t.Errorf("bad source: %#v", config1.Prefixes[0].Source)
	}
	if config1.Prefixes[0].SourceRaw != "foo" {
		t.Errorf("bad source_raw: %s", config1.Prefixes[0].SourceRaw)
	}
	if config1.Prefixes[0].Destination != "bar" {
		t.Errorf("bad destination: %s", config1.Prefixes[0].Destination)
	}

	if config1.Prefixes[1].Source == nil {
		t.Errorf("bad source: %#v", config1.Prefixes[1].Source)
	}
	if config1.Prefixes[1].SourceRaw != "foo-2" {
		t.Errorf("bad source_raw: %s", config1.Prefixes[1].SourceRaw)
	}
	if config1.Prefixes[1].Destination != "bar-2" {
		t.Errorf("bad destination: %s", config1.Prefixes[1].Destination)
	}
}

func TestMerge_Excludes(t *testing.T) {
	config1 := testConfig(`
		exclude {
			source = "foo"
		}
	`, t)
	config2 := testConfig(`
		exclude {
			source = "foo-2"
		}
	`, t)
	config1.Merge(config2)

	if len(config1.Excludes) != 2 {
		t.Fatalf("bad excludes %d", len(config1.Excludes))
	}

	if config1.Excludes[0].Source != "foo" {
		t.Errorf("bad source: %#v", config1.Excludes[0].Source)
	}

	if config1.Excludes[1].Source != "foo-2" {
		t.Errorf("bad source: %#v", config1.Excludes[1].Source)
	}
}

func TestMerge_wait(t *testing.T) {
	config1 := testConfig(`
		wait = "1s:1s"
	`, t)
	config2 := testConfig(`
		wait = "2s:2s"
	`, t)
	config1.Merge(config2)

	if !reflect.DeepEqual(config1.Wait, config2.Wait) {
		t.Errorf("expected \n\n%#v\n\n to be \n\n%#v\n\n", config1.Wait, config2.Wait)
	}
}

func TestParseConfig_readFileError(t *testing.T) {
	_, err := ParseConfig(path.Join(os.TempDir(), "config.json"))
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expected := "no such file or directory"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected %q to include %q", err.Error(), expected)
	}
}

func TestParseConfig_correctValues(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
    consul = "nyc1.demo.consul.io"
    max_stale = "5s"
    token = "abcd1234"
    wait = "5s:10s"
    retry = "10s"
		pid_file = "/var/run/ct"
    log_level = "warn"

    status_dir = "global/statuses/replicators"

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
		Path:     configFile.Name(),
		PidFile:  "/var/run/ct",
		Consul:   "nyc1.demo.consul.io",
		Token:    "abcd1234",
		MaxStale: time.Second * 5,
		Auth: &AuthConfig{
			Enabled:  true,
			Username: "test",
			Password: "test",
		},
		Prefixes: []*Prefix{},
		Excludes: []*Exclude{},
		SSL: &SSLConfig{
			Enabled: true,
			Verify:  false,
		},
		Syslog: &SyslogConfig{
			Enabled:  true,
			Facility: "LOCAL5",
		},
		Wait: &watch.Wait{
			Min: time.Second * 5,
			Max: time.Second * 10,
		},
		Retry:     10 * time.Second,
		LogLevel:  "warn",
		StatusDir: "global/statuses/replicators",
		setKeys:   config.setKeys,
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
	prefix, err := ParsePrefix("global")
	if err != nil {
		t.Fatal(err)
	}

	if prefix.Source.Prefix != "global/" {
		t.Errorf("expected %q to be %q", prefix.Source.Prefix, "global/")
	}

	// if destination is not explicitly specified, source will be copied to destination
	// destination may not exist, so the destination folder must end with a slash
	if prefix.Destination != "global/" {
		t.Errorf("expected %q to be %q", prefix.Destination, "global/")
	}
}

func TestParsePrefix_sourceSlash(t *testing.T) {
	prefix, err := ParsePrefix("/global")
	if err != nil {
		t.Fatal(err)
	}

	if prefix.Source.Prefix != "global/" {
		t.Errorf("expected %q to be %q", prefix.Source.Prefix, "global/")
	}
}

func TestParsePrefix_destination(t *testing.T) {
	prefix, err := ParsePrefix("global@nyc4:backup")
	if err != nil {
		t.Fatal(err)
	}

	if prefix.Destination != "backup/" {
		t.Errorf("expected %q to be %q", prefix.Destination, "backup/")
	}
	if prefix.Source.Prefix != "global/" {
		t.Errorf("expected %q to be %q", prefix.Source.Prefix, "global/")
	}
}
