// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"gopkg.in/yaml.v3"

	"go.opentelemetry.io/otelc/tool/ex"
)

// whereSelectorKeys are the non-package selector keys accepted at the top of a
// where clause. Point selectors and the func/raw narrowing selectors are
// decoded onto the concrete rule struct (they drive AST matching); file and the
// combinators are the file-predicate surface consumed by the setup Filter.
//
//nolint:gochecknoglobals // package-private lookup table
var whereSelectorKeys = map[string]bool{
	// point selectors
	"func": true, "recv": true, "struct": true, "function_call": true,
	"directive": true, "kind": true, "identifier": true,
	// func-rule signature narrowing
	"signature": true, "signature_contains": true,
	"result": true, "last_result": true, "param": true,
	// raw-rule narrowing
	"pattern": true, "placement": true,
	// file predicate + combinators
	"file": true, "all-of": true, "one-of": true, "not": true,
}

// validateWhereKeys rejects package selectors nested inside where and any
// unrecognized where key, so a malformed or typo'd selector fails loudly at
// load time instead of being silently ignored. A nil node (no where clause) is
// valid.
func validateWhereKeys(where *yaml.Node) error {
	if where == nil {
		return nil
	}
	if where.Kind == yaml.AliasNode {
		where = where.Alias
	}
	if where.Kind != yaml.MappingNode {
		return ex.Newf("where must be a map")
	}
	for i := 0; i+1 < len(where.Content); i += 2 {
		key := where.Content[i].Value
		switch key {
		case "target":
			return ex.Newf("target must be top-level, not inside where")
		case "version":
			return ex.Newf("version must be top-level, not inside where")
		default:
			if !whereSelectorKeys[key] {
				return ex.Newf("unsupported where key %q", key)
			}
		}
	}
	return nil
}

// decodeWhereSelectors decodes a where clause's point and narrowing selectors
// onto the concrete rule (func, recv, struct, function_call, directive, kind,
// identifier, signature*, result, last_result, param, pattern, placement).
// The file leaf and combinators have no matching field on a concrete rule and
// are ignored here; whereFilter routes them to base.Where instead. A nil node
// (no where clause) is a no-op.
func decodeWhereSelectors(where *yaml.Node, target any) error {
	if where == nil {
		return nil
	}
	if err := where.Decode(target); err != nil {
		return ex.Wrap(err)
	}
	return nil
}

// whereFilter extracts only the file-predicate portion of a where clause — the
// file leaf plus the all-of/one-of/not combinators — which is all the setup
// Filter consumes. Point selectors are decoded onto the concrete rule instead
// and must not leak into the returned WhereDef, which filter.Build rejects as
// unsupported selector composition. Returns nil when there is no file predicate
// and no combinator, matching the "no executable where" contract.
//
//nolint:nilnil // a nil WhereDef means "no executable where predicate"
func whereFilter(where *yaml.Node) (*WhereDef, error) {
	if where == nil {
		return nil, nil
	}
	var w WhereDef
	if err := where.Decode(&w); err != nil {
		return nil, ex.Wrap(err)
	}
	if w.File == nil && w.AllOf == nil && w.OneOf == nil && w.Not == nil {
		return nil, nil
	}
	return &WhereDef{File: w.File, AllOf: w.AllOf, OneOf: w.OneOf, Not: w.Not}, nil
}
