// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/dave/dst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaseTypeName(t *testing.T) {
	tests := []struct {
		name     string
		typeSrc  string
		expected string
	}{
		{
			name:     "simple ident",
			typeSrc:  "int",
			expected: "int",
		},
		{
			name:     "pointer type",
			typeSrc:  "*string",
			expected: "string",
		},
		{
			name:     "double pointer",
			typeSrc:  "**float64",
			expected: "float64",
		},
		{
			name:     "package qualified type",
			typeSrc:  "pkg.Type",
			expected: "Type",
		},
		{
			name:     "pointer to package qualified type",
			typeSrc:  "*pkg.Type",
			expected: "Type",
		},
		{
			name:     "interface type",
			typeSrc:  "interface{}",
			expected: "interface{}",
		},
		{
			name:     "array type",
			typeSrc:  "[]int",
			expected: "int",
		},
		{
			name:     "nested array type",
			typeSrc:  "[][]string",
			expected: "string",
		},
		{
			name:     "array of pointer type",
			typeSrc:  "[]*int",
			expected: "int",
		},
		{
			name:     "array of package qualified type",
			typeSrc:  "[]pkg.Type",
			expected: "Type",
		},
		{
			name:     "ellipsis type",
			typeSrc:  "...int",
			expected: "int",
		},
		{
			name:     "ellipsis of pointer type",
			typeSrc:  "...*string",
			expected: "string",
		},
		{
			name:     "ellipsis of package qualified type",
			typeSrc:  "...pkg.Type",
			expected: "Type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse a function with the type as a parameter
			src := "package main\nfunc f(p " + tt.typeSrc + ") {}"
			parser := ast.NewAstParser()
			file, err := parser.ParseSource(src)
			require.NoError(t, err)

			funcDecl, ok := file.Decls[0].(*dst.FuncDecl)
			require.True(t, ok)
			require.NotNil(t, funcDecl.Type.Params)
			require.Len(t, funcDecl.Type.Params.List, 1)

			typeExpr := funcDecl.Type.Params.List[0].Type
			result := baseTypeName(typeExpr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCheckHookDecl(t *testing.T) {
	tests := []struct {
		name        string
		trampSrc    string
		hookSrc     string
		before      bool
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid before hook - pointer types match value types",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string, param1 *int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
func H1Before(ctx inst.HookContext, p1 string, p2 int) {}`,
			before: true,
		},
		{
			name: "valid after hook - pointer types match value types",
			trampSrc: `
package main
func OtelAfterTrampoline(hookContext *HookContext, ret0 *float32, ret1 *error) {}`,
			hookSrc: `
package testdata
import "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
func H1After(ctx inst.HookContext, r1 float32, r2 error) {}`,
			before: false,
		},
		{
			name: "invalid - missing HookContext in hook",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
func H1Before(p1 string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "expected 2 params, got 1",
		},
		{
			name: "invalid - type mismatch",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string, param1 *int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
func H1Before(ctx inst.HookContext, p1 string, p2 string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trampFunc := parseFunc(t, tt.trampSrc)
			hookFunc := parseFunc(t, tt.hookSrc)

			ip := &InstrumentPhase{}
			if tt.before {
				ip.beforeTrampFunc = trampFunc
			} else {
				ip.afterTrampFunc = trampFunc
			}

			err := ip.checkHookDecl(hookFunc, tt.before)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
