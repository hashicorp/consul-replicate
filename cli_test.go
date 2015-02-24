package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"
	"time"

	dep "github.com/hashicorp/consul-template/dependency"
	"github.com/hashicorp/consul-template/watch"
)

// Deprecated CLI options
// TODO: Remove in the next release

func TestParseFlags_addr(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-addr", "1.2.3.4",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := "1.2.3.4"
	if config.Consul != expected {
		t.Errorf("expected %q to be %q", config.Consul, expected)
	}
}

func TestParseFlags_dstPrefix(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-prefix", "global",
		"-dst-prefix", "backup",
	})
	if err != nil {
		t.Fatal(err)
	}

	prefix, err := ParsePrefix("global:backup")
	if err != nil {
		t.Fatal(err)
	}

	expected := []*Prefix{prefix}
	if !reflect.DeepEqual(config.Prefixes, expected) {
		t.Errorf("expected %q to be %q", config.Prefixes, expected)
	}
}

func TestParseFlags_dstPrefixNoPrefix(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	_, _, _, err := cli.parseFlags([]string{
		"-dst-prefix", "backup",
	})
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expected := "must specify at least one prefix"
	if err.Error() != expected {
		t.Errorf("expected %q to be %q", err.Error(), expected)
	}
}

func TestParseFlags_src(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-prefix", "global",
		"-src", "nyc2",
	})
	if err != nil {
		t.Fatal(err)
	}

	prefix, err := ParsePrefix("global@nyc2")
	if err != nil {
		t.Fatal(err)
	}

	expected := []*Prefix{prefix}
	if !reflect.DeepEqual(config.Prefixes, expected) {
		t.Errorf("expected %q to be %q", config.Prefixes, expected)
	}
}

func TestParseFlags_srcNoPrefix(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	_, _, _, err := cli.parseFlags([]string{
		"-src", "nyc2",
	})
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expected := "must specify at least one prefix"
	if err.Error() != expected {
		t.Errorf("expected %q to be %q", err.Error(), expected)
	}
}

func TestParseFlags_srcBadPrefix(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	_, _, _, err := cli.parseFlags([]string{
		"-prefix", "global",
		"-src", "n((*y@#c@!2",
	})
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expected := "invalid key prefix dependency format"
	if !strings.Contains(err.Error(), expected) {
		t.Errorf("expected %q to be %q", err.Error(), expected)
	}
}

func TestParseFlags_lock(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	_, _, _, err := cli.parseFlags([]string{
		"-lock", "service/locks/consul-replicate",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseFlags_status(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-status", "service/statuses/consul-replicate",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := "service/statuses/consul-replicate"
	if config.StatusDir != expected {
		t.Errorf("expected %q to be %q", config.StatusDir, expected)
	}
}

func TestParseFlags_service(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	_, _, _, err := cli.parseFlags([]string{
		"-service", "replicator",
	})
	if err != nil {
		t.Fatal(err)
	}
}

// End deprecated CLI options

func TestParseFlags_consul(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-consul", "12.34.56.78",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := "12.34.56.78"
	if config.Consul != expected {
		t.Errorf("expected %q to be %q", config.Consul, expected)
	}
}

func TestParseFlags_token(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-token", "abcd1234",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := "abcd1234"
	if config.Token != expected {
		t.Errorf("expected %q to be %q", config.Token, expected)
	}
}

func TestParseFlags_authUsername(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-auth", "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if config.Auth.Enabled != true {
		t.Errorf("expected auth to be enabled")
	}

	expected := "test"
	if config.Auth.Username != expected {
		t.Errorf("expected %v to be %v", config.Auth.Username, expected)
	}
}

func TestParseFlags_authUsernamePassword(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-auth", "test:test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if config.Auth.Enabled != true {
		t.Errorf("expected auth to be enabled")
	}

	expected := "test"
	if config.Auth.Username != expected {
		t.Errorf("expected %v to be %v", config.Auth.Username, expected)
	}
	if config.Auth.Password != expected {
		t.Errorf("expected %v to be %v", config.Auth.Password, expected)
	}
}

func TestParseFlags_SSL(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-ssl",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := true
	if config.SSL.Enabled != expected {
		t.Errorf("expected %v to be %v", config.SSL.Enabled, expected)
	}
}

func TestParseFlags_noSSL(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-ssl=false",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := false
	if config.SSL.Enabled != expected {
		t.Errorf("expected %v to be %v", config.SSL.Enabled, expected)
	}
}

func TestParseFlags_SSLVerify(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-ssl-verify",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := true
	if config.SSL.Verify != expected {
		t.Errorf("expected %v to be %v", config.SSL.Verify, expected)
	}
}

func TestParseFlags_noSSLVerify(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-ssl-verify=false",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := false
	if config.SSL.Verify != expected {
		t.Errorf("expected %v to be %v", config.SSL.Verify, expected)
	}
}

func TestParseFlags_maxStale(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-max-stale", "10h",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := 10 * time.Hour
	if config.MaxStale != expected {
		t.Errorf("expected %q to be %q", config.MaxStale, expected)
	}
}

func TestParseFlags_prefixes(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-prefix", "global@nyc1:backup",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(config.Prefixes) != 1 {
		t.Fatal("expected 1 prefix")
	}

	d, err := dep.ParseStoreKeyPrefix("global@nyc1")
	if err != nil {
		t.Fatal(err)
	}
	expected := &Prefix{
		Source:      d,
		SourceRaw:   "global@nyc1",
		Destination: "backup",
	}

	if !reflect.DeepEqual(config.Prefixes[0], expected) {
		t.Errorf("expected %q to be %q", config.Prefixes[0], expected)
	}
}

func TestParseFlags_syslog(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-syslog",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := true
	if config.Syslog.Enabled != expected {
		t.Errorf("expected %v to be %v", config.Syslog.Enabled, expected)
	}
}

func TestParseFlags_syslogFacility(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-syslog-facility", "LOCAL5",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := "LOCAL5"
	if config.Syslog.Facility != expected {
		t.Errorf("expected %v to be %v", config.Syslog.Facility, expected)
	}
}

func TestParseFlags_wait(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-wait", "10h:11h",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := &watch.Wait{
		Min: 10 * time.Hour,
		Max: 11 * time.Hour,
	}
	if !reflect.DeepEqual(config.Wait, expected) {
		t.Errorf("expected %v to be %v", config.Wait, expected)
	}
}

func TestParseFlags_waitError(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	_, _, _, err := cli.parseFlags([]string{
		"-wait", "watermelon:bacon",
	})
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expected := "invalid value"
	if !strings.Contains(err.Error(), expected) {
		t.Errorf("expected %q to contain %q", err.Error(), expected)
	}
}

func TestParseFlags_config(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-config", "/path/to/file",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := "/path/to/file"
	if config.Path != expected {
		t.Errorf("expected %v to be %v", config.Path, expected)
	}
}

func TestParseFlags_retry(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-retry", "10h",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := 10 * time.Hour
	if config.Retry != expected {
		t.Errorf("expected %v to be %v", config.Retry, expected)
	}
}

func TestParseFlags_once(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	_, once, _, err := cli.parseFlags([]string{
		"-once",
	})
	if err != nil {
		t.Fatal(err)
	}

	if once != true {
		t.Errorf("expected once to be true")
	}
}

func TestParseFlags_version(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	_, _, version, err := cli.parseFlags([]string{
		"-version",
	})
	if err != nil {
		t.Fatal(err)
	}

	if version != true {
		t.Errorf("expected version to be true")
	}
}

func TestParseFlags_logLevel(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-log-level", "debug",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := "debug"
	if config.LogLevel != expected {
		t.Errorf("expected %v to be %v", config.LogLevel, expected)
	}
}

func TestParseFlags_statusDir(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	config, _, _, err := cli.parseFlags([]string{
		"-status-dir", "custom-status-dir",
	})
	if err != nil {
		t.Fatal(err)
	}

	expected := "custom-status-dir"
	if config.StatusDir != expected {
		t.Errorf("expected %v to be %v", config.StatusDir, expected)
	}
}

func TestParseFlags_errors(t *testing.T) {
	cli := NewCLI(ioutil.Discard, ioutil.Discard)
	_, _, _, err := cli.parseFlags([]string{
		"-totally", "-not", "-valid",
	})

	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}
}

func TestRun_printsErrors(t *testing.T) {
	outStream, errStream := new(bytes.Buffer), new(bytes.Buffer)
	cli := NewCLI(outStream, errStream)
	args := strings.Split("consul-replicate -bacon delicious", " ")

	status := cli.Run(args)
	if status == ExitCodeOK {
		t.Fatal("expected not OK exit code")
	}

	expected := "flag provided but not defined: -bacon"
	if !strings.Contains(errStream.String(), expected) {
		t.Errorf("expected %q to eq %q", errStream.String(), expected)
	}
}

func TestRun_versionFlag(t *testing.T) {
	outStream, errStream := new(bytes.Buffer), new(bytes.Buffer)
	cli := NewCLI(outStream, errStream)
	args := strings.Split("consul-replicate -version", " ")

	status := cli.Run(args)
	if status != ExitCodeOK {
		t.Errorf("expected %q to eq %q", status, ExitCodeOK)
	}

	expected := fmt.Sprintf("consul-replicate v%s", Version)
	if !strings.Contains(errStream.String(), expected) {
		t.Errorf("expected %q to eq %q", errStream.String(), expected)
	}
}

func TestRun_parseError(t *testing.T) {
	outStream, errStream := new(bytes.Buffer), new(bytes.Buffer)
	cli := NewCLI(outStream, errStream)
	args := strings.Split("consul-replicate -bacon delicious", " ")

	status := cli.Run(args)
	if status != ExitCodeParseFlagsError {
		t.Errorf("expected %q to eq %q", status, ExitCodeParseFlagsError)
	}

	expected := "flag provided but not defined: -bacon"
	if !strings.Contains(errStream.String(), expected) {
		t.Fatalf("expected %q to contain %q", errStream.String(), expected)
	}
}

func TestRun_onceFlag(t *testing.T) {
	t.Skip("pending a rewrite of the runner")

	outStream, errStream := new(bytes.Buffer), new(bytes.Buffer)
	cli := NewCLI(outStream, errStream)

	command := fmt.Sprintf("consul-replicate -consul demo.consul.io -prefix global@nyc1 -once")
	args := strings.Split(command, " ")

	ch := make(chan int, 1)
	go func() {
		ch <- cli.Run(args)
	}()

	select {
	case status := <-ch:
		if status != ExitCodeOK {
			t.Errorf("expected %d to eq %d", status, ExitCodeOK)
			t.Errorf("stderr: %s", errStream.String())
		}
	case <-time.After(2 * time.Second):
		t.Errorf("expected exit, did not exit after 2 seconds")
	}
}
