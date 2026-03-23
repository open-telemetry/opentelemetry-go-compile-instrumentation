// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"
	"text/template"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"gopkg.in/yaml.v3"
)

// InstDirectiveRule represents a rule that instruments functions annotated with
// magic comments (e.g., //otelc:span) by prepending templated Go code into
// their bodies.
//
// The template is executed with a DirectiveTemplateData context, giving access
// to function-specific values such as {{.FuncName}}.
type InstDirectiveRule struct {
	InstBaseRule `yaml:",inline"`

	Directive string `json:"directive" yaml:"directive"` // The directive name to match (without //)
	Template  string `json:"template"  yaml:"template"`  // Go text/template rendered into code prepended to matching functions
}

// DirectiveTemplateData is the dot value available inside a directive template.
// It is intentionally small for now; fields will be added as needed.
type DirectiveTemplateData struct {
	FuncName string // Name of the annotated function
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
	if _, err := template.New("").Parse(r.Template); err != nil {
		return ex.Wrapf(err, "invalid template syntax")
	}
	return nil
}

// Render executes the template with the given function context and returns
// the resulting Go source snippet.
func (r *InstDirectiveRule) Render(data DirectiveTemplateData) (string, error) {
	tmpl, err := template.New("").Parse(r.Template)
	if err != nil {
		return "", ex.Wrap(err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", ex.Wrap(err)
	}
	return buf.String(), nil
}
