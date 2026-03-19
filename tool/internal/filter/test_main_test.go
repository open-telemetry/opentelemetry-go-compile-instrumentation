// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"testing"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
)

func TestTestMainFilter_Match(t *testing.T) {
	tests := []struct {
		name        string
		shouldMatch bool
		importPath  string
		want        bool
	}{
		// ShouldMatch: true → match test packages
		{
			name:        "test package matches when ShouldMatch=true",
			shouldMatch: true,
			importPath:  "github.com/foo/bar.test",
			want:        true,
		},
		{
			name:        "non-test package does not match when ShouldMatch=true",
			shouldMatch: true,
			importPath:  "github.com/foo/bar",
			want:        false,
		},

		// ShouldMatch: false → match non-test packages
		{
			name:        "non-test package matches when ShouldMatch=false",
			shouldMatch: false,
			importPath:  "github.com/foo/bar",
			want:        true,
		},
		{
			name:        "test package does not match when ShouldMatch=false",
			shouldMatch: false,
			importPath:  "github.com/foo/bar.test",
			want:        false,
		},

		// Edge cases
		{
			name:        "package with .test inside path but not suffix",
			shouldMatch: true,
			importPath:  "github.com/foo.test/bar",
			want:        false,
		},
		{
			name:        "empty import path treated as non-test",
			shouldMatch: false,
			importPath:  "",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &filter.MatchContext{
				ImportPath: tt.importPath,
				SourceFile: "source.go",
				AST:        &dst.File{Name: &dst.Ident{Name: "pkg"}},
			}
			f := &filter.TestMainFilter{ShouldMatch: tt.shouldMatch}
			if got := f.Match(ctx); got != tt.want {
				t.Errorf("TestMainFilter{ShouldMatch: %v}.Match({ImportPath: %q}) = %v, want %v",
					tt.shouldMatch, tt.importPath, got, tt.want)
			}
		})
	}
}
