// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

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

	if def.Recv != "" && def.HasFunc == "" {
		return nil, ex.Newf("where.file.recv requires where.file.has_func")
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
		return &FuncFilter{Func: def.HasFunc, Recv: def.Recv}, nil
	case def.HasStruct != "":
		return &StructFilter{Struct: def.HasStruct}, nil
	case def.HasDirective != "":
		return nil, ex.Newf("where.file.has_directive is not yet supported")
	default:
		return nil, ex.Newf("where.file reached an unsupported predicate shape")
	}
}
