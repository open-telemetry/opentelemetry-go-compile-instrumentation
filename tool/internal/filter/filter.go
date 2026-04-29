// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package filter provides file-level predicate evaluation for the structured
// where.file clause defined in ADR-0003. Filters are constructed once per
// rule from a [rule.FilterDef] (the YAML representation) via [Build], then
// evaluated once per source file during the setup phase. A nil Filter value
// is valid and means "no filtering" — the rule applies unconditionally to any
// matching source file.
//
// The runtime filter tree maps directly onto the YAML where.file shape:
//
//	where:
//	  file:
//	    all-of:           # AllOf combinator (not yet implemented)
//	      - has_func: Foo # FuncFilter leaf
//	      - has_struct: Bar # StructFilter leaf
package filter

import "github.com/dave/dst"

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
