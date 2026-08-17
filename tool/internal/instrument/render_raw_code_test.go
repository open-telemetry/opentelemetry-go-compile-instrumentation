// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderRawCode(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		raw      string
		expected string
	}{
		{
			name:     "no braces left untouched",
			src:      "package main\nfunc Foo(a int) {}",
			raw:      `println("static")`,
			expected: `println("static")`,
		},
		{
			name:     "FuncName",
			src:      "package main\nfunc Foo() {}",
			raw:      "call({{.FuncName}})",
			expected: "call(Foo)",
		},
		{
			name:     "FuncArgument",
			src:      "package main\nfunc Foo(ctx int, name string) {}",
			raw:      "use({{ .FuncArgument 0 }}, {{ .FuncArgument 1 }})",
			expected: "use(ctx, name)",
		},
		{
			name:     "FuncReturn",
			src:      "package main\nfunc Foo() (int, error) { return 0, nil }",
			raw:      "check({{ .FuncReturn 0 }}, {{ .FuncReturn 1 }})",
			expected: "check(_unnamedRetVal_h1_0, _unnamedRetVal_h1_1)",
		},
		{
			name:     "counts",
			src:      "package main\nfunc Foo(a, b int) (int, error) { return 0, nil }",
			raw:      "n={{.FuncArgumentCount}} m={{.FuncReturnCount}}",
			expected: "n=2 m=2",
		},
		{
			name:     "trim markers",
			src:      "package main\nfunc Foo() {}",
			raw:      "call({{- .FuncName -}})",
			expected: "call(Foo)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcDecl := parseFunc(t, tt.src)

			result, err := renderRawCode(tt.raw, funcDecl, "h1")

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderRawCode_UnknownTagFails(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{Foo}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not defined")
}

func TestRenderRawCode_CompositeLiteralFails(t *testing.T) {
	// text/template treats every "{{ ... }}" as an action, so incidental
	// adjacent Go braces (e.g. a composite literal like []Point{{X: 1, Y: 2}})
	// fail to parse. Datadog/orchestrion's code.Template has the same
	// limitation for the same reason (plain text/template.Parse with no
	// escaping).
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode(`attrs := []Point{{X: 1, Y: 2}}; call({{.FuncName}})`, funcDecl, "h1")

	require.Error(t, err)
}

func TestRenderRawCode_OutOfRangeArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{.FuncArgument 0}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestRenderRawCode_InvalidTemplateSyntax(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{.FuncName", funcDecl, "h1")

	require.Error(t, err)
}

func TestRenderRawCode_NonIntegerArgumentIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")

	_, err := renderRawCode("{{.FuncArgument abc}}", funcDecl, "h1")

	require.Error(t, err)
}
