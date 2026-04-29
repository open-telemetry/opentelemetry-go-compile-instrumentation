// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"testing"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
)

func TestMatchContext_EmptyDecls(t *testing.T) {
	tree := &dst.File{Name: &dst.Ident{Name: "pkg"}, Decls: nil}
	ctx := &filter.MatchContext{
		ImportPath: "example.com/pkg",
		SourceFile: "/tmp/empty.go",
		AST:        tree,
	}

	if (&filter.FuncFilter{Func: "Missing"}).Match(ctx) {
		t.Fatal("FuncFilter.Match(empty decls) = true, want false")
	}
	if (&filter.StructFilter{Struct: "Missing"}).Match(ctx) {
		t.Fatal("StructFilter.Match(empty decls) = true, want false")
	}
}
