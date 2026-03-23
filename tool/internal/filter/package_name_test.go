// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"testing"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
)

func TestPackageNameFilter_Match(t *testing.T) {
	tests := []struct {
		name        string
		filterName  string
		packageName string
		want        bool
	}{
		{
			name:        "exact match",
			filterName:  "main",
			packageName: "main",
			want:        true,
		},
		{
			name:        "no match",
			filterName:  "main",
			packageName: "http",
			want:        false,
		},
		{
			name:        "test package suffix ignored — name is literal",
			filterName:  "foo_test",
			packageName: "foo_test",
			want:        true,
		},
		{
			name:        "base package name does not match _test variant",
			filterName:  "foo",
			packageName: "foo_test",
			want:        false,
		},
		{
			name:        "empty filter never matches non-empty package",
			filterName:  "",
			packageName: "main",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &filter.MatchContext{
				ImportPath: "example.com/pkg",
				SourceFile: "source.go",
				AST:        &dst.File{Name: &dst.Ident{Name: tt.packageName}},
			}
			f := &filter.PackageNameFilter{Name: tt.filterName}
			if got := f.Match(ctx); got != tt.want {
				t.Errorf("PackageNameFilter{%q}.Match(pkg=%q) = %v, want %v",
					tt.filterName, tt.packageName, got, tt.want)
			}
		})
	}
}
