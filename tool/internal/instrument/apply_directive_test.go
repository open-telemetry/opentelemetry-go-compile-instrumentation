// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestRenderDirective(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		template string
		expected string
	}{
		{
			name:     "FuncName no spaces",
			src:      "package main\nfunc Foo() {}",
			template: "call({{.FuncName}})",
			expected: "call(Foo)",
		},
		{
			name:     "FuncName with spaces",
			src:      "package main\nfunc Foo() {}",
			template: "call({{ .FuncName }})",
			expected: "call(Foo)",
		},
		{
			name:     "FuncArgument",
			src:      "package main\nfunc Foo(ctx int, name string) {}",
			template: "use({{ .FuncArgument 0 }}, {{ .FuncArgument 1 }})",
			expected: "use(ctx, name)",
		},
		{
			name:     "FuncReturn",
			src:      "package main\nfunc Foo() (int, error) { return 0, nil }",
			template: "check({{ .FuncReturn 0 }}, {{ .FuncReturn 1 }})",
			expected: "check(_unnamedRetVal0, _unnamedRetVal1)",
		},
		{
			name:     "counts",
			src:      "package main\nfunc Foo(a, b int) (int, error) { return 0, nil }",
			template: "n={{.FuncArgumentCount}} m={{.FuncReturnCount}}",
			expected: "n=2 m=2",
		},
		{
			name:     "template without any Func tag leaves function untouched",
			src:      "package main\nfunc Foo() {}",
			template: `println("static")`,
			expected: `println("static")`,
		},
		{
			name:     "trim markers",
			src:      "package main\nfunc Foo() {}",
			template: "call({{- .FuncName -}})",
			expected: "call(Foo)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcDecl := parseFunc(t, tt.src)
			tmpl, err := rule.ParseDirectiveTemplate(tt.template)
			require.NoError(t, err)

			result, err := renderDirective(tmpl, newFuncTemplateData(funcDecl, nil))

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseDirectiveTemplate_UnknownTagFails(t *testing.T) {
	_, err := rule.ParseDirectiveTemplate("{{Bogus}}")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not defined")
}

func TestParseDirectiveTemplate_CompositeLiteralFails(t *testing.T) {
	// text/template treats every "{{ ... }}" as an action, so incidental
	// adjacent Go braces (e.g. a composite literal like []Point{{X: 1, Y: 2}})
	// fail to parse. Datadog/orchestrion's code.Template has the same
	// limitation for the same reason (plain text/template.Parse with no
	// escaping).
	_, err := rule.ParseDirectiveTemplate(`attrs := []Point{{X: 1, Y: 2}}; call({{.FuncName}})`)

	require.Error(t, err)
}

func TestRenderDirective_OutOfRangeArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	tmpl, err := rule.ParseDirectiveTemplate("{{.FuncArgument 0}}")
	require.NoError(t, err)

	_, err = renderDirective(tmpl, newFuncTemplateData(funcDecl, nil))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}
