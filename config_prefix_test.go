package main

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/consul-template/config"
)

func TestPrefixConfig(t *testing.T) {
	cases := []struct {
		name string
		s    string
		e    *PrefixConfig
		err  bool
	}{
		{
			"empty",
			"",
			nil,
			true,
		},
		{
			"empty_spaces",
			" ",
			nil,
			true,
		},
		{
			"missing_datacenter",
			"foo",
			nil,
			true,
		},
		{
			"prefix_source",
			"foo@dc",
			&PrefixConfig{
				Datacenter:  config.String("dc"),
				Destination: config.String("foo"),
				Source:      config.String("foo"),
			},
			false,
		},
		{
			"prefix_source_slash",
			"/foo@dc",
			&PrefixConfig{
				Datacenter:  config.String("dc"),
				Destination: config.String("foo"),
				Source:      config.String("foo"),
			},
			false,
		},
		{
			"prefix_destination",
			"foo@dc:bar",
			&PrefixConfig{
				Datacenter:  config.String("dc"),
				Destination: config.String("bar"),
				Source:      config.String("foo"),
			},
			false,
		},
		{
			"weird_characters",
			"@*(#42",
			nil,
			true,
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.name), func(t *testing.T) {
			p, err := ParsePrefixConfig(tc.s)
			if (err != nil) != tc.err {
				t.Fatal(err)
			}

			if p != nil {
				p.Dependency = nil
			}

			if !reflect.DeepEqual(tc.e, p) {
				t.Errorf("\nexp: %#v\nact: %#v", tc.e, p)
			}
		})
	}
}
