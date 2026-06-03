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
			name:  "where one-of unsupported",
			where: &rule.WhereDef{OneOf: []rule.WhereDef{{Func: "Foo"}, {Func: "Bar"}}},
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

func TestBuild_AllOf(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{AllOf: []rule.FilterDef{
		{HasFunc: "Foo"},
		{HasStruct: "Bar"},
	}}}
	f, err := setup.Build(where)
	if err != nil {
		t.Fatalf("Build(%+v) error = %v, want nil", where, err)
	}
	allOf, ok := f.(setup.AllOf)
	if !ok {
		t.Fatalf("Build() returned %T, want setup.AllOf", f)
	}
	if len(allOf) != 2 {
		t.Fatalf("AllOf len = %d, want 2", len(allOf))
	}
	if _, isFunc := allOf[0].(*setup.FuncFilter); !isFunc {
		t.Errorf("AllOf[0] = %T, want *setup.FuncFilter", allOf[0])
	}
	if _, isStruct := allOf[1].(*setup.StructFilter); !isStruct {
		t.Errorf("AllOf[1] = %T, want *setup.StructFilter", allOf[1])
	}
}

func TestBuild_AllOf_Empty(t *testing.T) {
	// An explicit empty all-of: [] is present (non-nil slice) and compiles to an
	// empty AllOf that matches vacuously, rather than erroring with "no active
	// predicate".
	where := &rule.WhereDef{File: &rule.FilterDef{AllOf: []rule.FilterDef{}}}
	f, err := setup.Build(where)
	if err != nil {
		t.Fatalf("Build(empty AllOf) error = %v, want nil", err)
	}
	allOf, ok := f.(setup.AllOf)
	if !ok {
		t.Fatalf("Build(empty AllOf) = %T, want setup.AllOf", f)
	}
	if len(allOf) != 0 {
		t.Fatalf("AllOf len = %d, want 0", len(allOf))
	}
	if !allOf.Match(nil) {
		t.Error("empty AllOf.Match(nil) = false, want true (vacuous truth)")
	}
}

func TestBuild_AllOf_Nested(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{AllOf: []rule.FilterDef{
		{AllOf: []rule.FilterDef{{HasFunc: "Foo"}}},
	}}}
	f, err := setup.Build(where)
	if err != nil {
		t.Fatalf("Build(nested AllOf) error = %v, want nil", err)
	}
	outer, ok := f.(setup.AllOf)
	if !ok || len(outer) != 1 {
		t.Fatalf("Build(nested) = %T, want setup.AllOf of len 1", f)
	}
	if _, isNested := outer[0].(setup.AllOf); !isNested {
		t.Errorf("AllOf[0] = %T, want nested setup.AllOf", outer[0])
	}
}

func TestBuild_AllOf_InvalidChild(t *testing.T) {
	// An empty child FilterDef has no active predicate and must fail the build.
	where := &rule.WhereDef{File: &rule.FilterDef{AllOf: []rule.FilterDef{{}}}}
	if _, err := setup.Build(where); err == nil {
		t.Fatal("Build(AllOf with empty child) error = nil, want error")
	}
}

// stubFilter is a Filter whose Match result is fixed, used to test AllOf
// composition without parsing source. It records call count to assert
// short-circuiting.
type stubFilter struct {
	result bool
	calls  *int
}

func (s stubFilter) Match(*setup.MatchContext) bool {
	if s.calls != nil {
		*s.calls++
	}
	return s.result
}

func TestAllOf_Match(t *testing.T) {
	tests := []struct {
		name     string
		children setup.AllOf
		want     bool
	}{
		{"empty is vacuously true", setup.AllOf{}, true},
		{"all children match", setup.AllOf{stubFilter{result: true}, stubFilter{result: true}}, true},
		{"one child fails", setup.AllOf{stubFilter{result: true}, stubFilter{result: false}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.children.Match(nil); got != tt.want {
				t.Errorf("AllOf.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllOf_Match_ShortCircuits(t *testing.T) {
	calls := 0
	a := setup.AllOf{stubFilter{result: false, calls: &calls}, stubFilter{result: true, calls: &calls}}
	if a.Match(nil) {
		t.Fatal("AllOf.Match() = true, want false")
	}
	if calls != 1 {
		t.Errorf("evaluated %d children, want 1 (short-circuit on first non-match)", calls)
	}
}

func TestBuild_OneOf(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{OneOf: []rule.FilterDef{
		{HasFunc: "Foo"},
		{HasStruct: "Bar"},
	}}}
	f, err := setup.Build(where)
	if err != nil {
		t.Fatalf("Build(%+v) error = %v, want nil", where, err)
	}
	oneOf, ok := f.(setup.OneOf)
	if !ok {
		t.Fatalf("Build() returned %T, want setup.OneOf", f)
	}
	if len(oneOf) != 2 {
		t.Fatalf("OneOf len = %d, want 2", len(oneOf))
	}
	if _, isFunc := oneOf[0].(*setup.FuncFilter); !isFunc {
		t.Errorf("OneOf[0] = %T, want *setup.FuncFilter", oneOf[0])
	}
	if _, isStruct := oneOf[1].(*setup.StructFilter); !isStruct {
		t.Errorf("OneOf[1] = %T, want *setup.StructFilter", oneOf[1])
	}
}

func TestBuild_OneOf_Nested(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{OneOf: []rule.FilterDef{
		{OneOf: []rule.FilterDef{{HasFunc: "Foo"}}},
	}}}
	f, err := setup.Build(where)
	if err != nil {
		t.Fatalf("Build(nested OneOf) error = %v, want nil", err)
	}
	outer, ok := f.(setup.OneOf)
	if !ok || len(outer) != 1 {
		t.Fatalf("Build(nested) = %T, want setup.OneOf of len 1", f)
	}
	if _, isNested := outer[0].(setup.OneOf); !isNested {
		t.Errorf("OneOf[0] = %T, want nested setup.OneOf", outer[0])
	}
}

func TestBuild_OneOf_InvalidChild(t *testing.T) {
	// An empty child FilterDef has no active predicate and must fail the build.
	where := &rule.WhereDef{File: &rule.FilterDef{OneOf: []rule.FilterDef{{}}}}
	if _, err := setup.Build(where); err == nil {
		t.Fatal("Build(OneOf with empty child) error = nil, want error")
	}
}

func TestOneOf_Match(t *testing.T) {
	tests := []struct {
		name     string
		children setup.OneOf
		want     bool
	}{
		{"empty never matches", setup.OneOf{}, false},
		{"one child matches", setup.OneOf{stubFilter{result: false}, stubFilter{result: true}}, true},
		{"no children match", setup.OneOf{stubFilter{result: false}, stubFilter{result: false}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.children.Match(nil); got != tt.want {
				t.Errorf("OneOf.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOneOf_Match_ShortCircuits(t *testing.T) {
	calls := 0
	o := setup.OneOf{stubFilter{result: true, calls: &calls}, stubFilter{result: false, calls: &calls}}
	if !o.Match(nil) {
		t.Fatal("OneOf.Match() = false, want true")
	}
	if calls != 1 {
		t.Errorf("evaluated %d children, want 1 (short-circuit on first match)", calls)
	}
}

type filterExpected struct {
	Type   string `yaml:"type"`
	Func   string `yaml:"func"`
	Recv   string `yaml:"recv"`
	Struct string `yaml:"struct"`
	// Children describes the expected sub-filters for combinator types
	// (e.g. AllOf). It is nil for leaf filters.
	Children []filterExpected `yaml:"children"`
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
	case "AllOf":
		allOf, ok := got.(setup.AllOf)
		if !ok {
			t.Fatalf("Build(%q) = %T, want setup.AllOf", name, got)
		}
		if len(allOf) != len(want.Children) {
			t.Fatalf("Build(%q) AllOf len = %d, want %d", name, len(allOf), len(want.Children))
		}
		for i := range allOf {
			assertBuiltFilter(t, name, allOf[i], want.Children[i])
		}
	default:
		t.Fatalf("unexpected expected filter type %q", want.Type)
	}
}
