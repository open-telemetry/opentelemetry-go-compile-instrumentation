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

func TestBuild_NilWhere(t *testing.T) {
	f, err := filter.Build(nil)
	if err != nil {
		t.Fatalf("Build(nil) error = %v, want nil", err)
	}
	if f != nil {
		t.Errorf("Build(nil) = %T, want nil", f)
	}
}

func TestBuild_FuncFilter(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{HasFunc: "ServeHTTP", Recv: "*serverHandler"}}
	f, err := filter.Build(where)
	if err != nil {
		t.Fatalf("Build(%+v) error = %v, want nil", where, err)
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

func TestBuild_StructFilter(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{HasStruct: "Server"}}
	f, err := filter.Build(where)
	if err != nil {
		t.Fatalf("Build(%+v) error = %v, want nil", where, err)
	}
	sf, ok := f.(*filter.StructFilter)
	if !ok {
		t.Fatalf("Build() returned %T, want *filter.StructFilter", f)
	}
	if sf.Struct != "Server" {
		t.Errorf("StructFilter.Struct = %q, want %q", sf.Struct, "Server")
	}
}

func TestBuild_ErrorCases(t *testing.T) {
	tests := []struct {
		name  string
		where *rule.WhereDef
	}{
		{
			name:  "empty where.file",
			where: &rule.WhereDef{File: &rule.FilterDef{}},
		},
		{
			name:  "recv without has_func",
			where: &rule.WhereDef{File: &rule.FilterDef{Recv: "*Server"}},
		},
		{
			name:  "multiple file predicates",
			where: &rule.WhereDef{File: &rule.FilterDef{HasFunc: "Foo", HasStruct: "Bar"}},
		},
		{
			name:  "where one-of unsupported",
			where: &rule.WhereDef{OneOf: []rule.WhereDef{{Func: "Foo"}, {Func: "Bar"}}},
		},
		{
			name:  "where.file one-of unsupported",
			where: &rule.WhereDef{File: &rule.FilterDef{OneOf: []rule.FilterDef{{HasFunc: "Foo"}, {HasFunc: "Bar"}}}},
		},
		{
			name:  "where selector composition unsupported",
			where: &rule.WhereDef{Func: "Foo"},
		},
		{
			name:  "where.file.has_directive unsupported",
			where: &rule.WhereDef{File: &rule.FilterDef{HasDirective: "otelc:span"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := filter.Build(tt.where); err == nil {
				t.Fatalf("Build(%+v) error = nil, want error", tt.where)
			}
		})
	}
}

type filterExpected struct {
	Type   string `yaml:"type"`
	Func   string `yaml:"func"`
	Recv   string `yaml:"recv"`
	Struct string `yaml:"struct"`
}

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
			runYAMLRoundTripCase(t, dir, name)
		})
	}
}

func runYAMLRoundTripCase(t *testing.T, dir, name string) {
	t.Helper()

	content, readErr := os.ReadFile(filepath.Join(dir, name))
	if readErr != nil {
		t.Fatalf("ReadFile(%q) error = %v", name, readErr)
	}

	var def rule.FilterDef
	if unmarshalErr := yaml.Unmarshal(content, &def); unmarshalErr != nil {
		t.Fatalf("yaml.Unmarshal(%q) error = %v", name, unmarshalErr)
	}

	got, buildErr := filter.Build(&rule.WhereDef{File: &def})
	if strings.HasPrefix(name, "err_") {
		if buildErr == nil {
			t.Fatalf("Build(%q) error = nil, want error", name)
		}
		return
	}
	if buildErr != nil {
		t.Fatalf("Build(%q) error = %v, want nil", name, buildErr)
	}

	expectedFile := filepath.Join(dir, strings.TrimSuffix(name, ".yml")+".expected")
	want := loadExpectedFilter(t, expectedFile)
	assertBuiltFilter(t, name, got, want)
}

func loadExpectedFilter(t *testing.T, path string) filterExpected {
	t.Helper()

	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, readErr)
	}

	var want filterExpected
	if unmarshalErr := yaml.Unmarshal(content, &want); unmarshalErr != nil {
		t.Fatalf("yaml.Unmarshal(%q) error = %v", path, unmarshalErr)
	}

	return want
}

func assertBuiltFilter(t *testing.T, name string, got filter.Filter, want filterExpected) {
	t.Helper()

	switch want.Type {
	case "FuncFilter":
		funcFilter, ok := got.(*filter.FuncFilter)
		if !ok {
			t.Fatalf("Build(%q) = %T, want *filter.FuncFilter", name, got)
		}
		if funcFilter.Func != want.Func || funcFilter.Recv != want.Recv {
			t.Fatalf("Build(%q) = %+v, want func=%q recv=%q", name, funcFilter, want.Func, want.Recv)
		}
	case "StructFilter":
		structFilter, ok := got.(*filter.StructFilter)
		if !ok {
			t.Fatalf("Build(%q) = %T, want *filter.StructFilter", name, got)
		}
		if structFilter.Struct != want.Struct {
			t.Fatalf("Build(%q) = %+v, want struct=%q", name, structFilter, want.Struct)
		}
	default:
		t.Fatalf("unexpected expected filter type %q", want.Type)
	}
}
