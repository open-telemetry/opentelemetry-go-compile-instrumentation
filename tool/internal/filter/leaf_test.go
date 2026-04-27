// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
)

func parseSource(t *testing.T, src string) *filter.MatchContext {
	t.Helper()
	parser := ast.NewAstParser()
	tree, err := parser.ParseSource(src)
	if err != nil {
		t.Fatalf("parseSource: %v", err)
	}
	return &filter.MatchContext{
		ImportPath: "example.com/pkg",
		SourceFile: "/tmp/source.go",
		AST:        tree,
	}
}

func TestFuncFilter_Match(t *testing.T) {
	ctx := parseSource(t, `package main

func Foo() {}
type MyType struct{}
func (m *MyType) Method() {}
`)

	tests := []struct {
		name string
		f    *filter.FuncFilter
		want bool
	}{
		{name: "free function", f: &filter.FuncFilter{Func: "Foo"}, want: true},
		{name: "method with recv", f: &filter.FuncFilter{Func: "Method", Recv: "*MyType"}, want: true},
		{name: "wrong recv", f: &filter.FuncFilter{Func: "Method", Recv: "*Other"}, want: false},
		{name: "method without recv", f: &filter.FuncFilter{Func: "Method"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.Match(ctx); got != tt.want {
				t.Fatalf("FuncFilter.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStructFilter_Match(t *testing.T) {
	ctx := parseSource(t, `package main

type Server struct{}
func NotAStruct() {}
`)

	if !(&filter.StructFilter{Struct: "Server"}).Match(ctx) {
		t.Fatal("StructFilter.Match(Server) = false, want true")
	}
	if (&filter.StructFilter{Struct: "NotAStruct"}).Match(ctx) {
		t.Fatal("StructFilter.Match(NotAStruct) = true, want false")
	}
}
