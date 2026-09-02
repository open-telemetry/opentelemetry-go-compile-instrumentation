// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strconv"
	"strings"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
	"gopkg.in/yaml.v3"
)

// InstDirectiveRule represents a rule that instruments functions annotated with
// magic comments (e.g., //otelc:span) by prepending templated Go code into
// their bodies. The template supports {{.FuncName}} as a placeholder.
type InstDirectiveRule struct {
	InstBaseRule `yaml:",inline"`

	Directive string `json:"directive" yaml:"directive"`
	Template  string `json:"template"  yaml:"template"`
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
	if _, err := ParseFuncTemplate(r.Template); err != nil {
		return ex.Wrapf(err, "invalid template syntax")
	}
	return nil
}

// Identity returns a content-derived key used to salt the synthetic argument
// and return-value names FuncArgument/FuncReturn assign (see
// collectArguments/collectReturnValues), the same way InstFuncRule.Identity
// salts func-rule trampoline names (issue #560, PR #1035). It is a function
// purely of what the rule does — its target, version, directive, and
// template — never of the rule's name.
func (r *InstDirectiveRule) Identity() string {
	enc := func(s string) string { return strconv.Itoa(len(s)) + ":" + s }
	parts := []string{enc(r.Target), enc(r.Version), enc(r.Directive), enc(r.Template)}
	return util.CRC32(strings.Join(parts, ""))
}
