// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

// Build constructs a runtime Filter from a FilterDef.
//
// Returns (nil, nil) when def is nil, meaning no additional filtering is
// applied and the rule matches all eligible source files.
//
// Returns an error when def contains an invalid or not-yet-implemented
// configuration. Unsupported combinators (all-of, one-of, not) and
// not-yet-implemented leaf types (directive, import_path, package_name,
// test_main) return descriptive errors.
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

	// Recv without Func is a misconfiguration — catch it early.
	if def.Recv != "" && def.Func == "" {
		return nil, ex.Newf("recv requires func to be set in filter definition")
	}

	// Count active leaf predicates (Recv is part of Func, not independent).
	// Keep this block in sync with the FilterDef predicate fields.
	active := 0
	if def.Func != "" {
		active++
	}
	if def.Struct != "" {
		active++
	}
	if def.Directive != "" {
		active++
	}
	if def.ImportPath != "" {
		active++
	}
	if def.PackageName != "" {
		active++
	}
	if def.TestMain != nil {
		active++
	}

	if active == 0 {
		return nil, ex.Newf("filter definition has no active predicate")
	}
	if active > 1 {
		return nil, ex.Newf("filter definition has multiple active predicates; use all-of to combine them")
	}

	switch {
	case def.Func != "":
		return &FuncFilter{Func: def.Func, Recv: def.Recv}, nil
	case def.Struct != "":
		return &StructFilter{Struct: def.Struct}, nil
	case def.Directive != "":
		return nil, ex.Newf("directive filter requires directive support (not yet available)")
	case def.ImportPath != "":
		return &ImportPathFilter{Pattern: def.ImportPath}, nil
	case def.PackageName != "":
		return nil, ex.Newf("package_name filter is not yet supported")
	case def.TestMain != nil:
		return nil, ex.Newf("test_main filter is not yet supported")
	default:
		// Unreachable: active == 1 guarantees one of the cases above matched.
		// If this fires, a new FilterDef field was added without a matching case.
		return nil, ex.Newf("internal error: unhandled active predicate in buildDef")
	}
}
