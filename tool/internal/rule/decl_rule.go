// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstDeclRule represents a rule that matches a named top-level declaration
// (function, type, variable, or constant) and applies an action to it.
//
// Example YAML:
//
//	assign_default_transport:
//	  target: net/http
//	  kind: var
//	  identifier: DefaultTransport
//	  replace: |
//	    &http.Transport{MaxIdleConns: 100}
type InstDeclRule struct {
	InstBaseRule `yaml:",inline"`

	// Kind optionally constrains the kind of declaration to match.
	// Valid values: "func", "var", "const", "type", or "" (match any).
	Kind string `json:"kind" yaml:"kind"`

	// Identifier is the name of the top-level declaration to match.
	Identifier string `json:"identifier" yaml:"identifier"`

	// Replace is a Go expression to assign as the value of the matched
	// var or const declaration.
	Replace string `json:"replace" yaml:"replace"`
}

// NewInstDeclRule loads and validates an InstDeclRule from YAML data.
func NewInstDeclRule(data []byte, name string) (*InstDeclRule, error) {
	var r InstDeclRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid decl rule %q", name)
	}
	return &r, nil
}

// validDeclKinds lists accepted values for the kind field.
// An empty string ("") means match any kind.
var validDeclKinds = map[string]bool{ //nolint:gochecknoglobals // private lookup table
	"":      true, // match any
	"func":  true,
	"var":   true,
	"const": true,
	"type":  true,
}

func (r *InstDeclRule) validate() error {
	if strings.TrimSpace(r.Identifier) == "" {
		return ex.Newf("identifier cannot be empty")
	}
	if !validDeclKinds[r.Kind] {
		return ex.Newf("kind %q is invalid; must be one of: func, var, const, type, or empty", r.Kind)
	}
	if strings.TrimSpace(r.Replace) == "" {
		return ex.Newf("replace cannot be empty")
	}
	if r.Kind == "func" || r.Kind == "type" {
		return ex.Newf("replace is not valid when kind is %q", r.Kind)
	}
	return nil
}
