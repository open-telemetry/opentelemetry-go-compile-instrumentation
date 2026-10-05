// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// funcDeclFromSrc parses a single function declaration from a source snippet
// and returns its *ast.FuncDecl, so the AST helpers can be exercised against
// realistic receiver, parameter, and result lists.
func funcDeclFromSrc(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+src, 0)
	require.NoError(t, err)
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn
		}
	}
	t.Fatalf("no function declaration found in source: %q", src)
	return nil
}

func TestIsClientPointerRecv(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"pointer to Client", "func (c *Client) M() {}", true},
		{"value Client", "func (c Client) M() {}", false},
		{"pointer to other type", "func (o *Other) M() {}", false},
		{"no receiver", "func M() {}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := funcDeclFromSrc(t, tt.src)
			require.Equal(t, tt.want, isClientPointerRecv(fn.Recv))
		})
	}
}

func TestHasLeadingContext(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"leading context.Context", "func M(ctx context.Context) {}", true},
		{"non-context first param", "func M(x int) {}", false},
		{"no params", "func M() {}", false},
		{"context not first", "func M(x int, ctx context.Context) {}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := funcDeclFromSrc(t, tt.src)
			require.Equal(t, tt.want, hasLeadingContext(fn.Type.Params))
		})
	}
}

func TestLastResultIsError(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"single error result", "func M() error { return nil }", true},
		{"trailing error result", "func M() (int, error) { return 0, nil }", true},
		{"non-error result", "func M() int { return 0 }", false},
		{"no results", "func M() {}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := funcDeclFromSrc(t, tt.src)
			require.Equal(t, tt.want, lastResultIsError(fn.Type.Results))
		})
	}
}

func TestFieldCount(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"no params", "func M() {}", 0},
		{"single named param", "func M(a int) {}", 1},
		{"grouped named params", "func M(a, b int, c string) {}", 3},
		{"anonymous params", "func M(int, string) {}", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := funcDeclFromSrc(t, tt.src)
			require.Equal(t, tt.want, fieldCount(fn.Type.Params))
		})
	}
}

func TestFieldCountNil(t *testing.T) {
	require.Equal(t, 0, fieldCount(nil))
}

func TestTypeString(t *testing.T) {
	// Each case declares a single param whose type exercises one branch of
	// typeString, then asserts the rendered string.
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"identifier", "func M(x int) {}", "int"},
		{"selector", "func M(x context.Context) {}", "context.Context"},
		{"pointer", "func M(x *Client) {}", "*Client"},
		{"slice", "func M(x []byte) {}", "[]byte"},
		{"variadic", "func M(x ...int) {}", "...int"},
		{"interface", "func M(x interface{}) {}", "interface{}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := funcDeclFromSrc(t, tt.src)
			paramType := fn.Type.Params.List[0].Type
			require.Equal(t, tt.want, typeString(paramType))
		})
	}
}
