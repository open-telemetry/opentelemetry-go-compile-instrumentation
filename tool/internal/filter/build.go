// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// Build constructs a runtime Filter from a FilterDef.
//
// Returns (nil, nil) when def is nil, meaning no additional filtering is
// applied and the rule matches all eligible source files.
//
// Returns an error when def contains an invalid or not-yet-implemented
// configuration. Unsupported combinators (all-of, one-of, not) and
// not-yet-implemented leaf types (has_directive, include_test) return
// descriptive errors.
//
//nolint:nilnil // nil Filter is a valid return: it means "no filtering required"
func Build(def *rule.FilterDef) (Filter, error) {
	if def == nil {
		return nil, nil
	}
	return buildDef(def)
}

func buildDef(def *rule.FilterDef) (Filter, error) {
	// Combinators are not yet implemented; return a clear error.
	if len(def.AllOf) > 0 {
		return nil, ex.Newf("all-of combinator is not yet supported")
	}
	if len(def.OneOf) > 0 {
		return nil, ex.Newf("one-of combinator is not yet supported")
	}
	if def.Not != nil {
		return nil, ex.Newf("not combinator is not yet supported")
	}

	// HasRecv without HasFunc is a misconfiguration — catch it early.
	if def.HasRecv != "" && def.HasFunc == "" {
		return nil, ex.Newf("has_recv requires has_func to be set in filter definition")
	}

	// Count active leaf predicates (Recv is part of HasFunc, not independent).
	// Keep this block in sync with the FilterDef predicate fields.
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
	if def.IncludeTest != nil {
		active++
	}

	if active == 0 {
		return nil, ex.Newf("filter definition has no active predicate")
	}
	if active > 1 {
		return nil, ex.Newf(
			"filter definition has multiple active predicates;" +
				" combining predicates is not yet supported (use all-of once available)",
		)
	}

	switch {
	case def.HasFunc != "":
		return &FuncFilter{Func: def.HasFunc, Recv: def.HasRecv}, nil
	case def.HasStruct != "":
		return &StructFilter{Struct: def.HasStruct}, nil
	case def.HasDirective != "":
		return nil, ex.Newf("has_directive filter requires directive support (not yet available)")
	case def.IncludeTest != nil:
		return nil, ex.Newf("include_test filter is not yet supported")
	default:
		// Unreachable: active == 1 guarantees one of the cases above matched.
		// If this fires, a new FilterDef field was added without a matching case.
		util.ShouldNotReachHere()
		return nil, nil //nolint:nilnil // unreachable; satisfies compiler
	}
}
