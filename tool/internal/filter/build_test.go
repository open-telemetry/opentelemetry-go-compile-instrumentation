// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

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
	def := &rule.FilterDef{HasFunc: "ServeHTTP", HasRecv: "*serverHandler"}
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
	def := &rule.FilterDef{HasFunc: "MyFunc"}
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
	def := &rule.FilterDef{HasStruct: "MyStruct"}
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

func TestBuild_Error_HasRecvWithoutFunc(t *testing.T) {
	f, err := filter.Build(&rule.FilterDef{HasRecv: "*serverHandler"})
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
			name: "has_func and has_struct",
			def:  &rule.FilterDef{HasFunc: "Foo", HasStruct: "Bar"},
		},
		{
			name: "has_func and has_directive",
			def:  &rule.FilterDef{HasFunc: "Foo", HasDirective: "otelc:span"},
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
			def:  &rule.FilterDef{AllOf: []rule.FilterDef{{HasFunc: "Foo"}}},
		},
		{
			name: "one-of",
			def:  &rule.FilterDef{OneOf: []rule.FilterDef{{HasFunc: "Foo"}}},
		},
		{
			name: "not",
			def:  &rule.FilterDef{Not: &rule.FilterDef{HasFunc: "Foo"}},
		},
		{
			name: "has_directive",
			def:  &rule.FilterDef{HasDirective: "otelc:span"},
		},
		{
			name: "include_test",
			def:  &rule.FilterDef{IncludeTest: boolPtr(true)},
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

// filterExpected is the decoded form of a .expected companion file.
// It describes the expected type and fields of the Filter returned by Build.
type filterExpected struct {
	Type   string `yaml:"type"`
	Func   string `yaml:"func"`
	Recv   string `yaml:"recv"`
	Struct string `yaml:"struct"`
}

// TestBuild_YAMLRoundTrip auto-discovers .yml files under testdata/where/ and
// verifies that each FilterDef YAML round-trips correctly through filter.Build.
//
// Naming convention:
//   - ok_*.yml — Build must succeed; a companion .expected file describes the
//     expected Filter type and field values.
//   - err_*.yml — Build must return an error.
func TestBuild_YAMLRoundTrip(t *testing.T) {
	const dir = "testdata/where"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".yml") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", name, err)
			}
			var def rule.FilterDef
			if err := yaml.Unmarshal(content, &def); err != nil {
				t.Fatalf("yaml.Unmarshal(%q) error = %v", name, err)
			}

			f, buildErr := filter.Build(&def)

			if strings.HasPrefix(name, "err_") {
				if buildErr == nil {
					t.Fatalf("Build(%q) error = nil, want error", name)
				}
				return
			}

			// ok_* case: Build must succeed and match the .expected file.
			if buildErr != nil {
				t.Fatalf("Build(%q) error = %v, want nil", name, buildErr)
			}
			expectedFile := filepath.Join(dir, strings.TrimSuffix(name, ".yml")+".expected")
			expectedContent, err := os.ReadFile(expectedFile)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", expectedFile, err)
			}
			var want filterExpected
			if err := yaml.Unmarshal(expectedContent, &want); err != nil {
				t.Fatalf("yaml.Unmarshal(%q) error = %v", expectedFile, err)
			}

			switch want.Type {
			case "FuncFilter":
				ff, ok := f.(*filter.FuncFilter)
				if !ok {
					t.Fatalf("Build(%q) = %T, want *filter.FuncFilter", name, f)
				}
				if ff.Func != want.Func {
					t.Errorf("Build(%q) FuncFilter.Func = %q, want %q", name, ff.Func, want.Func)
				}
				if ff.Recv != want.Recv {
					t.Errorf("Build(%q) FuncFilter.Recv = %q, want %q", name, ff.Recv, want.Recv)
				}
			case "StructFilter":
				sf, ok := f.(*filter.StructFilter)
				if !ok {
					t.Fatalf("Build(%q) = %T, want *filter.StructFilter", name, f)
				}
				if sf.Struct != want.Struct {
					t.Errorf("Build(%q) StructFilter.Struct = %q, want %q", name, sf.Struct, want.Struct)
				}
			default:
				t.Fatalf("unexpected filter type in %q: %q", expectedFile, want.Type)
			}
		})
	}
}
