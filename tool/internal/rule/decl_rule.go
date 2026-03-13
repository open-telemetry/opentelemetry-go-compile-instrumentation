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
//	  declaration_of: DefaultTransport
//	  decl_kind: var
//	  assign_value: |
//	    &http.Transport{MaxIdleConns: 100}
type InstDeclRule struct {
	InstBaseRule `yaml:",inline"`

	// DeclarationOf is the name of the top-level declaration to match.
	DeclarationOf string `json:"declaration_of" yaml:"declaration_of"`

	// DeclKind optionally constrains the kind of declaration to match.
	// Valid values: "func", "var", "const", "type", or "" (match any).
	DeclKind string `json:"decl_kind" yaml:"decl_kind"`

	// AssignValue is a Go expression to assign as the value of the matched
	// var or const declaration.
	AssignValue string `json:"assign_value" yaml:"assign_value"`
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

var validDeclKinds = map[string]bool{
	"":      true,
	"func":  true,
	"var":   true,
	"const": true,
	"type":  true,
}

func (r *InstDeclRule) validate() error {
	if strings.TrimSpace(r.DeclarationOf) == "" {
		return ex.Newf("declaration_of cannot be empty")
	}
	if !validDeclKinds[r.DeclKind] {
		return ex.Newf("decl_kind %q is invalid; must be one of: func, var, const, type, or empty", r.DeclKind)
	}
	if r.AssignValue != "" && (r.DeclKind == "func" || r.DeclKind == "type") {
		return ex.Newf("assign_value is not valid when decl_kind is %q", r.DeclKind)
	}
	return nil
}
