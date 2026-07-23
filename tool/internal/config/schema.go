// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the root of the declarative configuration model.
// This mirrors the upstream OpenTelemetry configuration schema, specifically
// focusing on the instrumentation/development node.
type Config struct {
	Instrumentation struct {
		Development struct {
			General map[string]any `yaml:"general"`
			Go      map[string]any `yaml:"go"`
		} `yaml:"development"`
	} `yaml:"instrumentation"`
}

// ParseFile parses an OpenTelemetry declarative configuration file.
func ParseFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
