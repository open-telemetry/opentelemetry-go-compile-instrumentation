// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/ast"
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
			name:     "non-empty interface type",
			typeSrc:  "interface{ Read(p []byte) (n int, err error) }",
			expected: "interface{Read([]byte) (int, error)}",
		},
		{
			name:     "interface type with embedded interface",
			typeSrc:  "interface{ pkg.Reader }",
			expected: "interface{Reader}",
		},
		{
			name:     "empty struct type",
			typeSrc:  "struct{}",
			expected: "struct{}",
		},
		{
			name:     "non-empty struct type",
			typeSrc:  "struct{ Name string }",
			expected: "struct{Name string}",
		},
		{
			name:     "struct type with embedded field",
			typeSrc:  "struct{ pkg.Base }",
			expected: "struct{Base}",
		},
		{
			name:     "fixed-size array type with literal length",
			typeSrc:  "[5]int",
			expected: "[5]int",
		},
		{
			name:     "fixed-size array type with named length",
			typeSrc:  "[N]int",
			expected: "[N]int",
		},
		{
			name:     "func type with no params or results",
			typeSrc:  "func()",
			expected: "func()",
		},
		{
			name:     "func type with unnamed params and multiple results",
			typeSrc:  "func(int, string) (bool, error)",
			expected: "func(int, string) (bool, error)",
		},
		{
			name:     "func type with a single result",
			typeSrc:  "func(int) string",
			expected: "func(int) string",
		},
		{
			name:     "generic type with one type argument",
			typeSrc:  "Foo[int]",
			expected: "Foo[int]",
		},
		{
			name:     "generic type with multiple type arguments",
			typeSrc:  "Foo[int, string]",
			expected: "Foo[int, string]",
		},
		{
			name:     "array type",
			typeSrc:  "[]int",
			expected: "[]int",
		},
		{
			name:     "nested array type",
			typeSrc:  "[][]string",
			expected: "[][]string",
		},
		{
			name:     "array of pointer type",
			typeSrc:  "[]*int",
			expected: "[]int",
		},
		{
			name:     "array of package qualified type",
			typeSrc:  "[]pkg.Type",
			expected: "[]Type",
		},
		{
			name:     "ellipsis type",
			typeSrc:  "...int",
			expected: "...int",
		},
		{
			name:     "ellipsis of pointer type",
			typeSrc:  "...*string",
			expected: "...string",
		},
		{
			name:     "ellipsis of package qualified type",
			typeSrc:  "...pkg.Type",
			expected: "...Type",
		},
		{
			name:     "map type",
			typeSrc:  "map[string]int",
			expected: "map[string]int",
		},
		{
			name:     "map of package qualified types",
			typeSrc:  "map[pkg.Key]*pkg.Val",
			expected: "map[Key]Val",
		},
		{
			name:     "channel type",
			typeSrc:  "chan bool",
			expected: "chan bool",
		},
		{
			name:     "receive channel type",
			typeSrc:  "<-chan int",
			expected: "<-chan int",
		},
		{
			name:     "send channel type",
			typeSrc:  "chan<- string",
			expected: "chan<- string",
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

func TestBaseTypeName_NilExpr(t *testing.T) {
	assert.Empty(t, baseTypeName(nil))
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
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 string, p2 int) {}`,
			before: true,
		},
		{
			name: "valid before hook - slice and variadic compatibility",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *[]string, param1 *[]int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 []string, p2 ...int) {}`,
			before: true,
		},
		{
			name: "valid before hook - map and chan types",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *map[string]int, param1 *chan bool) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 map[string]int, p2 chan bool) {}`,
			before: true,
		},
		{
			name: "valid before hook - any/interface{} wildcard",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 any) {}`,
			before: true,
		},
		{
			name: "valid before hook - slice of any matching variadic interface{}",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *[]any) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 ...interface{}) {}`,
			before: true,
		},
		{
			name: "valid before hook - slice of concrete type matching variadic interface{}",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *[]SpanEndOption) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 ...interface{}) {}`,
			before: true,
		},
		{
			name: "valid after hook - pointer types match value types",
			trampSrc: `
package main
func OtelAfterTrampoline(hookContext *HookContext, ret0 *float32, ret1 *error) {}`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1After(ctx hook.HookContext, r1 float32, r2 error) {}`,
			before: false,
		},
		{
			name: "invalid - before hook first param is not HookContext",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
func H1Before(notCtx int, p1 string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "hook func first param must be HookContext, got int",
		},
		{
			name: "invalid - after hook param count mismatch",
			trampSrc: `
package main
func OtelAfterTrampoline(hookContext *HookContext, ret0 *string) {}`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1After(ctx hook.HookContext) {}`,
			before:      false,
			expectError: true,
			errorMsg:    "expected 2 params, got 1",
		},
		{
			name: "invalid - after hook type mismatch",
			trampSrc: `
package main
func OtelAfterTrampoline(hookContext *HookContext, ret0 *string) {}`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1After(ctx hook.HookContext, r1 int) {}`,
			before:      false,
			expectError: true,
			errorMsg:    "type mismatch, expected string, got int",
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
			name: "invalid - type mismatch basic",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string, param1 *int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 string, p2 string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch, expected int, got string",
		},
		{
			name: "invalid - scalar vs slice mismatch",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 []string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch, expected string, got []string",
		},
		{
			name: "invalid - map vs chan mismatch",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *map[string]int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 chan bool) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch, expected map[string]int, got chan bool",
		},
		{
			name: "invalid - slice element type mismatch",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *[]string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 []int) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch, expected []string, got []int",
		},
		{
			// Regression: baseTypeName used to normalize every non-empty
			// interface to the literal placeholder "interface{...}", so
			// structurally distinct interfaces collided and falsely matched.
			name: "invalid - distinct anonymous interfaces don't collide",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *interface{ Read(p []byte) (n int, err error) }) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 interface{ Write(p []byte) (n int, err error) }) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch",
		},
		{
			name: "valid before hook - matching anonymous interfaces",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *interface{ Read(p []byte) (n int, err error) }) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 interface{ Read(p []byte) (n int, err error) }) {}`,
			before: true,
		},
		{
			// Same class of bug as the anonymous interface case above, but for
			// anonymous struct types.
			name: "invalid - distinct anonymous structs don't collide",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *struct{ Name string }) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 struct{ Age int }) {}`,
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
