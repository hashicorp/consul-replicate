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
			&Prefix{Source: "1", Destination: "4"},
			&Prefix{Source: "2", Destination: "5"},
			&Prefix{Source: "3", Destination: "6"},
			&Prefix{Source: "4", Destination: "7"},
		},
		Excludes: []*Exclude{
			&Exclude{Source: "3"},
		},
		ExcludeMatches: []*ExcludeMatch{
			&ExcludeMatch{Source: "2"},
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

	if runner.clients == nil {
		t.Errorf("expected %#v to not be %#v", runner.clients, nil)
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
