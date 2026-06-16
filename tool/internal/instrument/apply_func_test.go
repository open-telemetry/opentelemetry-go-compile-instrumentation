// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestApplyFuncRuleSignatureFilterMismatchIsLookupMiss(t *testing.T) {
	parser := ast.NewAstParser()
	root, err := parser.ParseSource(`package main

func Target(value string) error { return nil }
`)
	require.NoError(t, err)

	sig := rule.FuncSignature{Args: []string{"int"}, Returns: []string{"error"}}
	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "mismatch"},
		Func:         "Target",
		Before:       "BeforeTarget",
		Signature:    &sig,
	}

	err = newTestPhase().applyFuncRule(context.Background(), funcRule, root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can not find function Target")
}

func TestCollectArguments(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected []string
	}{
		{
			name:     "no params no receiver",
			src:      "package main\nfunc F() {}",
			expected: []string{},
		},
		{
			name:     "named params",
			src:      "package main\nfunc F(a int, b string) {}",
			expected: []string{"a", "b"},
		},
		{
			name:     "unnamed params (len(Names) == 0)",
			src:      "package main\nfunc F(int, string) {}",
			expected: []string{"_ignoredParam0", "_ignoredParam1"},
		},
		{
			name:     "mixed named and unnamed params via group",
			src:      "package main\nfunc F(a, b int) {}",
			expected: []string{"a", "b"},
		},
		{
			name:     "underscore params",
			src:      "package main\nfunc F(_ int, _ string) {}",
			expected: []string{"_ignoredParam0", "_ignoredParam1"},
		},
		{
			name:     "named receiver",
			src:      "package main\ntype T struct{}\nfunc (t T) F() {}",
			expected: []string{"t"},
		},
		{
			name:     "unnamed receiver",
			src:      "package main\ntype T struct{}\nfunc (T) F() {}",
			expected: []string{"_ignoredParam0"},
		},
		{
			name:     "named receiver with params",
			src:      "package main\ntype T struct{}\nfunc (t T) F(a int, b string) {}",
			expected: []string{"t", "a", "b"},
		},
		{
			name:     "unnamed receiver with unnamed params",
			src:      "package main\ntype T struct{}\nfunc (T) F(int, string) {}",
			expected: []string{"_ignoredParam0", "_ignoredParam1", "_ignoredParam2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcDecl := parseFunc(t, tt.src)
			args := collectArguments(funcDecl)
			assert.Equal(t, tt.expected, args)
		})
	}
}

func TestCollectReturnValues(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected []string
	}{
		{
			name:     "no return values",
			src:      "package main\nfunc F() {}",
			expected: nil,
		},
		{
			name:     "named return values",
			src:      "package main\nfunc F() (a int, b string) { return }",
			expected: []string{"a", "b"},
		},
		{
			name:     "unnamed return values",
			src:      "package main\nfunc F() (int, string) { return 0, \"\" }",
			expected: []string{"_unnamedRetVal0", "_unnamedRetVal1"},
		},
		{
			name:     "underscore return values",
			src:      "package main\nfunc F() (_ int, _ string) { return }",
			expected: []string{"_ignoredParam0", "_ignoredParam1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcDecl := parseFunc(t, tt.src)
			retVals := collectReturnValues(funcDecl)
			assert.Equal(t, tt.expected, retVals)
		})
	}
}
