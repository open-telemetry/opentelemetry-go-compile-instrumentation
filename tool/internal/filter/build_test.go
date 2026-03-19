// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/filter"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestBuild_NilDef(t *testing.T) {
	f, err := filter.Build(nil)
	if err != nil {
		t.Fatalf("Build(nil) error = %v, want nil", err)
	}
	if f != nil {
		t.Errorf("Build(nil) = %T, want nil", f)
	}
}

func TestBuild_FuncFilter(t *testing.T) {
	def := &rule.FilterDef{Func: "ServeHTTP", Recv: "*serverHandler"}
	f, err := filter.Build(def)
	if err != nil {
		t.Fatalf("Build(%+v) error = %v, want nil", def, err)
	}
	ff, ok := f.(*filter.FuncFilter)
	if !ok {
		t.Fatalf("Build() returned %T, want *filter.FuncFilter", f)
	}
	if ff.Func != "ServeHTTP" {
		t.Errorf("FuncFilter.Func = %q, want %q", ff.Func, "ServeHTTP")
	}
	if ff.Recv != "*serverHandler" {
		t.Errorf("FuncFilter.Recv = %q, want %q", ff.Recv, "*serverHandler")
	}
}

func TestBuild_FuncFilter_NoRecv(t *testing.T) {
	def := &rule.FilterDef{Func: "MyFunc"}
	f, err := filter.Build(def)
	if err != nil {
		t.Fatalf("Build(%+v) error = %v, want nil", def, err)
	}
	ff, ok := f.(*filter.FuncFilter)
	if !ok {
		t.Fatalf("Build() returned %T, want *filter.FuncFilter", f)
	}
	if ff.Func != "MyFunc" {
		t.Errorf("FuncFilter.Func = %q, want %q", ff.Func, "MyFunc")
	}
	if ff.Recv != "" {
		t.Errorf("FuncFilter.Recv = %q, want empty", ff.Recv)
	}
}

func TestBuild_StructFilter(t *testing.T) {
	def := &rule.FilterDef{Struct: "MyStruct"}
	f, err := filter.Build(def)
	if err != nil {
		t.Fatalf("Build(%+v) error = %v, want nil", def, err)
	}
	sf, ok := f.(*filter.StructFilter)
	if !ok {
		t.Fatalf("Build() returned %T, want *filter.StructFilter", f)
	}
	if sf.Struct != "MyStruct" {
		t.Errorf("StructFilter.Struct = %q, want %q", sf.Struct, "MyStruct")
	}
}

func TestBuild_Error_EmptyFilterDef(t *testing.T) {
	f, err := filter.Build(&rule.FilterDef{})
	if err == nil {
		t.Fatal("Build(empty FilterDef) error = nil, want error")
	}
	if f != nil {
		t.Errorf("Build(empty FilterDef) = %T, want nil filter on error", f)
	}
}

func TestBuild_Error_RecvWithoutFunc(t *testing.T) {
	f, err := filter.Build(&rule.FilterDef{Recv: "*serverHandler"})
	if err == nil {
		t.Fatal("Build({Recv only}) error = nil, want error")
	}
	if f != nil {
		t.Errorf("Build({Recv only}) = %T, want nil filter on error", f)
	}
}

func TestBuild_Error_MultipleActiveLeaves(t *testing.T) {
	tests := []struct {
		name string
		def  *rule.FilterDef
	}{
		{
			name: "func and struct",
			def:  &rule.FilterDef{Func: "Foo", Struct: "Bar"},
		},
		{
			name: "func and import_path",
			def:  &rule.FilterDef{Func: "Foo", ImportPath: "example.com/**"},
		},
		{
			name: "struct and package_name",
			def:  &rule.FilterDef{Struct: "Bar", PackageName: "main"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := filter.Build(tt.def)
			if err == nil {
				t.Fatalf("Build(%+v) error = nil, want error for multiple active predicates", tt.def)
			}
		})
	}
}

func TestBuild_ImportPath(t *testing.T) {
	t.Run("exact path builds successfully", func(t *testing.T) {
		def := &rule.FilterDef{ImportPath: "github.com/foo/bar"}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(%+v) error = %v, want nil", def, err)
		}
		if _, ok := f.(*filter.ImportPathFilter); !ok {
			t.Errorf("Build(ImportPath) returned %T, want *filter.ImportPathFilter", f)
		}
	})
	t.Run("glob pattern builds successfully", func(t *testing.T) {
		def := &rule.FilterDef{ImportPath: "github.com/foo/**"}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(%+v) error = %v, want nil", def, err)
		}
		ipf, ok := f.(*filter.ImportPathFilter)
		if !ok {
			t.Fatalf("Build(ImportPath) returned %T, want *filter.ImportPathFilter", f)
		}
		if ipf.Pattern != "github.com/foo/**" {
			t.Errorf("ImportPathFilter.Pattern = %q, want %q", ipf.Pattern, "github.com/foo/**")
		}
	})
}

func TestBuild_ImportPath_InvalidPattern(t *testing.T) {
	// Build must reject malformed bracket expressions at construction time so
	// that bad YAML rules fail fast rather than silently producing non-matches.
	// path.Match returns ErrBadPattern for unclosed bracket expressions.
	def := &rule.FilterDef{ImportPath: "github.com/foo/["}
	_, err := filter.Build(def)
	if err == nil {
		t.Fatal("Build(ImportPath with unclosed bracket) error = nil, want error")
	}
}

func TestBuild_Error_UnsupportedCombinators(t *testing.T) {
	tests := []struct {
		name string
		def  *rule.FilterDef
	}{
		{
			name: "all-of",
			def:  &rule.FilterDef{AllOf: []rule.FilterDef{{Func: "Foo"}}},
		},
		{
			name: "one-of",
			def:  &rule.FilterDef{OneOf: []rule.FilterDef{{Func: "Foo"}}},
		},
		{
			name: "not",
			def:  &rule.FilterDef{Not: &rule.FilterDef{Func: "Foo"}},
		},
		{
			name: "directive",
			def:  &rule.FilterDef{Directive: "otelc:span"},
		},
		{
			name: "package_name",
			def:  &rule.FilterDef{PackageName: "main"},
		},
		{
			name: "test_main",
			def:  &rule.FilterDef{TestMain: boolPtr(true)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := filter.Build(tt.def)
			if err == nil {
				t.Fatalf("Build(%+v) error = nil, want error for not-yet-supported predicate", tt.def)
			}
		})
	}
}

// boolPtr returns a pointer to the given bool value. Used to construct
// *bool fields in FilterDef table entries without a named local variable.
func boolPtr(b bool) *bool { return &b }
