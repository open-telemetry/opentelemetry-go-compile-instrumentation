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
		{
			name:     "receiver excluded from FuncArgument",
			src:      "package main\ntype T struct{}\nfunc (t T) Foo(a int) {}",
			raw:      "use({{ .FuncArgument 0 }})",
			expected: "use(a)",
		},
		{
			name:     "unnamed parameter gets a synthetic name",
			raw:      "use({{ .FuncArgument 0 }})",
			src:      "package main\nfunc Foo(int) {}",
			expected: "use(_ignoredParam_h1_0)",
		},
		{
			name:     "blank parameter gets a synthetic name",
			src:      "package main\nfunc Foo(_ int) {}",
			raw:      "use({{ .FuncArgument 0 }})",
			expected: "use(_ignoredParam_h1_0)",
		},
		{
			name:     "blank named return gets a synthetic name",
			src:      "package main\nfunc Foo() (_ int) { return 0 }",
			raw:      "check({{ .FuncReturn 0 }})",
			expected: "check(_ignoredRetVal_h1_0)",
		},
		{
			name:     "named return values are collected as-is",
			src:      "package main\nfunc Foo() (a int, b error) { return 0, nil }",
			raw:      "check({{ .FuncReturn 0 }}, {{ .FuncReturn 1 }})",
			expected: "check(a, b)",
		},
		{
			name:     "control-flow actions are available",
			src:      "package main\nfunc Foo(a int) {}",
			raw:      "{{ if gt .FuncArgumentCount 0 }}has args{{ else }}no args{{ end }}",
			expected: "has args",
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

func TestRenderRawCode_HashSaltsSyntheticNames(t *testing.T) {
	// Each call parses its own funcDecl
	src := "package main\nfunc Foo(int) {}"
	raw := "use({{ .FuncArgument 0 }})"

	result1, err := renderRawCode(raw, parseFunc(t, src), "h1")
	require.NoError(t, err)

	result2, err := renderRawCode(raw, parseFunc(t, src), "h2")
	require.NoError(t, err)

	assert.NotEqual(t, result1, result2, "different hashes must salt the synthetic name differently")
	assert.Equal(t, "use(_ignoredParam_h1_0)", result1)
	assert.Equal(t, "use(_ignoredParam_h2_0)", result2)
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

func TestRenderRawCode_NegativeArgumentIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")

	_, err := renderRawCode("{{.FuncArgument -1}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestRenderRawCode_OutOfRangeReturn(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{.FuncReturn 0}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestRenderRawCode_NegativeReturnIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() (int, error) { return 0, nil }")

	_, err := renderRawCode("{{.FuncReturn -1}}", funcDecl, "h1")

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
