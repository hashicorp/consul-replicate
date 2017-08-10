package main

import (
	"fmt"
	"strings"

	"github.com/hashicorp/consul-template/config"
)

// ExcludeConfig is a key path prefix to exclude from replication
type ExcludeConfig struct {
	Source *string `mapstructure:"source"`
}

func ParseExcludeConfig(s string) (*ExcludeConfig, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("missing exclude")
	}
	return &ExcludeConfig{
		Source: config.String(s),
	}, nil
}

func DefaultExcludeConfig() *ExcludeConfig {
	return &ExcludeConfig{}
}

func (c *ExcludeConfig) Copy() *ExcludeConfig {
	if c == nil {
		return nil
	}

	var o ExcludeConfig

	o.Source = c.Source

	return &o
}

func (c *ExcludeConfig) Merge(o *ExcludeConfig) *ExcludeConfig {
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

	if o.Source != nil {
		r.Source = o.Source
	}

	return r
}

func (c *ExcludeConfig) Finalize() {
	if c.Source == nil {
		c.Source = config.String("")
	}
}

func (c *ExcludeConfig) GoString() string {
	if c == nil {
		return "(*ExcludeConfig)(nil)"
	}

	return fmt.Sprintf("&ExcludeConfig{"+
		"Source:%s"+
		"}",
		config.StringGoString(c.Source),
	)
}

type ExcludeConfigs []*ExcludeConfig

func DefaultExcludeConfigs() *ExcludeConfigs {
	return &ExcludeConfigs{}
}

func (c *ExcludeConfigs) Copy() *ExcludeConfigs {
	if c == nil {
		return nil
	}

	o := make(ExcludeConfigs, len(*c))
	for i, t := range *c {
		o[i] = t.Copy()
	}
	return &o
}

func (c *ExcludeConfigs) Merge(o *ExcludeConfigs) *ExcludeConfigs {
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

func (c *ExcludeConfigs) Finalize() {
	if c == nil {
		*c = *DefaultExcludeConfigs()
	}

	for _, t := range *c {
		t.Finalize()
	}
}

func (c *ExcludeConfigs) GoString() string {
	if c == nil {
		return "(*ExcludeConfigs)(nil)"
	}

	s := make([]string, len(*c))
	for i, t := range *c {
		s[i] = t.GoString()
	}

	return "{" + strings.Join(s, ", ") + "}"
}
