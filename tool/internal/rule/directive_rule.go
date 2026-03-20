// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstDirectiveRule represents a rule that matches AST nodes annotated with
// magic comments (e.g., //otelc:span). This is a pure filter with no advice —
// it becomes useful when combinators like all-of land.
type InstDirectiveRule struct {
	InstBaseRule `yaml:",inline"`

	Directive string `json:"directive" yaml:"directive"` // The directive name to match (without //)
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
	return nil
}
