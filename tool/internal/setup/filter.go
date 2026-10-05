// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"strings"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// File-level predicate evaluation for the structured where.file clause defined
// in ADR-0003. Filters are constructed once per rule from a [FilterDef] (the
// YAML representation) via [build], then evaluated once per source file during
// the setup phase. A nil filter value is valid and means "no filtering" — the
// rule applies unconditionally to any matching source file.
//
// The runtime filter tree maps directly onto the YAML where.file shape:
//
//	where:
//	  file:
//	    all-of:           # allOf combinator
//	      - has_func: Foo # funcFilter leaf
//	      - has_struct: Bar # structFilter leaf

// --- filter interface and context ---

// filter is the runtime interface for file-level join-point filtering.
// A filter evaluates whether an instrumentation rule should be applied to a
// specific source file based on contextual information.
//
// Implementations must be safe for concurrent use: a single filter instance
// is evaluated across multiple source files, potentially from parallel
// goroutines spawned by matchDeps.
type filter interface {
	Match(ctx *matchContext) bool
}

// matchContext carries the per-file information available to where.file
// predicates. It is constructed once per source file in the setup phase and
// passed to all filters associated with the rules being evaluated for that
// file.
type matchContext struct {
	// IsTest reports whether the source file is part of a test build — a
	// compilation the Go toolchain produces only while building a test binary
	// (a package augmented with its _test.go files, an external xxx_test
	// package, or the generated _testmain.go runner). It is a property of the
	// whole compile, so it is identical for every file in a given package.
	IsTest bool

	// SourceFile is the absolute path to the source file being evaluated.
	SourceFile string

	// AST is the parsed dst tree of the source file. Filters must treat it
	// as read-only; node updates would corrupt downstream rule matching.
	AST *dst.File
}

// --- Leaf filters ---

var (
	_ filter = (*funcFilter)(nil)
	_ filter = (*structFilter)(nil)
	_ filter = (*packageNameFilter)(nil)
	_ filter = (*isTestFilter)(nil)
	_ filter = (*directiveFilter)(nil)
)

// funcFilter matches source files that declare the named function or method.
type funcFilter struct {
	Func string
	Recv string
}

func (f *funcFilter) Match(ctx *matchContext) bool {
	// We create an `InstFuncRule`` because including `setup.funcFilter` to
	// `ast.FindFuncDecl` causes an import loop.
	fr := &rule.InstFuncRule{
		Func: f.Func,
		Recv: f.Recv,
	}
	_, ok, _ := ast.FindFuncDecl(ctx.AST, fr)
	return ok
}

// structFilter matches source files that declare the named struct.
type structFilter struct {
	Struct string
}

func (f *structFilter) Match(ctx *matchContext) bool {
	// Only true structs match; an interface or alias of the same name does not.
	// This inverts under `not`, which now includes files it once excluded.
	return ast.FindStructType(ctx.AST, f.Struct) != nil
}

// packageNameFilter matches source files whose declared package clause equals
// Name. The declared name is read from ctx.AST.Name.Name (the `package foo`
// line), not the import path (use target for that) and not the build's
// test-ness (use is_test for that). Non-test files in a package share one
// declared name; an external test file may declare a different name (e.g.
// "foo_test").
type packageNameFilter struct {
	Name string
}

func (f *packageNameFilter) Match(ctx *matchContext) bool {
	return ctx.AST.Name.Name == f.Name
}

// directiveFilter matches source files that contain the specified directive.
type directiveFilter struct {
	Directive string
}

func (f *directiveFilter) Match(ctx *matchContext) bool {
	return ast.FileHasLeadingDirective(ctx.AST, f.Directive)
}

// isTestFilter selects or excludes test builds — compilations the Go toolchain
// produces only as part of `go test` (see matchContext.IsTest).
//
// ShouldMatch == true  → match only test builds
// ShouldMatch == false → match only non-test builds
//
// The predicate is tri-state at the schema level: a nil *bool in FilterDef
// means "unset" (no filtering), while true/false express explicit intent.
// This filter is only constructed when the field is explicitly set, so
// ShouldMatch is never ambiguous once an isTestFilter exists.
type isTestFilter struct {
	ShouldMatch bool
}

func (f *isTestFilter) Match(ctx *matchContext) bool {
	return f.ShouldMatch == ctx.IsTest
}

// --- Combinators ---

var _ filter = (allOf)(nil)

// allOf matches when every child filter matches. An empty allOf matches
// vacuously (all conditions in an empty set are satisfied). Evaluation
// short-circuits on the first non-matching child.
type allOf []filter

func (a allOf) Match(ctx *matchContext) bool {
	for _, f := range a {
		if !f.Match(ctx) {
			return false
		}
	}
	return true
}

var _ filter = (oneOf)(nil)

// oneOf matches when at least one child filter matches. An empty oneOf never
// matches (no condition in an empty set is satisfied). Evaluation
// short-circuits on the first matching child.
type oneOf []filter

func (o oneOf) Match(ctx *matchContext) bool {
	for _, f := range o {
		if f.Match(ctx) {
			return true
		}
	}
	return false
}

var _ filter = (*not)(nil)

// not matches when its inner filter does not match (logical negation).
type not struct{ Inner filter }

func (n *not) Match(ctx *matchContext) bool { return !n.Inner.Match(ctx) }

// --- build ---

// build constructs a runtime filter from a structured where clause.
//
// A nil result is valid and means the rule has no executable where.file
// predicate.
//
//nolint:nilnil // nil filter means "no executable file predicate"
func build(where *rule.WhereDef) (filter, error) {
	if where == nil {
		return nil, nil
	}

	if len(where.AllOf) > 0 {
		return nil, ex.Newf("where all-of selector composition is not yet supported")
	}
	if len(where.OneOf) > 0 {
		return nil, ex.Newf("where one-of selector composition is not yet supported")
	}
	if where.Not != nil {
		return nil, ex.Newf("where not selector composition is not yet supported")
	}

	if where.Func != "" || where.Recv != "" || where.Struct != "" ||
		where.FunctionCall != "" || where.Directive != "" ||
		where.Kind != "" || where.Identifier != "" {
		return nil, ex.Newf("where selector composition beyond where.file is not yet supported")
	}

	if where.File == nil {
		return nil, nil
	}

	return buildFile(where.File)
}

// buildFile compiles the where.file predicate for a single node.
//
// When all-of is present (a non-nil slice, including an explicit empty
// all-of: []), it owns the composition for this node: sibling leaf predicates
// and other combinators on the same node are rejected outright rather than
// silently ignored, so an ambiguous spec fails fast at build time. An empty
// all-of: [] is treated as present and compiles to an empty allOf{}, which
// matches vacuously (see allOf.Match) — consistent with the documented type
// semantics.
//
// hasLeafPredicate reports whether any leaf (non-combinator) where.file
// predicate is set on def. The combinator branches use it to reject a leaf
// predicate that sits as a sibling of a combinator on the same node.
func hasLeafPredicate(def *rule.FilterDef) bool {
	return def.HasFunc != "" || def.HasRecv != "" ||
		def.HasStruct != "" || def.HasDirective != "" ||
		strings.TrimSpace(def.HasPackage) != "" || def.IsTest != nil
}

//nolint:nilnil // unreachable default branch is guarded by util.ShouldNotReachHere
func buildFile(def *rule.FilterDef) (filter, error) {
	// Presence is detected via a non-nil slice (not len > 0): YAML unmarshals an
	// explicit all-of: [] to a non-nil empty slice, and that empty combinator is
	// a deliberate, vacuously-true predicate — not the absence of one.
	if def.AllOf != nil {
		// all-of owns the composition for this node; sibling predicates would be
		// silently ignored, so reject the ambiguous combination explicitly. This
		// guard runs for the empty case too, so all-of: [] + has_func: X is still
		// rejected. Sibling combinators are detected by non-nil presence (not
		// len > 0), so an explicit empty one-of: [] is also rejected here.
		if hasLeafPredicate(def) || def.OneOf != nil || def.Not != nil {
			return nil, ex.Newf("where.file.all-of cannot be combined with other predicates")
		}
		children, err := buildChildren(def.AllOf, "all-of")
		if err != nil {
			return nil, err
		}
		return allOf(children), nil
	}
	// Presence via non-nil slice (mirrors all-of): an explicit one-of: [] is a
	// deliberate, vacuously-false predicate (oneOf.Match returns false for an
	// empty set), not the absence of one.
	if def.OneOf != nil {
		// one-of owns the composition for this node; reject sibling predicates
		// that would otherwise be silently ignored. The guard runs for the empty
		// case too, so one-of: [] + has_func: X is still rejected. Sibling
		// combinators are detected by non-nil presence, independent of branch
		// order.
		if hasLeafPredicate(def) || def.AllOf != nil || def.Not != nil {
			return nil, ex.Newf("where.file.one-of cannot be combined with other predicates")
		}
		children, err := buildChildren(def.OneOf, "one-of")
		if err != nil {
			return nil, err
		}
		return oneOf(children), nil
	}
	if def.Not != nil {
		// not owns the composition for this node; reject sibling predicates.
		// Sibling combinators are detected by non-nil presence (not len > 0), so
		// an explicit empty all-of: [] / one-of: [] is also rejected here.
		if hasLeafPredicate(def) || def.AllOf != nil || def.OneOf != nil {
			return nil, ex.Newf("where.file.not cannot be combined with other predicates")
		}
		return buildNot(def.Not)
	}

	if def.HasRecv != "" && def.HasFunc == "" {
		return nil, ex.Newf("where.file.has_recv requires where.file.has_func")
	}

	active := 0
	if def.HasFunc != "" {
		active++
	}
	if def.HasStruct != "" {
		active++
	}
	if def.HasDirective != "" {
		active++
	}
	if strings.TrimSpace(def.HasPackage) != "" {
		active++
	}
	if def.IsTest != nil {
		active++
	}

	if active == 0 {
		return nil, ex.Newf("where.file has no active predicate")
	}
	if active > 1 {
		return nil, ex.Newf("where.file has multiple active predicates; explicit composition is not yet supported")
	}

	switch {
	case def.HasFunc != "":
		return &funcFilter{Func: def.HasFunc, Recv: def.HasRecv}, nil
	case def.HasStruct != "":
		return &structFilter{Struct: def.HasStruct}, nil
	case def.HasDirective != "":
		return &directiveFilter{Directive: def.HasDirective}, nil
	case strings.TrimSpace(def.HasPackage) != "":
		return &packageNameFilter{Name: strings.TrimSpace(def.HasPackage)}, nil
	case def.IsTest != nil:
		return &isTestFilter{ShouldMatch: *def.IsTest}, nil
	default:
		// The active-predicate counter above proves at least one leaf is set;
		// matching the convention in match.go / instrument.go / trampoline.go,
		// flag this branch as unreachable rather than synthesizing an error.
		util.ShouldNotReachHere()
		return nil, nil
	}
}

// buildChildren compiles each child of a where.file combinator group with the
// same buildFile rules, so nesting (a combinator within a combinator) composes
// naturally. The caller converts the result to the concrete combinator type
// (allOf / oneOf). label names the combinator for error context.
func buildChildren(defs []rule.FilterDef, label string) ([]filter, error) {
	filters := make([]filter, 0, len(defs))
	for i := range defs {
		f, err := buildFile(&defs[i])
		if err != nil {
			return nil, ex.Wrapf(err, "where.file.%s[%d]", label, i)
		}
		if f == nil {
			// buildFile returns a non-nil filter for every valid leaf; a nil here
			// would make the combinator's Match panic, so fail loudly instead.
			return nil, ex.Newf("where.file.%s[%d] produced no filter", label, i)
		}
		filters = append(filters, f)
	}
	return filters, nil
}

// buildNot compiles a where.file.not predicate into a not combinator that
// negates its single inner predicate. The inner predicate is compiled with the
// same buildFile rules.
func buildNot(def *rule.FilterDef) (filter, error) {
	inner, err := buildFile(def)
	if err != nil {
		return nil, ex.Wrapf(err, "where.file.not")
	}
	if inner == nil {
		return nil, ex.Newf("where.file.not produced no inner filter")
	}
	return &not{Inner: inner}, nil
}
