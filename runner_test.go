package main

import (
	"os"
	"reflect"
	"testing"
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
