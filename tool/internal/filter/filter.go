// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package filter provides file-level predicate evaluation for structured
// where.file clauses during the setup phase.
package filter

import "github.com/dave/dst"

// Filter evaluates whether a rule should be considered for a specific source
// file during setup matching.
type Filter interface {
	Match(ctx *MatchContext) bool
}

// MatchContext carries the per-file information available to where.file
// predicates.
type MatchContext struct {
	ImportPath string
	SourceFile string
	AST        *dst.File
}
