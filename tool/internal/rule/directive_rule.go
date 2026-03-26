// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/valyala/fasttemplate"
	"gopkg.in/yaml.v3"
)

// InstDirectiveRule represents a rule that instruments functions annotated with
// magic comments (e.g., //otelc:span) by prepending templated Go code into
// their bodies. The template supports {{FuncName}} as a placeholder.
type InstDirectiveRule struct {
	InstBaseRule `yaml:",inline"`

	Directive string `json:"directive" yaml:"directive"` // The directive name to match (without //)
	Template  string `json:"template"  yaml:"template"`  // Go text/template rendered into code prepended to matching functions
}

// NewInstDirectiveRule loads and validates an InstDirectiveRule from YAML data.
func NewInstDirectiveRule(data []byte, name string) (*InstDirectiveRule, error) {
	var r InstDirectiveRule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, ex.Wrap(err)
	}
	if r.Name == "" {
		r.Name = name
	}
	if err := r.validate(); err != nil {
		return nil, ex.Wrapf(err, "invalid directive rule %q", name)
	}
	return &r, nil
}

func (r *InstDirectiveRule) validate() error {
	if strings.TrimSpace(r.Directive) == "" {
		return ex.Newf("directive cannot be empty")
	}
	if strings.Contains(r.Directive, " ") {
		return ex.Newf("directive cannot contain spaces")
	}
	if strings.HasPrefix(r.Directive, "//") {
		return ex.Newf("directive should not start with //")
	}
	if strings.TrimSpace(r.Template) == "" {
		return ex.Newf("template cannot be empty")
	}
	if _, err := fasttemplate.NewTemplate(r.Template, "{{", "}}"); err != nil {
		return ex.Wrapf(err, "invalid template syntax")
	}
	return nil
}
