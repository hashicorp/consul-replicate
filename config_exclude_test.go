// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/consul-template/config"
)

func TestExcludeConfig(t *testing.T) {
	cases := []struct {
		name string
		s    string
		e    *ExcludeConfig
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
			"name",
			"foo",
			&ExcludeConfig{
				Source: config.String("foo"),
			},
			false,
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("%d_%s", i, tc.name), func(t *testing.T) {
			p, err := ParseExcludeConfig(tc.s)
			if (err != nil) != tc.err {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(tc.e, p) {
				t.Errorf("\nexp: %#v\nact: %#v", tc.e, p)
			}
		})
	}
}
