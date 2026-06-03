// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule_test

import (
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestIsGlobTarget(t *testing.T) {
	tests := []struct {
		target string
		want   bool
	}{
		{"example.com/svc", false},
		{"net/http", false},
		{"", false},
		{"example.com/svc/*", true},
		{"example.com/svc/**", true},
		{"example.com/*/handler", true},
		{"example.com/svc/[ab]c", true},
		{"example.com/svc/v?", true},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			if got := rule.IsGlobTarget(tt.target); got != tt.want {
				t.Errorf("IsGlobTarget(%q) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		// Exact targets are never glob-validated.
		{name: "exact path", target: "example.com/svc"},
		{name: "empty path", target: ""},

		// Valid glob patterns.
		{name: "single segment star", target: "example.com/svc/*"},
		{name: "double star whole segment", target: "example.com/svc/**"},
		{name: "double star leading", target: "**/internal"},
		{name: "star in middle segment", target: "example.com/*/handler"},
		{name: "char class", target: "example.com/svc/v[12]"},
		{name: "question mark", target: "example.com/svc/v?"},

		// Invalid: ** fused with other characters in a segment is ambiguous.
		{name: "double star fused suffix", target: "example.com/svc**", wantErr: true},
		{name: "double star fused prefix", target: "example.com/**svc", wantErr: true},
		{name: "triple star", target: "example.com/***", wantErr: true},

		// Invalid: malformed bracket expression (unclosed). Note: Go's
		// path.Match only rejects unclosed brackets, not semantically reversed
		// ranges like "[z-a]" (which silently never match), so we validate only
		// what stdlib can detect rather than hand-rolling a bracket parser.
		{name: "unclosed bracket", target: "example.com/svc/[ab", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rule.ValidateTarget(tt.target)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateTarget(%q) = nil, want error", tt.target)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateTarget(%q) = %v, want nil", tt.target, err)
			}
		})
	}
}

func TestMatchGlobTarget(t *testing.T) {
	tests := []struct {
		pattern    string
		importPath string
		want       bool
	}{
		// Single-segment wildcard: matches exactly one segment, no boundary crossing.
		{"example.com/svc/*", "example.com/svc/users", true},
		{"example.com/svc/*", "example.com/svc/orders", true},
		{"example.com/svc/*", "example.com/svc", false},
		{"example.com/svc/*", "example.com/svc/users/v2", false},
		{"example.com/*/handler", "example.com/foo/handler", true},
		{"example.com/*/handler", "example.com/foo/bar/handler", false},

		// Multi-segment wildcard: ** matches zero or more whole segments.
		{"example.com/svc/**", "example.com/svc", true},          // zero segments
		{"example.com/svc/**", "example.com/svc/users", true},    // one segment
		{"example.com/svc/**", "example.com/svc/users/v2", true}, // many segments
		{"example.com/svc/**", "example.com/other", false},

		// ** in the middle.
		{"example.com/**/handler", "example.com/handler", true},       // ** matches 0
		{"example.com/**/handler", "example.com/a/handler", true},     // ** matches 1
		{"example.com/**/handler", "example.com/a/b/c/handler", true}, // ** matches 3
		{"example.com/**/handler", "example.com/a/b/c/other", false},

		// Bare ** matches everything including the empty path.
		{"**", "anything/at/all", true},
		{"**", "", true},

		// Char class within a single segment.
		{"example.com/svc/v[12]", "example.com/svc/v1", true},
		{"example.com/svc/v[12]", "example.com/svc/v3", false},

		// Exact patterns still work through the matcher.
		{"example.com/svc", "example.com/svc", true},
		{"example.com/svc", "example.com/svc/users", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"_vs_"+tt.importPath, func(t *testing.T) {
			if got := rule.MatchGlobTarget(tt.pattern, tt.importPath); got != tt.want {
				t.Errorf("MatchGlobTarget(%q, %q) = %v, want %v",
					tt.pattern, tt.importPath, got, tt.want)
			}
		})
	}
}
