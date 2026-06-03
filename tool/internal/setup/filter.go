// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"strings"

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
//	    all-of:           # AllOf combinator (not yet implemented)
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
	_ Filter = (*IsTestFilter)(nil)
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

// IsTestFilter selects or excludes test packages based on whether the
// package's import path carries the ".test" suffix that the Go toolchain
// appends when compiling a test binary.
//
// ShouldMatch == true  → match only test packages (import path ends in ".test")
// ShouldMatch == false → match only non-test packages
//
// The predicate is tri-state at the schema level: a nil *bool in FilterDef
// means "unset" (no filtering), while true/false express explicit intent.
// This filter is only constructed when the field is explicitly set, so
// ShouldMatch is never ambiguous once an IsTestFilter exists.
type IsTestFilter struct {
	ShouldMatch bool
}

// testImportPathSuffix is the suffix the Go toolchain appends to the import
// path of a package when building a test binary.
const testImportPathSuffix = ".test"

func (f *IsTestFilter) Match(ctx *MatchContext) bool {
	isTest := strings.HasSuffix(ctx.ImportPath, testImportPathSuffix)
	return f.ShouldMatch == isTest
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

//nolint:nilnil // unreachable default branch is guarded by util.ShouldNotReachHere
func buildFile(def *rule.FilterDef) (Filter, error) {
	if len(def.AllOf) > 0 {
		return nil, ex.Newf("where.file all-of predicate composition is not yet supported")
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
		return &FuncFilter{Func: def.HasFunc, Recv: def.HasRecv}, nil
	case def.HasStruct != "":
		return &StructFilter{Struct: def.HasStruct}, nil
	case def.HasDirective != "":
		return nil, ex.Newf("where.file.has_directive is not yet supported")
	case def.IsTest != nil:
		return &IsTestFilter{ShouldMatch: *def.IsTest}, nil
	default:
		// The active-predicate counter above proves at least one leaf is set;
		// matching the convention in match.go / instrument.go / trampoline.go,
		// flag this branch as unreachable rather than synthesizing an error.
		util.ShouldNotReachHere()
		return nil, nil
	}
}
