// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"gopkg.in/yaml.v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
)

// --- Filter interface and context ---

func TestMatchContext_EmptyDecls(t *testing.T) {
	tree := &dst.File{Name: &dst.Ident{Name: "pkg"}, Decls: nil}
	ctx := &setup.MatchContext{
		ImportPath: "example.com/pkg",
		SourceFile: "/tmp/empty.go",
		AST:        tree,
	}

	if (&setup.FuncFilter{Func: "Missing"}).Match(ctx) {
		t.Fatal("FuncFilter.Match(empty decls) = true, want false")
	}
	if (&setup.StructFilter{Struct: "Missing"}).Match(ctx) {
		t.Fatal("StructFilter.Match(empty decls) = true, want false")
	}
}

func TestIsTestFilter_Match(t *testing.T) {
	tree := &dst.File{Name: &dst.Ident{Name: "pkg"}, Decls: nil}

	tests := []struct {
		name        string
		shouldMatch bool
		importPath  string
		want        bool
	}{
		// ShouldMatch: true → match only test packages
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
		// ShouldMatch: false → match only non-test packages
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
			name:        "dottest inside path but not suffix",
			shouldMatch: true,
			importPath:  "github.com/foo.test/bar",
			want:        false,
		},
		{
			name:        "empty import path treated as non-test when ShouldMatch=false",
			shouldMatch: false,
			importPath:  "",
			want:        true,
		},
		{
			name:        "empty import path does not match when ShouldMatch=true",
			shouldMatch: true,
			importPath:  "",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &setup.MatchContext{
				ImportPath: tt.importPath,
				SourceFile: "/tmp/source.go",
				AST:        tree,
			}
			f := &setup.IsTestFilter{ShouldMatch: tt.shouldMatch}
			if got := f.Match(ctx); got != tt.want {
				t.Fatalf("IsTestFilter{ShouldMatch:%v}.Match({ImportPath:%q}) = %v, want %v",
					tt.shouldMatch, tt.importPath, got, tt.want)
			}
		})
	}
}

// --- Leaf filters ---

func parseSource(t *testing.T, src string) *setup.MatchContext {
	t.Helper()
	parser := ast.NewAstParser()
	tree, err := parser.ParseSource(src)
	if err != nil {
		t.Fatalf("parseSource: %v", err)
	}
	return &setup.MatchContext{
		ImportPath: "example.com/pkg",
		SourceFile: "/tmp/source.go",
		AST:        tree,
	}
}

func TestFuncFilter_Match(t *testing.T) {
	ctx := parseSource(t, `package main

func Foo() {}
type MyType struct{}
func (m *MyType) Method() {}
`)

	tests := []struct {
		name string
		f    *setup.FuncFilter
		want bool
	}{
		{name: "free function", f: &setup.FuncFilter{Func: "Foo"}, want: true},
		{name: "method with recv", f: &setup.FuncFilter{Func: "Method", Recv: "*MyType"}, want: true},
		{name: "wrong recv", f: &setup.FuncFilter{Func: "Method", Recv: "*Other"}, want: false},
		{name: "method without recv", f: &setup.FuncFilter{Func: "Method"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.Match(ctx); got != tt.want {
				t.Fatalf("FuncFilter.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStructFilter_Match(t *testing.T) {
	ctx := parseSource(t, `package main

type Server struct{}
func NotAStruct() {}
`)

	if !(&setup.StructFilter{Struct: "Server"}).Match(ctx) {
		t.Fatal("StructFilter.Match(Server) = false, want true")
	}
	if (&setup.StructFilter{Struct: "NotAStruct"}).Match(ctx) {
		t.Fatal("StructFilter.Match(NotAStruct) = true, want false")
	}
}

// --- Build ---

func TestBuild_NilWhere(t *testing.T) {
	f, err := setup.Build(nil)
	if err != nil {
		t.Fatalf("Build(nil) error = %v, want nil", err)
	}
	if f != nil {
		t.Errorf("Build(nil) = %T, want nil", f)
	}
}

func TestBuild_FuncFilter(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{HasFunc: "ServeHTTP", HasRecv: "*serverHandler"}}
	f, err := setup.Build(where)
	if err != nil {
		t.Fatalf("Build(%+v) error = %v, want nil", where, err)
	}
	ff, ok := f.(*setup.FuncFilter)
	if !ok {
		t.Fatalf("Build() returned %T, want *setup.FuncFilter", f)
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
	f, err := setup.Build(where)
	if err != nil {
		t.Fatalf("Build(%+v) error = %v, want nil", where, err)
	}
	sf, ok := f.(*setup.StructFilter)
	if !ok {
		t.Fatalf("Build() returned %T, want *setup.StructFilter", f)
	}
	if sf.Struct != "Server" {
		t.Errorf("StructFilter.Struct = %q, want %q", sf.Struct, "Server")
	}
}

func boolPtr(b bool) *bool { return &b }

func TestBuild_IsTestFilter(t *testing.T) {
	t.Run("true matches test packages", func(t *testing.T) {
		where := &rule.WhereDef{File: &rule.FilterDef{IsTest: boolPtr(true)}}
		f, err := setup.Build(where)
		if err != nil {
			t.Fatalf("Build(IsTest=true) error = %v, want nil", err)
		}
		itf, ok := f.(*setup.IsTestFilter)
		if !ok {
			t.Fatalf("Build(IsTest=true) returned %T, want *setup.IsTestFilter", f)
		}
		if !itf.ShouldMatch {
			t.Errorf("IsTestFilter.ShouldMatch = false, want true")
		}
	})

	t.Run("false matches non-test packages", func(t *testing.T) {
		where := &rule.WhereDef{File: &rule.FilterDef{IsTest: boolPtr(false)}}
		f, err := setup.Build(where)
		if err != nil {
			t.Fatalf("Build(IsTest=false) error = %v, want nil", err)
		}
		itf, ok := f.(*setup.IsTestFilter)
		if !ok {
			t.Fatalf("Build(IsTest=false) returned %T, want *setup.IsTestFilter", f)
		}
		if itf.ShouldMatch {
			t.Errorf("IsTestFilter.ShouldMatch = true, want false")
		}
	})

	t.Run("nil is_test leaves filter nil", func(t *testing.T) {
		// A nil IsTest must not produce an IsTestFilter — it means "unset".
		// We exercise this indirectly: a FilterDef with only IsTest==nil has no
		// active predicate and Build must return an error, not silently
		// construct a filter that treats nil as false.
		where := &rule.WhereDef{File: &rule.FilterDef{}}
		_, err := setup.Build(where)
		if err == nil {
			t.Fatal("Build(empty FilterDef) error = nil, want error: nil IsTest must not count as active predicate")
		}
	})
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
			name:  "has_recv without has_func",
			where: &rule.WhereDef{File: &rule.FilterDef{HasRecv: "*Server"}},
		},
		{
			name:  "multiple file predicates",
			where: &rule.WhereDef{File: &rule.FilterDef{HasFunc: "Foo", HasStruct: "Bar"}},
		},
		{
			name:  "is_test combined with another predicate",
			where: &rule.WhereDef{File: &rule.FilterDef{HasFunc: "Foo", IsTest: boolPtr(true)}},
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
			if _, err := setup.Build(tt.where); err == nil {
				t.Fatalf("Build(%+v) error = nil, want error", tt.where)
			}
		})
	}
}

type filterExpected struct {
	Type        string `yaml:"type"`
	Func        string `yaml:"func"`
	Recv        string `yaml:"recv"`
	Struct      string `yaml:"struct"`
	ShouldMatch *bool  `yaml:"should_match"`
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

	got, buildErr := setup.Build(&rule.WhereDef{File: &def})
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

func assertBuiltFilter(t *testing.T, name string, got setup.Filter, want filterExpected) {
	t.Helper()

	switch want.Type {
	case "FuncFilter":
		funcFilter, ok := got.(*setup.FuncFilter)
		if !ok {
			t.Fatalf("Build(%q) = %T, want *setup.FuncFilter", name, got)
		}
		if funcFilter.Func != want.Func || funcFilter.Recv != want.Recv {
			t.Fatalf("Build(%q) = %+v, want func=%q recv=%q", name, funcFilter, want.Func, want.Recv)
		}
	case "StructFilter":
		structFilter, ok := got.(*setup.StructFilter)
		if !ok {
			t.Fatalf("Build(%q) = %T, want *setup.StructFilter", name, got)
		}
		if structFilter.Struct != want.Struct {
			t.Fatalf("Build(%q) = %+v, want struct=%q", name, structFilter, want.Struct)
		}
	case "IsTestFilter":
		itf, ok := got.(*setup.IsTestFilter)
		if !ok {
			t.Fatalf("Build(%q) = %T, want *setup.IsTestFilter", name, got)
		}
		if want.ShouldMatch == nil {
			t.Fatalf("expected file %q has type IsTestFilter but no should_match field", name)
		}
		if itf.ShouldMatch != *want.ShouldMatch {
			t.Fatalf("Build(%q) IsTestFilter.ShouldMatch = %v, want %v", name, itf.ShouldMatch, *want.ShouldMatch)
		}
	default:
		t.Fatalf("unexpected expected filter type %q", want.Type)
	}
}
