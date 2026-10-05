// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"strings"
	"text/template"

	"go.opentelemetry.io/otelc/tool/ex"
)

// FuncTemplate is a text/template-backed template rendered against the
// shared function template variables (FuncName, FuncArgument N, FuncReturn
// N, FuncArgumentCount, FuncReturnCount). It is used by both directive rules
// and raw rules.
type FuncTemplate struct {
	tmpl *template.Template
}

// ParseFuncTemplate parses a rule's template text. Note that "{{ ... }}" will
// always be parsed as a template action.
func ParseFuncTemplate(text string) (*FuncTemplate, error) {
	tmpl, err := template.New("func").Parse(text)
	if err != nil {
		return nil, ex.Wrap(err)
	}
	return &FuncTemplate{tmpl: tmpl}, nil
}

// Execute renders the template against data (typically a value exposing
// FuncName/FuncArgument/... methods).
func (t *FuncTemplate) Execute(data any) (string, error) {
	var sb strings.Builder
	if err := t.tmpl.Execute(&sb, data); err != nil {
		return "", ex.Wrap(err)
	}
	return sb.String(), nil
}
