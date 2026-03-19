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

func TestBuild_AllOf(t *testing.T) {
	t.Run("single func child", func(t *testing.T) {
		def := &rule.FilterDef{AllOf: []rule.FilterDef{{Func: "Foo"}}}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(%+v) error = %v, want nil", def, err)
		}
		if _, ok := f.(filter.AllOf); !ok {
			t.Errorf("Build(AllOf) returned %T, want filter.AllOf", f)
		}
	})
	t.Run("multiple children", func(t *testing.T) {
		def := &rule.FilterDef{AllOf: []rule.FilterDef{{Func: "Foo"}, {Struct: "Bar"}}}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(%+v) error = %v, want nil", def, err)
		}
		af, ok := f.(filter.AllOf)
		if !ok {
			t.Fatalf("Build(AllOf) returned %T, want filter.AllOf", f)
		}
		if len(af) != 2 {
			t.Errorf("AllOf len = %d, want 2", len(af))
		}
	})
	t.Run("invalid child returns error", func(t *testing.T) {
		def := &rule.FilterDef{AllOf: []rule.FilterDef{{}}} // empty child is invalid
		_, err := filter.Build(def)
		if err == nil {
			t.Fatal("Build(AllOf with empty child) error = nil, want error")
		}
	})
	t.Run("nested allof", func(t *testing.T) {
		def := &rule.FilterDef{AllOf: []rule.FilterDef{{AllOf: []rule.FilterDef{{Func: "Foo"}}}}}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(nested AllOf) error = %v, want nil", err)
		}
		if _, ok := f.(filter.AllOf); !ok {
			t.Errorf("Build(nested AllOf) returned %T, want filter.AllOf", f)
		}
	})
}

func TestBuild_OneOf(t *testing.T) {
	t.Run("single func child", func(t *testing.T) {
		def := &rule.FilterDef{OneOf: []rule.FilterDef{{Func: "Foo"}}}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(%+v) error = %v, want nil", def, err)
		}
		if _, ok := f.(filter.OneOf); !ok {
			t.Errorf("Build(OneOf) returned %T, want filter.OneOf", f)
		}
	})
	t.Run("multiple children", func(t *testing.T) {
		def := &rule.FilterDef{OneOf: []rule.FilterDef{{Func: "Foo"}, {Struct: "Bar"}}}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(%+v) error = %v, want nil", def, err)
		}
		of, ok := f.(filter.OneOf)
		if !ok {
			t.Fatalf("Build(OneOf) returned %T, want filter.OneOf", f)
		}
		if len(of) != 2 {
			t.Errorf("OneOf len = %d, want 2", len(of))
		}
	})
	t.Run("invalid child returns error", func(t *testing.T) {
		def := &rule.FilterDef{OneOf: []rule.FilterDef{{}}}
		_, err := filter.Build(def)
		if err == nil {
			t.Fatal("Build(OneOf with empty child) error = nil, want error")
		}
	})
	t.Run("oneof inside allof", func(t *testing.T) {
		def := &rule.FilterDef{
			AllOf: []rule.FilterDef{
				{OneOf: []rule.FilterDef{{Func: "Foo"}, {Struct: "Bar"}}},
			},
		}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(AllOf{OneOf{...}}) error = %v, want nil", err)
		}
		if _, ok := f.(filter.AllOf); !ok {
			t.Errorf("Build(AllOf{OneOf{...}}) returned %T, want filter.AllOf", f)
		}
	})
}

func TestBuild_Not(t *testing.T) {
	t.Run("negates func filter", func(t *testing.T) {
		def := &rule.FilterDef{Not: &rule.FilterDef{Func: "Foo"}}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(%+v) error = %v, want nil", def, err)
		}
		if _, ok := f.(*filter.Not); !ok {
			t.Errorf("Build(Not) returned %T, want *filter.Not", f)
		}
	})
	t.Run("invalid inner returns error", func(t *testing.T) {
		def := &rule.FilterDef{Not: &rule.FilterDef{}} // empty inner is invalid
		_, err := filter.Build(def)
		if err == nil {
			t.Fatal("Build(Not with empty inner) error = nil, want error")
		}
	})
	t.Run("not wrapping allof", func(t *testing.T) {
		def := &rule.FilterDef{
			Not: &rule.FilterDef{AllOf: []rule.FilterDef{{Func: "Foo"}}},
		}
		f, err := filter.Build(def)
		if err != nil {
			t.Fatalf("Build(Not{AllOf{...}}) error = %v, want nil", err)
		}
		if _, ok := f.(*filter.Not); !ok {
			t.Errorf("Build(Not{AllOf{...}}) returned %T, want *filter.Not", f)
		}
	})
}

func TestBuild_Error_UnsupportedCombinators(t *testing.T) {
	tests := []struct {
		name string
		def  *rule.FilterDef
	}{
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
