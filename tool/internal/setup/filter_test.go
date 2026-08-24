// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"gopkg.in/yaml.v3"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// --- filter interface and context ---

func TestMatchContext_EmptyDecls(t *testing.T) {
	tree := &dst.File{Name: &dst.Ident{Name: "pkg"}, Decls: nil}
	ctx := &matchContext{
		SourceFile: "/tmp/empty.go",
		AST:        tree,
	}

	if (&funcFilter{Func: "Missing"}).Match(ctx) {
		t.Fatal("funcFilter.Match(empty decls) = true, want false")
	}
	if (&structFilter{Struct: "Missing"}).Match(ctx) {
		t.Fatal("structFilter.Match(empty decls) = true, want false")
	}
}

func TestIsTestFilter_Match(t *testing.T) {
	tree := &dst.File{Name: &dst.Ident{Name: "pkg"}, Decls: nil}

	tests := []struct {
		name        string
		shouldMatch bool
		isTest      bool
		want        bool
	}{
		// ShouldMatch: true → match only test builds.
		{name: "test build matches when ShouldMatch=true", shouldMatch: true, isTest: true, want: true},
		{name: "non-test build does not match when ShouldMatch=true", shouldMatch: true, isTest: false, want: false},
		// ShouldMatch: false → match only non-test builds.
		{name: "non-test build matches when ShouldMatch=false", shouldMatch: false, isTest: false, want: true},
		{name: "test build does not match when ShouldMatch=false", shouldMatch: false, isTest: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &matchContext{
				IsTest:     tt.isTest,
				SourceFile: "/tmp/source.go",
				AST:        tree,
			}
			f := &isTestFilter{ShouldMatch: tt.shouldMatch}
			if got := f.Match(ctx); got != tt.want {
				t.Fatalf("isTestFilter{ShouldMatch:%v}.Match({IsTest:%v}) = %v, want %v",
					tt.shouldMatch, tt.isTest, got, tt.want)
			}
		})
	}
}

// --- Leaf filters ---

func parseSource(t *testing.T, src string) *matchContext {
	t.Helper()
	parser := ast.NewAstParser()
	tree, err := parser.ParseSource(src)
	if err != nil {
		t.Fatalf("parseSource: %v", err)
	}
	return &matchContext{
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
		f    *funcFilter
		want bool
	}{
		{name: "free function", f: &funcFilter{Func: "Foo"}, want: true},
		{name: "method with recv", f: &funcFilter{Func: "Method", Recv: "*MyType"}, want: true},
		{name: "wrong recv", f: &funcFilter{Func: "Method", Recv: "*Other"}, want: false},
		{name: "method without recv", f: &funcFilter{Func: "Method"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.Match(ctx); got != tt.want {
				t.Fatalf("funcFilter.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStructFilter_Match(t *testing.T) {
	ctx := parseSource(t, `package main

type Server struct{}
type Reader interface{ Read() error }
func NotAStruct() {}
`)

	if !(&structFilter{Struct: "Server"}).Match(ctx) {
		t.Fatal("structFilter.Match(Server) = false, want true")
	}
	if (&structFilter{Struct: "Reader"}).Match(ctx) {
		t.Fatal("structFilter.Match(Reader) = true, want false")
	}
	// Interfaces no longer match, so `Not: has_struct` now *includes* files
	// declaring an interface of that name, where it previously excluded them.
	if !(&not{Inner: &structFilter{Struct: "Reader"}}).Match(ctx) {
		t.Fatal("not(structFilter(Reader)).Match = false, want true")
	}
	if (&structFilter{Struct: "NotAStruct"}).Match(ctx) {
		t.Fatal("structFilter.Match(NotAStruct) = true, want false")
	}
}

func TestPackageNameFilter_Match(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		filterName string
		want       bool
	}{
		{
			name:       "declared name matches",
			src:        "package main\n",
			filterName: "main",
			want:       true,
		},
		{
			name:       "declared name does not match",
			src:        "package main\n",
			filterName: "other",
			want:       false,
		},
		{
			name:       "external test package name matches",
			src:        "package main_test\n",
			filterName: "main_test",
			want:       true,
		},
		{
			name:       "external test package does not match internal name",
			src:        "package main_test\n",
			filterName: "main",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := parseSource(t, tt.src)
			f := &packageNameFilter{Name: tt.filterName}
			if got := f.Match(ctx); got != tt.want {
				t.Fatalf("packageNameFilter{Name:%q}.Match(package %q) = %v, want %v",
					tt.filterName, ctx.AST.Name.Name, got, tt.want)
			}
		})
	}
}

func TestDirectiveFilter_Match(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		directive string
		want      bool
	}{
		{
			name:      "directive present",
			src:       "//go:build linux\npackage main\n\nfunc foo() {}\n",
			directive: "go:build",
			want:      true,
		},
		{
			name:      "directive missing",
			src:       "package main\n",
			directive: "go:build",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := parseSource(t, tt.src)
			f := &directiveFilter{Directive: tt.directive}
			if got := f.Match(ctx); got != tt.want {
				t.Fatalf("directiveFilter{Directive:%q}.Match() = %v, want %v",
					tt.directive, got, tt.want)
			}
		})
	}
}

// --- build ---

func TestBuild_NilWhere(t *testing.T) {
	f, err := build(nil)
	if err != nil {
		t.Fatalf("build(nil) error = %v, want nil", err)
	}
	if f != nil {
		t.Errorf("build(nil) = %T, want nil", f)
	}
}

func TestBuild_FuncFilter(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{HasFunc: "ServeHTTP", HasRecv: "*serverHandler"}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(%+v) error = %v, want nil", where, err)
	}
	ff, ok := f.(*funcFilter)
	if !ok {
		t.Fatalf("build() returned %T, want *funcFilter", f)
	}
	if ff.Func != "ServeHTTP" {
		t.Errorf("funcFilter.Func = %q, want %q", ff.Func, "ServeHTTP")
	}
	if ff.Recv != "*serverHandler" {
		t.Errorf("funcFilter.Recv = %q, want %q", ff.Recv, "*serverHandler")
	}
}

func TestBuild_StructFilter(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{HasStruct: "Server"}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(%+v) error = %v, want nil", where, err)
	}
	sf, ok := f.(*structFilter)
	if !ok {
		t.Fatalf("build() returned %T, want *structFilter", f)
	}
	if sf.Struct != "Server" {
		t.Errorf("structFilter.Struct = %q, want %q", sf.Struct, "Server")
	}
}

func boolPtr(b bool) *bool { return &b }

func TestBuild_IsTestFilter(t *testing.T) {
	t.Run("true matches test packages", func(t *testing.T) {
		where := &rule.WhereDef{File: &rule.FilterDef{IsTest: boolPtr(true)}}
		f, err := build(where)
		if err != nil {
			t.Fatalf("build(IsTest=true) error = %v, want nil", err)
		}
		itf, ok := f.(*isTestFilter)
		if !ok {
			t.Fatalf("build(IsTest=true) returned %T, want *isTestFilter", f)
		}
		if !itf.ShouldMatch {
			t.Errorf("isTestFilter.ShouldMatch = false, want true")
		}
	})

	t.Run("false matches non-test packages", func(t *testing.T) {
		where := &rule.WhereDef{File: &rule.FilterDef{IsTest: boolPtr(false)}}
		f, err := build(where)
		if err != nil {
			t.Fatalf("build(IsTest=false) error = %v, want nil", err)
		}
		itf, ok := f.(*isTestFilter)
		if !ok {
			t.Fatalf("build(IsTest=false) returned %T, want *isTestFilter", f)
		}
		if itf.ShouldMatch {
			t.Errorf("isTestFilter.ShouldMatch = true, want false")
		}
	})

	t.Run("nil is_test leaves filter nil", func(t *testing.T) {
		// A nil IsTest must not produce an isTestFilter — it means "unset".
		// We exercise this indirectly: a FilterDef with only IsTest==nil has no
		// active predicate and build must return an error, not silently
		// construct a filter that treats nil as false.
		where := &rule.WhereDef{File: &rule.FilterDef{}}
		_, err := build(where)
		if err == nil {
			t.Fatal("build(empty FilterDef) error = nil, want error: nil IsTest must not count as active predicate")
		}
	})
}

func TestBuild_PackageNameFilter(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{HasPackage: "main"}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(HasPackage=%q) error = %v, want nil", "main", err)
	}
	pnf, ok := f.(*packageNameFilter)
	if !ok {
		t.Fatalf("build() returned %T, want *packageNameFilter", f)
	}
	if pnf.Name != "main" {
		t.Errorf("packageNameFilter.Name = %q, want %q", pnf.Name, "main")
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
			name:  "is_test combined with another predicate",
			where: &rule.WhereDef{File: &rule.FilterDef{HasFunc: "Foo", IsTest: boolPtr(true)}},
		},
		{
			// A combinator owns the node: is_test as a sibling must be rejected,
			// not silently ignored (regression guard for hasLeafPredicate).
			name: "is_test sibling of all-of",
			where: &rule.WhereDef{File: &rule.FilterDef{
				AllOf:  []rule.FilterDef{{HasFunc: "Foo"}},
				IsTest: boolPtr(true),
			}},
		},
		{
			name: "is_test sibling of one-of",
			where: &rule.WhereDef{File: &rule.FilterDef{
				OneOf:  []rule.FilterDef{{HasFunc: "Foo"}},
				IsTest: boolPtr(true),
			}},
		},
		{
			name: "is_test sibling of not",
			where: &rule.WhereDef{File: &rule.FilterDef{
				Not:    &rule.FilterDef{HasFunc: "Foo"},
				IsTest: boolPtr(false),
			}},
		},
		{
			name:  "has_package combined with another predicate",
			where: &rule.WhereDef{File: &rule.FilterDef{HasPackage: "main", HasFunc: "Foo"}},
		},
		{
			// A combinator owns the node: has_package as a sibling must be
			// rejected, not silently ignored (regression guard for hasLeafPredicate).
			name: "has_package sibling of all-of",
			where: &rule.WhereDef{File: &rule.FilterDef{
				AllOf:      []rule.FilterDef{{HasFunc: "Foo"}},
				HasPackage: "main",
			}},
		},
		{
			name: "has_package sibling of one-of",
			where: &rule.WhereDef{File: &rule.FilterDef{
				OneOf:      []rule.FilterDef{{HasFunc: "Foo"}},
				HasPackage: "main",
			}},
		},
		{
			name: "has_package sibling of not",
			where: &rule.WhereDef{File: &rule.FilterDef{
				Not:        &rule.FilterDef{HasFunc: "Foo"},
				HasPackage: "main",
			}},
		},
		{
			// Explicit regression for the primary use case of has_package: combining
			// it with is_test as siblings (without all-of) must be rejected.
			name:  "has_package combined with is_test",
			where: &rule.WhereDef{File: &rule.FilterDef{HasPackage: "foo_test", IsTest: boolPtr(true)}},
		},
		{
			// Whitespace-only has_package must not count as an active predicate.
			name:  "has_package whitespace only",
			where: &rule.WhereDef{File: &rule.FilterDef{HasPackage: "   "}},
		},
		{
			name:  "where one-of unsupported",
			where: &rule.WhereDef{OneOf: []rule.WhereDef{{Func: "Foo"}, {Func: "Bar"}}},
		},
		{
			name:  "where selector composition unsupported",
			where: &rule.WhereDef{Func: "Foo"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := build(tt.where); err == nil {
				t.Fatalf("build(%+v) error = nil, want error", tt.where)
			}
		})
	}
}

func TestBuild_AllOf(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{AllOf: []rule.FilterDef{
		{HasFunc: "Foo"},
		{HasStruct: "Bar"},
	}}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(%+v) error = %v, want nil", where, err)
	}
	gotAllOf, ok := f.(allOf)
	if !ok {
		t.Fatalf("build() returned %T, want allOf", f)
	}
	if len(gotAllOf) != 2 {
		t.Fatalf("allOf len = %d, want 2", len(gotAllOf))
	}
	if _, isFunc := gotAllOf[0].(*funcFilter); !isFunc {
		t.Errorf("allOf[0] = %T, want *funcFilter", gotAllOf[0])
	}
	if _, isStruct := gotAllOf[1].(*structFilter); !isStruct {
		t.Errorf("allOf[1] = %T, want *structFilter", gotAllOf[1])
	}
}

func TestBuild_AllOf_Empty(t *testing.T) {
	// An explicit empty all-of: [] is present (non-nil slice) and compiles to an
	// empty allOf that matches vacuously, rather than erroring with "no active
	// predicate".
	where := &rule.WhereDef{File: &rule.FilterDef{AllOf: []rule.FilterDef{}}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(empty allOf) error = %v, want nil", err)
	}
	gotAllOf, ok := f.(allOf)
	if !ok {
		t.Fatalf("build(empty allOf) = %T, want allOf", f)
	}
	if len(gotAllOf) != 0 {
		t.Fatalf("allOf len = %d, want 0", len(gotAllOf))
	}
	if !gotAllOf.Match(nil) {
		t.Error("empty allOf.Match(nil) = false, want true (vacuous truth)")
	}
}

func TestBuild_AllOf_Nested(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{AllOf: []rule.FilterDef{
		{AllOf: []rule.FilterDef{{HasFunc: "Foo"}}},
	}}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(nested allOf) error = %v, want nil", err)
	}
	outer, ok := f.(allOf)
	if !ok || len(outer) != 1 {
		t.Fatalf("build(nested) = %T, want allOf of len 1", f)
	}
	if _, isNested := outer[0].(allOf); !isNested {
		t.Errorf("allOf[0] = %T, want nested allOf", outer[0])
	}
}

func TestBuild_AllOf_InvalidChild(t *testing.T) {
	// An empty child FilterDef has no active predicate and must fail the build.
	where := &rule.WhereDef{File: &rule.FilterDef{AllOf: []rule.FilterDef{{}}}}
	if _, err := build(where); err == nil {
		t.Fatal("build(allOf with empty child) error = nil, want error")
	}
}

// stubFilter is a filter whose Match result is fixed, used to test allOf
// composition without parsing source. It records call count to assert
// short-circuiting.
type stubFilter struct {
	result bool
	calls  *int
}

func (s stubFilter) Match(*matchContext) bool {
	if s.calls != nil {
		*s.calls++
	}
	return s.result
}

func TestAllOf_Match(t *testing.T) {
	tests := []struct {
		name     string
		children allOf
		want     bool
	}{
		{"empty is vacuously true", allOf{}, true},
		{"all children match", allOf{stubFilter{result: true}, stubFilter{result: true}}, true},
		{"one child fails", allOf{stubFilter{result: true}, stubFilter{result: false}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.children.Match(nil); got != tt.want {
				t.Errorf("allOf.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllOf_Match_ShortCircuits(t *testing.T) {
	calls := 0
	a := allOf{stubFilter{result: false, calls: &calls}, stubFilter{result: true, calls: &calls}}
	if a.Match(nil) {
		t.Fatal("allOf.Match() = true, want false")
	}
	if calls != 1 {
		t.Errorf("evaluated %d children, want 1 (short-circuit on first non-match)", calls)
	}
}

func TestBuild_OneOf_Empty(t *testing.T) {
	// An explicit empty one-of: [] is present (non-nil slice) and compiles to an
	// empty oneOf that matches nothing (vacuous false), rather than erroring with
	// "no active predicate".
	where := &rule.WhereDef{File: &rule.FilterDef{OneOf: []rule.FilterDef{}}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(empty oneOf) error = %v, want nil", err)
	}
	gotOneOf, ok := f.(oneOf)
	if !ok {
		t.Fatalf("build(empty oneOf) = %T, want oneOf", f)
	}
	if len(gotOneOf) != 0 {
		t.Fatalf("oneOf len = %d, want 0", len(gotOneOf))
	}
	if gotOneOf.Match(nil) {
		t.Error("empty oneOf.Match(nil) = true, want false (no member matches)")
	}
}

func TestBuild_OneOf(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{OneOf: []rule.FilterDef{
		{HasFunc: "Foo"},
		{HasStruct: "Bar"},
	}}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(%+v) error = %v, want nil", where, err)
	}
	gotOneOf, ok := f.(oneOf)
	if !ok {
		t.Fatalf("build() returned %T, want oneOf", f)
	}
	if len(gotOneOf) != 2 {
		t.Fatalf("oneOf len = %d, want 2", len(gotOneOf))
	}
	if _, isFunc := gotOneOf[0].(*funcFilter); !isFunc {
		t.Errorf("oneOf[0] = %T, want *funcFilter", gotOneOf[0])
	}
	if _, isStruct := gotOneOf[1].(*structFilter); !isStruct {
		t.Errorf("oneOf[1] = %T, want *structFilter", gotOneOf[1])
	}
}

func TestBuild_OneOf_Nested(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{OneOf: []rule.FilterDef{
		{OneOf: []rule.FilterDef{{HasFunc: "Foo"}}},
	}}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(nested oneOf) error = %v, want nil", err)
	}
	outer, ok := f.(oneOf)
	if !ok || len(outer) != 1 {
		t.Fatalf("build(nested) = %T, want oneOf of len 1", f)
	}
	if _, isNested := outer[0].(oneOf); !isNested {
		t.Errorf("oneOf[0] = %T, want nested oneOf", outer[0])
	}
}

func TestBuild_OneOf_InvalidChild(t *testing.T) {
	// An empty child FilterDef has no active predicate and must fail the build.
	where := &rule.WhereDef{File: &rule.FilterDef{OneOf: []rule.FilterDef{{}}}}
	if _, err := build(where); err == nil {
		t.Fatal("build(oneOf with empty child) error = nil, want error")
	}
}

func TestOneOf_Match(t *testing.T) {
	tests := []struct {
		name     string
		children oneOf
		want     bool
	}{
		{"empty never matches", oneOf{}, false},
		{"one child matches", oneOf{stubFilter{result: false}, stubFilter{result: true}}, true},
		{"no children match", oneOf{stubFilter{result: false}, stubFilter{result: false}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.children.Match(nil); got != tt.want {
				t.Errorf("oneOf.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOneOf_Match_ShortCircuits(t *testing.T) {
	calls := 0
	o := oneOf{stubFilter{result: true, calls: &calls}, stubFilter{result: false, calls: &calls}}
	if !o.Match(nil) {
		t.Fatal("oneOf.Match() = false, want true")
	}
	if calls != 1 {
		t.Errorf("evaluated %d children, want 1 (short-circuit on first match)", calls)
	}
}

func TestBuild_Not(t *testing.T) {
	where := &rule.WhereDef{File: &rule.FilterDef{Not: &rule.FilterDef{HasStruct: "Mock"}}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(%+v) error = %v, want nil", where, err)
	}
	not, ok := f.(*not)
	if !ok {
		t.Fatalf("build() returned %T, want *not", f)
	}
	if _, isStruct := not.Inner.(*structFilter); !isStruct {
		t.Errorf("not.Inner = %T, want *structFilter", not.Inner)
	}
}

func TestBuild_Not_Nested(t *testing.T) {
	// not wrapping a not (double negation) compiles to nested not combinators.
	where := &rule.WhereDef{File: &rule.FilterDef{Not: &rule.FilterDef{Not: &rule.FilterDef{HasFunc: "Foo"}}}}
	f, err := build(where)
	if err != nil {
		t.Fatalf("build(nested not) error = %v, want nil", err)
	}
	outer, ok := f.(*not)
	if !ok {
		t.Fatalf("build(nested) = %T, want *not", f)
	}
	if _, isNested := outer.Inner.(*not); !isNested {
		t.Errorf("not.Inner = %T, want nested *not", outer.Inner)
	}
}

func TestBuild_Not_InvalidChild(t *testing.T) {
	// An empty inner FilterDef has no active predicate and must fail the build.
	where := &rule.WhereDef{File: &rule.FilterDef{Not: &rule.FilterDef{}}}
	if _, err := build(where); err == nil {
		t.Fatal("build(not with empty inner) error = nil, want error")
	}
}

func TestNot_Match(t *testing.T) {
	if (&not{Inner: stubFilter{result: true}}).Match(nil) {
		t.Error("not.Match() over a matching inner = true, want false")
	}
	if !(&not{Inner: stubFilter{result: false}}).Match(nil) {
		t.Error("not.Match() over a non-matching inner = false, want true")
	}
}

type filterExpected struct {
	Type        string `yaml:"type"`
	Func        string `yaml:"func"`
	Recv        string `yaml:"recv"`
	Struct      string `yaml:"struct"`
	Package     string `yaml:"package"`
	Directive   string `yaml:"directive"`
	ShouldMatch *bool  `yaml:"should_match"`
	// Children describes the expected sub-filters for combinator types
	// (e.g. allOf). It is nil for leaf filters.
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

	got, buildErr := build(&rule.WhereDef{File: &def})
	if strings.HasPrefix(name, "err_") {
		if buildErr == nil {
			t.Fatalf("build(%q) error = nil, want error", name)
		}
		return
	}
	if buildErr != nil {
		t.Fatalf("build(%q) error = %v, want nil", name, buildErr)
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

func assertBuiltFilter(t *testing.T, name string, got filter, want filterExpected) {
	t.Helper()

	switch want.Type {
	case "FuncFilter":
		funcFilter, ok := got.(*funcFilter)
		if !ok {
			t.Fatalf("build(%q) = %T, want *funcFilter", name, got)
		}
		if funcFilter.Func != want.Func || funcFilter.Recv != want.Recv {
			t.Fatalf("build(%q) = %+v, want func=%q recv=%q", name, funcFilter, want.Func, want.Recv)
		}
	case "StructFilter":
		structFilter, ok := got.(*structFilter)
		if !ok {
			t.Fatalf("build(%q) = %T, want *structFilter", name, got)
		}
		if structFilter.Struct != want.Struct {
			t.Fatalf("build(%q) = %+v, want struct=%q", name, structFilter, want.Struct)
		}
	case "DirectiveFilter":
		directiveFilter, ok := got.(*directiveFilter)
		if !ok {
			t.Fatalf("build(%q) = %T, want *directiveFilter", name, got)
		}
		if directiveFilter.Directive != want.Directive {
			t.Fatalf("build(%q) = %+v, want directive=%q", name, directiveFilter, want.Directive)
		}
	case "PackageNameFilter":
		pnf, ok := got.(*packageNameFilter)
		if !ok {
			t.Fatalf("build(%q) = %T, want *packageNameFilter", name, got)
		}
		if pnf.Name != want.Package {
			t.Fatalf("build(%q) packageNameFilter.Name = %q, want %q", name, pnf.Name, want.Package)
		}
	case "IsTestFilter":
		itf, ok := got.(*isTestFilter)
		if !ok {
			t.Fatalf("build(%q) = %T, want *isTestFilter", name, got)
		}
		if want.ShouldMatch == nil {
			t.Fatalf("expected file %q has type isTestFilter but no should_match field", name)
		}
		if itf.ShouldMatch != *want.ShouldMatch {
			t.Fatalf("build(%q) isTestFilter.ShouldMatch = %v, want %v", name, itf.ShouldMatch, *want.ShouldMatch)
		}
	case "AllOf", "OneOf", "Not":
		assertBuiltCombinator(t, name, got, want)
	default:
		t.Fatalf("unexpected expected filter type %q", want.Type)
	}
}

// assertBuiltCombinator verifies allOf/oneOf/not combinator filters and recurses
// into their children. It is split out of assertBuiltFilter so that neither
// function exceeds the linter's cognitive-complexity budget.
func assertBuiltCombinator(t *testing.T, name string, got filter, want filterExpected) {
	t.Helper()

	switch want.Type {
	case "AllOf":
		gotAllOf, ok := got.(allOf)
		if !ok {
			t.Fatalf("build(%q) = %T, want allOf", name, got)
		}
		if len(gotAllOf) != len(want.Children) {
			t.Fatalf("build(%q) allOf len = %d, want %d", name, len(gotAllOf), len(want.Children))
		}
		for i := range gotAllOf {
			assertBuiltFilter(t, name, gotAllOf[i], want.Children[i])
		}
	case "OneOf":
		gotOneOf, ok := got.(oneOf)
		if !ok {
			t.Fatalf("build(%q) = %T, want oneOf", name, got)
		}
		if len(gotOneOf) != len(want.Children) {
			t.Fatalf("build(%q) oneOf len = %d, want %d", name, len(gotOneOf), len(want.Children))
		}
		for i := range gotOneOf {
			assertBuiltFilter(t, name, gotOneOf[i], want.Children[i])
		}
	case "Not":
		gotNot, ok := got.(*not)
		if !ok {
			t.Fatalf("build(%q) = %T, want *not", name, got)
		}
		if len(want.Children) != 1 {
			t.Fatalf("build(%q) not expects exactly 1 child, got %d", name, len(want.Children))
		}
		assertBuiltFilter(t, name, gotNot.Inner, want.Children[0])
	}
}
