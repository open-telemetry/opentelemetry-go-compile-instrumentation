// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// File-level predicate evaluation for the structured where.file clause defined
// in ADR-0003. Filters are constructed once per rule from a [FilterDef] (the
// YAML representation) via [Build], then evaluated once per source file during
// the setup phase. A nil Filter value is valid and means "no filtering" — the
// rule applies unconditionally to any matching source file.
//
// The runtime filter tree maps directly onto the YAML where.file shape:
//
//	where:
//	  file:
//	    all-of:           # AllOf combinator
//	      - has_func: Foo # FuncFilter leaf
//	      - has_struct: Bar # StructFilter leaf

// --- Filter interface and context ---

// Filter is the runtime interface for file-level join-point filtering.
// A Filter evaluates whether an instrumentation rule should be applied to a
// specific source file based on contextual information.
//
// Implementations must be safe for concurrent use: a single Filter instance
// is evaluated across multiple source files, potentially from parallel
// goroutines spawned by matchDeps.
type Filter interface {
	Match(ctx *MatchContext) bool
}

// MatchContext carries the per-file information available to where.file
// predicates. It is constructed once per source file in the setup phase and
// passed to all filters associated with the rules being evaluated for that
// file.
type MatchContext struct {
	// ImportPath is the Go import path of the package containing the source
	// file.
	ImportPath string

	// SourceFile is the absolute path to the source file being evaluated.
	SourceFile string

	// AST is the parsed dst tree of the source file. Filters must treat it
	// as read-only; node updates would corrupt downstream rule matching.
	AST *dst.File
}

// --- Leaf filters ---

var (
	_ Filter = (*FuncFilter)(nil)
	_ Filter = (*StructFilter)(nil)
)

// FuncFilter matches source files that declare the named function or method.
type FuncFilter struct {
	Func string
	Recv string
}

func (f *FuncFilter) Match(ctx *MatchContext) bool {
	return ast.FindFuncDecl(ctx.AST, f.Func, f.Recv) != nil
}

// StructFilter matches source files that declare the named struct.
type StructFilter struct {
	Struct string
}

func (f *StructFilter) Match(ctx *MatchContext) bool {
	return ast.FindStructDecl(ctx.AST, f.Struct) != nil
}

// --- Combinators ---

var _ Filter = (AllOf)(nil)

// AllOf matches when every child filter matches. An empty AllOf matches
// vacuously (all conditions in an empty set are satisfied). Evaluation
// short-circuits on the first non-matching child.
type AllOf []Filter

func (a AllOf) Match(ctx *MatchContext) bool {
	for _, f := range a {
		if !f.Match(ctx) {
			return false
		}
	}
	return true
}

// --- Build ---

// Build constructs a runtime Filter from a structured where clause.
//
// A nil result is valid and means the rule has no executable where.file
// predicate.
//
//nolint:nilnil // nil Filter means "no executable file predicate"
func Build(where *rule.WhereDef) (Filter, error) {
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
// silently ignored, so an ambiguous spec fails fast at Build time. An empty
// all-of: [] is treated as present and compiles to an empty AllOf{}, which
// matches vacuously (see AllOf.Match) — consistent with the documented type
// semantics.
//
//nolint:nilnil // unreachable default branch is guarded by util.ShouldNotReachHere
func buildFile(def *rule.FilterDef) (Filter, error) {
	// Presence is detected via a non-nil slice (not len > 0): YAML unmarshals an
	// explicit all-of: [] to a non-nil empty slice, and that empty combinator is
	// a deliberate, vacuously-true predicate — not the absence of one.
	if def.AllOf != nil {
		// all-of owns the composition for this node; sibling predicates would be
		// silently ignored, so reject the ambiguous combination explicitly. This
		// guard runs for the empty case too, so all-of: [] + has_func: X is still
		// rejected.
		if def.HasFunc != "" || def.HasRecv != "" || def.HasStruct != "" ||
			def.HasDirective != "" || len(def.OneOf) > 0 || def.Not != nil {
			return nil, ex.Newf("where.file.all-of cannot be combined with other predicates")
		}
		return buildAllOf(def.AllOf)
	}
	if len(def.OneOf) > 0 {
		return nil, ex.Newf("where.file one-of predicate composition is not yet supported")
	}
	if def.Not != nil {
		return nil, ex.Newf("where.file not predicate composition is not yet supported")
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

	if active == 0 {
		return nil, ex.Newf("where.file has no active predicate")
	}
	if active > 1 {
		return nil, ex.Newf("where.file has multiple active predicates; explicit composition is not yet supported")
	}

	switch {
	case def.HasFunc != "":
		return &FuncFilter{Func: def.HasFunc, Recv: def.HasRecv}, nil
	case def.HasStruct != "":
		return &StructFilter{Struct: def.HasStruct}, nil
	case def.HasDirective != "":
		return nil, ex.Newf("where.file.has_directive is not yet supported")
	default:
		// The active-predicate counter above proves at least one leaf is set;
		// matching the convention in match.go / instrument.go / trampoline.go,
		// flag this branch as unreachable rather than synthesizing an error.
		util.ShouldNotReachHere()
		return nil, nil
	}
}

// buildAllOf compiles a where.file.all-of group into an AllOf combinator that
// matches only when every child predicate matches. Children are compiled with
// the same buildFile rules, so nesting (all-of within all-of) composes
// naturally.
func buildAllOf(defs []rule.FilterDef) (Filter, error) {
	filters := make(AllOf, 0, len(defs))
	for i := range defs {
		f, err := buildFile(&defs[i])
		if err != nil {
			return nil, ex.Wrapf(err, "where.file.all-of[%d]", i)
		}
		if f == nil {
			// buildFile returns a non-nil filter for every valid leaf; a nil here
			// would make AllOf.Match panic, so fail loudly instead.
			return nil, ex.Newf("where.file.all-of[%d] produced no filter", i)
		}
		filters = append(filters, f)
	}
	return filters, nil
}
