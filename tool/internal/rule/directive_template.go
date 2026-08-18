// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"
	"text/template"

	"go.opentelemetry.io/otelc/tool/ex"
)

// DirectiveTemplate is a text/template-backed template for directive rule
// bodies.
type DirectiveTemplate struct {
	tmpl *template.Template
}

// ParseDirectiveTemplate parses a directive rule's template text. Note that
// "{{ ... }}" will always be parsed as a template action.
func ParseDirectiveTemplate(text string) (*DirectiveTemplate, error) {
	tmpl, err := template.New("directive").Parse(text)
	if err != nil {
		return nil, ex.Wrap(err)
	}
	return &DirectiveTemplate{tmpl: tmpl}, nil
}

// Execute renders the template against data (typically a value exposing
// FuncName/FuncArgument/... methods).
func (d *DirectiveTemplate) Execute(data any) (string, error) {
	var sb strings.Builder
	if err := d.tmpl.Execute(&sb, data); err != nil {
		return "", ex.Wrap(err)
	}
	return sb.String(), nil
}
