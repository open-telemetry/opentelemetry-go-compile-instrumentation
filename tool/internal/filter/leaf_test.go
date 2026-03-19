// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"testing"

	pkgast "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
)

// parseSource parses in-memory Go source into a decorated AST.
// This avoids filesystem I/O in tests while exercising the same parser path
// used by production code.
func parseSource(t *testing.T, src string) *filter.MatchContext {
	t.Helper()
	parser := pkgast.NewAstParser()
	tree, err := parser.ParseSource(src)
	if err != nil {
		t.Fatalf("parseSource: %v", err)
	}
	return &filter.MatchContext{
		ImportPath: "example.com/pkg",
		SourceFile: "source.go",
		AST:        tree,
	}
}

func TestFuncFilter_Match(t *testing.T) {
	ctx := parseSource(t, `package main

func Foo() {}
func Bar(x int) string { return "" }

type MyType struct{}

func (m *MyType) Method() {}
func (m MyType) ValueMethod() {}
`)

	tests := []struct {
		name string
		f    *filter.FuncFilter
		want bool
	}{
		{
			name: "matches free function",
			f:    &filter.FuncFilter{Func: "Foo"},
			want: true,
		},
		{
			name: "matches free function with parameters",
			f:    &filter.FuncFilter{Func: "Bar"},
			want: true,
		},
		{
			name: "no match for unknown function",
			f:    &filter.FuncFilter{Func: "Baz"},
			want: false,
		},
		{
			name: "matches method with pointer receiver",
			f:    &filter.FuncFilter{Func: "Method", Recv: "*MyType"},
			want: true,
		},
		{
			name: "matches method with value receiver",
			f:    &filter.FuncFilter{Func: "ValueMethod", Recv: "MyType"},
			want: true,
		},
		{
			name: "no match: method name correct but wrong receiver",
			f:    &filter.FuncFilter{Func: "Method", Recv: "*OtherType"},
			want: false,
		},
		{
			name: "no match: free function when receiver specified",
			f:    &filter.FuncFilter{Func: "Foo", Recv: "*MyType"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.f.Match(ctx)
			if got != tt.want {
				t.Errorf("FuncFilter{Func: %q, Recv: %q}.Match() = %v, want %v",
					tt.f.Func, tt.f.Recv, got, tt.want)
			}
		})
	}
}

func TestStructFilter_Match(t *testing.T) {
	ctx := parseSource(t, `package main

type MyStruct struct {
	Field string
}

type OtherStruct struct{}

func NotAStruct() {}
`)

	tests := []struct {
		name string
		f    *filter.StructFilter
		want bool
	}{
		{
			name: "matches existing struct",
			f:    &filter.StructFilter{Struct: "MyStruct"},
			want: true,
		},
		{
			name: "matches another struct",
			f:    &filter.StructFilter{Struct: "OtherStruct"},
			want: true,
		},
		{
			name: "no match for unknown struct",
			f:    &filter.StructFilter{Struct: "UnknownStruct"},
			want: false,
		},
		{
			name: "no match for function name",
			f:    &filter.StructFilter{Struct: "NotAStruct"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.f.Match(ctx)
			if got != tt.want {
				t.Errorf("StructFilter{Struct: %q}.Match() = %v, want %v",
					tt.f.Struct, got, tt.want)
			}
		})
	}
}
