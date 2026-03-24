// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package filter provides the Filter interface and related types for join point
// filtering during the compile instrumentation setup phase.
//
// Filters are constructed once per rule from a [rule.FilterDef] (the YAML
// representation) via [Build], then evaluated against source files during
// the setup phase. A nil Filter value is valid and means "no filtering" —
// the rule applies unconditionally to any matching source file.
//
// The filter tree maps directly onto the YAML where clause:
//
//	where:
//	  all-of:           # AllOf combinator (not yet implemented)
//	    - func: Foo     # FuncFilter leaf
//	    - struct: Bar   # StructFilter leaf
package filter

import "github.com/dave/dst"

// Filter is the runtime interface for join point filtering.
// A Filter evaluates whether an instrumentation rule should be applied
// to a specific source file based on contextual information.
//
// Implementations must be safe for concurrent use: a single Filter instance
// is evaluated across multiple source files, potentially from parallel goroutines.
type Filter interface {
	Match(ctx *MatchContext) bool
}

// MatchContext carries per-file contextual information for filter evaluation.
// It is constructed once per source file in preciseMatching and passed to all
// filters associated with the rules being evaluated for that file.
type MatchContext struct {
	// ImportPath is the Go import path of the package containing the source file.
	ImportPath string

	// SourceFile is the absolute path to the source file being evaluated.
	SourceFile string

	// AST is the parsed decorated syntax tree of the source file.
	// Filters that inspect declarations (FuncFilter, StructFilter) use this field.
	// The declared package name is available via AST.Name.Name.
	AST *dst.File
}
