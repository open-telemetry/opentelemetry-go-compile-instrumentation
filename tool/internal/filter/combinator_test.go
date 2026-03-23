// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"testing"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
)

// alwaysMatch is a test Filter stub that always returns a fixed boolean result.
type alwaysMatch bool

func (a alwaysMatch) Match(_ *filter.MatchContext) bool { return bool(a) }

// callSpy records whether its Match method was called.
type callSpy struct {
	result bool
	called *bool
}

func (s callSpy) Match(_ *filter.MatchContext) bool {
	*s.called = true
	return s.result
}

// minimalCtx returns a MatchContext with a minimal AST for combinator tests.
// Combinator tests do not inspect AST contents; leaf filters used here are stubs.
func minimalCtx() *filter.MatchContext {
	return &filter.MatchContext{
		ImportPath: "example.com/pkg",
		SourceFile: "source.go",
		AST:        &dst.File{Name: &dst.Ident{Name: "pkg"}},
	}
}

func TestAllOf(t *testing.T) {
	tests := []struct {
		name    string
		filters []filter.Filter
		want    bool
	}{
		{
			name:    "empty AllOf is vacuously true",
			filters: nil,
			want:    true,
		},
		{
			name:    "single true child",
			filters: []filter.Filter{alwaysMatch(true)},
			want:    true,
		},
		{
			name:    "single false child",
			filters: []filter.Filter{alwaysMatch(false)},
			want:    false,
		},
		{
			name:    "all true",
			filters: []filter.Filter{alwaysMatch(true), alwaysMatch(true), alwaysMatch(true)},
			want:    true,
		},
		{
			name:    "first false short-circuits",
			filters: []filter.Filter{alwaysMatch(false), alwaysMatch(true), alwaysMatch(true)},
			want:    false,
		},
		{
			name:    "last child false",
			filters: []filter.Filter{alwaysMatch(true), alwaysMatch(true), alwaysMatch(false)},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filter.AllOf(tt.filters)
			if got := f.Match(minimalCtx()); got != tt.want {
				t.Errorf("AllOf.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllOf_ShortCircuit(t *testing.T) {
	called := false
	spy := callSpy{result: true, called: &called}
	f := filter.AllOf{alwaysMatch(false), spy}
	f.Match(minimalCtx())
	if called {
		t.Error("AllOf short-circuit failed: second filter called after first returned false")
	}
}

func TestAllOf_Nested(t *testing.T) {
	inner := filter.AllOf{alwaysMatch(true), alwaysMatch(true)}
	outer := filter.AllOf{inner, alwaysMatch(true)}
	if !outer.Match(minimalCtx()) {
		t.Error("AllOf{AllOf{true, true}, true}.Match() = false, want true")
	}
}

func TestOneOf(t *testing.T) {
	tests := []struct {
		name    string
		filters []filter.Filter
		want    bool
	}{
		{
			name:    "empty OneOf is false",
			filters: nil,
			want:    false,
		},
		{
			name:    "single true child",
			filters: []filter.Filter{alwaysMatch(true)},
			want:    true,
		},
		{
			name:    "single false child",
			filters: []filter.Filter{alwaysMatch(false)},
			want:    false,
		},
		{
			name:    "all false",
			filters: []filter.Filter{alwaysMatch(false), alwaysMatch(false), alwaysMatch(false)},
			want:    false,
		},
		{
			name:    "first true short-circuits",
			filters: []filter.Filter{alwaysMatch(true), alwaysMatch(false), alwaysMatch(false)},
			want:    true,
		},
		{
			name:    "last child true",
			filters: []filter.Filter{alwaysMatch(false), alwaysMatch(false), alwaysMatch(true)},
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filter.OneOf(tt.filters)
			if got := f.Match(minimalCtx()); got != tt.want {
				t.Errorf("OneOf.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOneOf_ShortCircuit(t *testing.T) {
	called := false
	spy := callSpy{result: false, called: &called}
	f := filter.OneOf{alwaysMatch(true), spy}
	f.Match(minimalCtx())
	if called {
		t.Error("OneOf short-circuit failed: second filter called after first returned true")
	}
}

func TestOneOf_Nested(t *testing.T) {
	inner := filter.OneOf{alwaysMatch(false), alwaysMatch(true)}
	outer := filter.AllOf{inner, alwaysMatch(true)}
	if !outer.Match(minimalCtx()) {
		t.Error("AllOf{OneOf{false, true}, true}.Match() = false, want true")
	}
}
