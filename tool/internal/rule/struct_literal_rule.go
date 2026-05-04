// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstStructLiteralRule represents a rule that wraps a struct literal with a custom template.
// For example, to wrap `net/http.Server{}` with `otelhttp.NewServer`:
//
//	wrap_struct_literal:
//	  target: "*"
//	  struct_literal: "net/http.Server"
//	  match: "value-only" # value-only|pointer-only|any
//	  template: |
//	    func(s http.Server) http.Server {
//	        return s
//	    }({{ . }})
type InstStructLiteralRule struct {
	InstBaseRule `yaml:",inline"`

	StructLiteral string `json:"struct_literal" yaml:"struct_literal"` // The type name of the struct literal to be matched
	Match         string `json:"match"          yaml:"match"`          // "value-only", "pointer-only", or "any"
	Template      string `json:"template"       yaml:"template"`       // The Go template to wrap the literal
}

// NewInstStructLiteralRule loads and validates an InstStructLiteralRule from YAML data.
func NewInstStructLiteralRule(data []byte, name string) (*InstStructLiteralRule, error) {
	var r InstStructLiteralRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid struct_literal rule %q", name)
	}
	return &r, nil
}

func (r *InstStructLiteralRule) validate() error {
	if strings.TrimSpace(r.StructLiteral) == "" {
		return ex.Newf("struct_literal cannot be empty")
	}
	if strings.TrimSpace(r.Template) == "" {
		return ex.Newf("template cannot be empty")
	}
	match := strings.ToLower(strings.TrimSpace(r.Match))
	switch match {
	case "":
		r.Match = "any" // default to any
	case "value-only", "pointer-only", "any":
		r.Match = match
	default:
		return ex.Newf("match must be 'value-only', 'pointer-only', or 'any'")
	}
	return nil
}
