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
			name: "import_path",
			def:  &rule.FilterDef{ImportPath: "example.com/**"},
		},
		{
			name: "package_name",
			def:  &rule.FilterDef{PackageName: "main"},
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

func TestBuild_TestMain(t *testing.T) {
	t.Run("true matches test packages", func(t *testing.T) {
		def := &rule.FilterDef{TestMain: boolPtr(true)}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(%+v) error = %v, want nil", def, err)
		}
		tmf, ok := f.(*filter.TestMainFilter)
		if !ok {
			t.Fatalf("Build(TestMain=true) returned %T, want *filter.TestMainFilter", f)
		}
		if !tmf.ShouldMatch {
			t.Error("TestMainFilter.ShouldMatch = false, want true")
		}
	})
	t.Run("false matches non-test packages", func(t *testing.T) {
		def := &rule.FilterDef{TestMain: boolPtr(false)}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(%+v) error = %v, want nil", def, err)
		}
		tmf, ok := f.(*filter.TestMainFilter)
		if !ok {
			t.Fatalf("Build(TestMain=false) returned %T, want *filter.TestMainFilter", f)
		}
		if tmf.ShouldMatch {
			t.Error("TestMainFilter.ShouldMatch = true, want false")
		}
	})
}

// boolPtr returns a pointer to the given bool value. Used to construct
// *bool fields in FilterDef table entries without a named local variable.
func boolPtr(b bool) *bool { return &b }
