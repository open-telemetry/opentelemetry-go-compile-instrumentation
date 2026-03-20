// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"testing"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// Exact matches
		{"github.com/foo/bar", "github.com/foo/bar", true},
		{"github.com/foo/bar", "github.com/foo/baz", false},
		{"github.com/foo/bar", "github.com/foo", false},
		{"github.com/foo/bar", "github.com/foo/bar/extra", false},

		// Single-segment wildcard (*)
		{"github.com/foo/*", "github.com/foo/bar", true},
		{"github.com/foo/*", "github.com/foo/baz", true},
		{"github.com/foo/*", "github.com/foo", false},
		{"github.com/foo/*", "github.com/foo/bar/baz", false},
		{"github.com/*/bar", "github.com/foo/bar", true},
		{"github.com/*/bar", "github.com/other/bar", true},
		{"github.com/*/bar", "github.com/foo/baz", false},

		// Multi-segment wildcard (**)
		{"github.com/foo/**", "github.com/foo", true},     // ** matches 0 segs
		{"github.com/foo/**", "github.com/foo/bar", true}, // ** matches 1 seg
		{"github.com/foo/**", "github.com/foo/bar/baz", true},
		{"github.com/foo/**", "github.com/other/bar", false},
		{"**", "anything/at/all", true},
		{"**", "", true},

		// ** in the middle
		{"github.com/**/bar", "github.com/bar", true},       // ** matches 0
		{"github.com/**/bar", "github.com/foo/bar", true},   // ** matches 1
		{"github.com/**/bar", "github.com/a/b/c/bar", true}, // ** matches 3
		{"github.com/**/bar", "github.com/foo/baz", false},

		// No wildcard, single segment
		{"example.com", "example.com", true},
		{"example.com", "other.com", false},

		// Edge cases
		{"*", "anything", true},
		{"*", "a/b", false}, // * does not cross segment boundaries
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.path, func(t *testing.T) {
			ctx := &filter.MatchContext{
				ImportPath: tt.path,
				SourceFile: "source.go",
				AST:        &dst.File{Name: &dst.Ident{Name: "pkg"}},
			}
			f := &filter.ImportPathFilter{Pattern: tt.pattern}
			got := f.Match(ctx)
			if got != tt.want {
				t.Errorf("ImportPathFilter{%q}.Match({ImportPath: %q}) = %v, want %v",
					tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

func TestImportPathFilter_Build(t *testing.T) {
	f := &filter.ImportPathFilter{Pattern: "github.com/foo/**"}
	ctx := &filter.MatchContext{
		ImportPath: "github.com/foo/bar",
		SourceFile: "source.go",
		AST:        &dst.File{Name: &dst.Ident{Name: "pkg"}},
	}
	if !f.Match(ctx) {
		t.Error("ImportPathFilter{github.com/foo/**}.Match(github.com/foo/bar) = false, want true")
	}
}

func TestContainsImportPath(t *testing.T) {
	tests := []struct {
		name string
		def  *rule.FilterDef
		want bool
	}{
		{
			name: "nil def returns false",
			def:  nil,
			want: false,
		},
		{
			name: "direct ImportPath returns true",
			def:  &rule.FilterDef{ImportPath: "github.com/foo/**"},
			want: true,
		},
		{
			name: "no ImportPath predicate returns false",
			def:  &rule.FilterDef{Func: "Foo"},
			want: false,
		},
		{
			name: "ImportPath nested in AllOf returns true",
			def: &rule.FilterDef{
				AllOf: []rule.FilterDef{{ImportPath: "github.com/foo/**"}},
			},
			want: true,
		},
		{
			name: "ImportPath nested in OneOf returns true",
			def: &rule.FilterDef{
				OneOf: []rule.FilterDef{{Func: "Bar"}, {ImportPath: "github.com/foo/**"}},
			},
			want: true,
		},
		{
			name: "ImportPath nested under Not returns true",
			def: &rule.FilterDef{
				Not: &rule.FilterDef{ImportPath: "github.com/foo/**"},
			},
			want: true,
		},
		{
			name: "deeply nested ImportPath returns true",
			def: &rule.FilterDef{
				AllOf: []rule.FilterDef{
					{OneOf: []rule.FilterDef{{Func: "Foo"}, {ImportPath: "example.com/**"}}},
				},
			},
			want: true,
		},
		{
			name: "def with only non-ImportPath predicates returns false",
			def:  &rule.FilterDef{Struct: "Bar"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filter.ContainsImportPath(tt.def)
			if got != tt.want {
				t.Errorf("ContainsImportPath(%+v) = %v, want %v", tt.def, got, tt.want)
			}
		})
	}
}
