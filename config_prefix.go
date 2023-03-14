// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"fmt"
	"strings"

	"github.com/hashicorp/consul-template/config"
	dep "github.com/hashicorp/consul-template/dependency"
)

// PrefixConfig is the representation of a key prefix.
type PrefixConfig struct {
	Datacenter  *string          `mapstructure:"datacenter"`
	Dependency  *dep.KVListQuery `mapstructure:"-"`
	Destination *string          `mapstructure:"destination"`
	Source      *string          `mapstructure:"source"`
}

// ParsePrefixConfig parses a prefix of the format "source@dc:destination" into
// the PrefixConfig.
func ParsePrefixConfig(s string) (*PrefixConfig, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("missing prefix")
	}

	parts := strings.SplitN(s, ":", 2)

	var source, destination string
	switch len(parts) {
	case 1:
		source, destination = parts[0], ""
	case 2:
		source, destination = parts[0], parts[1]
	default:
		return nil, fmt.Errorf("invalid format: %q", s)
	}

	if !dep.KVListQueryRe.MatchString(source) {
		return nil, fmt.Errorf("invalid source format: %q", source)
	}
	m := regexpMatch(dep.KVListQueryRe, source)

	prefix, dc := m["prefix"], m["dc"]

	if dc == "" {
		return nil, fmt.Errorf("missing datacenter")
	}

	if prefix == "" {
		return nil, fmt.Errorf("missing prefix")
	}

	d, err := dep.NewKVListQuery(source)
	if err != nil {
		return nil, err
	}

	if destination == "" {
		destination = prefix
	}

	return &PrefixConfig{
		Datacenter:  config.String(dc),
		Dependency:  d,
		Destination: config.String(destination),
		Source:      config.String(prefix),
	}, nil
}

func DefaultPrefixConfig() *PrefixConfig {
	return &PrefixConfig{}
}

func (c *PrefixConfig) Copy() *PrefixConfig {
	if c == nil {
		return nil
	}

	var o PrefixConfig

	o.Dependency = c.Dependency

	o.Source = c.Source

	o.Datacenter = c.Datacenter

	o.Destination = c.Destination

	return &o
}

func (c *PrefixConfig) Merge(o *PrefixConfig) *PrefixConfig {
	if c == nil {
		if o == nil {
			return nil
		}
		return o.Copy()
	}

	if o == nil {
		return c.Copy()
	}

	r := c.Copy()

	if o.Dependency != nil {
		r.Dependency = o.Dependency
	}

	if o.Source != nil {
		r.Source = o.Source
	}

	if o.Datacenter != nil {
		r.Datacenter = o.Datacenter
	}

	if o.Destination != nil {
		r.Destination = o.Destination
	}

	return r
}

func (c *PrefixConfig) Finalize() {
	if c.Source == nil {
		c.Source = config.String("")
	}

	if c.Datacenter == nil {
		c.Datacenter = config.String("")
	}

	if c.Destination == nil {
		c.Destination = config.String("")
	}
}

func (c *PrefixConfig) GoString() string {
	if c == nil {
		return "(*PrefixConfig)(nil)"
	}

	return fmt.Sprintf("&PrefixConfig{"+
		"Datacenter:%s, "+
		"Dependency:%s, "+
		"Destination:%s, "+
		"Source:%s"+
		"}",
		config.StringGoString(c.Datacenter),
		c.Dependency,
		config.StringGoString(c.Destination),
		config.StringGoString(c.Source),
	)
}

type PrefixConfigs []*PrefixConfig

func DefaultPrefixConfigs() *PrefixConfigs {
	return &PrefixConfigs{}
}

func (c *PrefixConfigs) Copy() *PrefixConfigs {
	if c == nil {
		return nil
	}

	o := make(PrefixConfigs, len(*c))
	for i, t := range *c {
		o[i] = t.Copy()
	}
	return &o
}

func (c *PrefixConfigs) Merge(o *PrefixConfigs) *PrefixConfigs {
	if c == nil {
		if o == nil {
			return nil
		}
		return o.Copy()
	}

	if o == nil {
		return c.Copy()
	}

	r := c.Copy()

	*r = append(*r, *o...)

	return r
}

func (c *PrefixConfigs) Finalize() {
	if c == nil {
		*c = *DefaultPrefixConfigs()
	}

	for _, t := range *c {
		t.Finalize()
	}
}

func (c *PrefixConfigs) GoString() string {
	if c == nil {
		return "(*PrefixConfigs)(nil)"
	}

	s := make([]string, len(*c))
	for i, t := range *c {
		s[i] = t.GoString()
	}

	return "{" + strings.Join(s, ", ") + "}"
}
