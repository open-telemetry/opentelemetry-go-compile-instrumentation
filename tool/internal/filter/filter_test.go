// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"testing"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
)

// TestMatchContext_EmptyDecls verifies that FuncFilter and StructFilter return
// false (not panic) when the source file AST has no declarations.
// This exercises the empty-Decls path that arises for files containing only
// a package clause and no other declarations.
func TestMatchContext_EmptyDecls(t *testing.T) {
	tree := &dst.File{Name: &dst.Ident{Name: "pkg"}, Decls: nil}
	ctx := &filter.MatchContext{
		ImportPath: "example.com/pkg",
		SourceFile: "/tmp/empty.go",
		AST:        tree,
	}

	t.Run("FuncFilter on empty decls returns false", func(t *testing.T) {
		f := &filter.FuncFilter{Func: "Missing"}
		got := f.Match(ctx)
		if got {
			t.Errorf("FuncFilter{Func: %q}.Match(emptyDecls) = true, want false", f.Func)
		}
	})

	t.Run("StructFilter on empty decls returns false", func(t *testing.T) {
		f := &filter.StructFilter{Struct: "Missing"}
		got := f.Match(ctx)
		if got {
			t.Errorf("StructFilter{Struct: %q}.Match(emptyDecls) = true, want false", f.Struct)
		}
	})
}
