package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/sseekamp/dnstonetbox/dhcpd"
	"github.com/sseekamp/dnstonetbox/netbox"
	"github.com/sseekamp/dnstonetbox/nsd"
	"github.com/sseekamp/dnstonetbox/unbound"
)

// Config is the top-level configuration structure.
type Config struct {
	Netbox   netbox.Config   `yaml:"netbox"`
	NSD      nsd.Config      `yaml:"nsd"`
	Unbound  unbound.Config  `yaml:"unbound"`
	DHCPD    dhcpd.Config    `yaml:"dhcpd"`
	Interval duration        `yaml:"interval"`
}

// duration wraps time.Duration to support YAML strings like "5m" or "1h".
type duration struct {
	time.Duration
}

func (d *duration) UnmarshalYAML(value *yaml.Node) error {
	dur, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	d.Duration = dur
	return nil
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	cfg := &Config{
		Interval: duration{5 * time.Minute},
	}
	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}
