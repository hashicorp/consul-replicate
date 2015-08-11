package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	dep "github.com/hashicorp/consul-template/dependency"
	"github.com/hashicorp/consul-template/test"
	"github.com/hashicorp/consul-template/watch"
)

func TestNewRunner_initialize(t *testing.T) {
	once := true
	config := &Config{
		Prefixes: []*Prefix{
			&Prefix{SourceRaw: "1", Destination: "4"},
			&Prefix{SourceRaw: "2", Destination: "5"},
			&Prefix{SourceRaw: "3", Destination: "6"},
			&Prefix{SourceRaw: "4", Destination: "7"},
		},
	}

	runner, err := NewRunner(config, once)
	if err != nil {
		t.Fatal(err)
	}

	// check the items we set in the config
	if !reflect.DeepEqual(runner.config.Prefixes, config.Prefixes) {
		t.Errorf("expected %#v to be %#v", runner.config.Prefixes, config.Prefixes)
	}

	if runner.once != once {
		t.Errorf("expected %#v to be %#v", runner.once, once)
	}

	if runner.client == nil {
		t.Errorf("expected %#v to not be %#v", runner.client, nil)
	}

	if runner.watcher == nil {
		t.Errorf("expected %#v to not be %#v", runner.watcher, nil)
	}

	if runner.data == nil {
		t.Errorf("expected %#v to not be %#v", runner.data, nil)
	}

	if runner.outStream != os.Stdout {
		t.Errorf("expected %#v to be %#v", runner.outStream, os.Stdout)
	}

	if runner.errStream != os.Stderr {
		t.Errorf("expected %#v to be %#v", runner.errStream, os.Stderr)
	}

	if runner.ErrCh == nil {
		t.Errorf("expected %#v to be %#v", runner.ErrCh, nil)
	}

	if runner.DoneCh == nil {
		t.Errorf("expected %#v to be %#v", runner.DoneCh, nil)
	}
}

func TestReceive_addsToData(t *testing.T) {
	runner, err := NewRunner(new(Config), false)
	if err != nil {
		t.Fatal(err)
	}

	prefix, err := ParsePrefix("global@nyc1")
	if err != nil {
		t.Fatal(err)
	}

	data := []*dep.KeyPair{
		&dep.KeyPair{
			Key:   "1",
			Value: "1",
		},
	}
	runner.Receive(&watch.View{
		Dependency: prefix.Source,
		Data:       data,
	})

	runner.RLock()
	defer runner.RUnlock()
	value, ok := runner.data[prefix.Source.HashCode()]
	if !ok {
		t.Fatalf("expected runner to have data")
	}
	if !reflect.DeepEqual(value.Data, data) {
		t.Errorf("expected %q to be %q", value.Data, data)
	}
}

func TestConfigDefaultOverrides(t *testing.T) {
	expected := "test/statuses"

	config := &Config{
		StatusDir: expected,
	}

	r, _ := NewRunner(config, true)
	if r.config.StatusDir != expected {
		t.Errorf("expected StatusDir %q to be %q", r.config.StatusDir, expected)
	}
}

func TestBuildConfig_singleFile(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
    consul = "127.0.0.1"
  `), t)
	defer test.DeleteTempfile(configFile, t)

	config := new(Config)
	if err := buildConfig(config, configFile.Name()); err != nil {
		t.Fatal(err)
	}

	expected := "127.0.0.1"
	if config.Consul != expected {
		t.Errorf("expected %q to be %q", config.Consul, expected)
	}
}

func TestBuildConfig_NonExistentDirectory(t *testing.T) {
	// Create a directory and then delete it
	configDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(configDir); err != nil {
		t.Fatal(err)
	}

	config := new(Config)
	err = buildConfig(config, configDir)
	if err == nil {
		t.Fatalf("expected error, but nothing was returned")
	}

	expected := "missing file/folder"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected %q to contain %q", err.Error(), expected)
	}
}

func TestBuildConfig_EmptyDirectory(t *testing.T) {
	// Create a directory with no files
	configDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(configDir)

	config := new(Config)
	err = buildConfig(config, configDir)
	if err == nil {
		t.Fatalf("expected error, but nothing was returned")
	}

	expected := "must contain at least one configuration file"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected %q to contain %q", err.Error(), expected)
	}
}

func TestBuildConfig_BadConfigs(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
    totally not a vaild config
  `), t)
	defer test.DeleteTempfile(configFile, t)

	configDir := filepath.Dir(configFile.Name())

	config := new(Config)
	err := buildConfig(config, configDir)
	if err == nil {
		t.Fatalf("expected error, but nothing was returned")
	}

	expected := "1 error(s) occurred"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected %q to contain %q", err.Error(), expected)
	}
}
