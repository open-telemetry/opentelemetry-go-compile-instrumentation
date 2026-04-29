// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"maps"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

// Normalize detects the structured target/version + where + do format defined
// in ADR-0003 and expands it into one or more flat rule maps expected by the
// existing rule constructors. If the fields map contains neither "where" nor
// "do", it is returned unchanged as a single-element slice (legacy passthrough
// for inline test strings).
//
// Canonical structured form:
//
//	rule_name:
//	  target: database/sql
//	  version: "v1.0.0,v2.0.0"
//	  where:
//	    func: Open
//	    file:
//	      has_func: init
//	  do:
//	    - inject_hooks:
//	        before: BeforeOpen
//	        path: "github.com/example/sql"
//	  imports:
//	    fmt: fmt
//
// `do` accepts two YAML shapes; both normalize to the same ordered list:
//
//   - sequence: `do: - inject_hooks: ...` (canonical, supports N modifiers)
//   - map:      `do: inject_hooks: ...`   (sugar for a single-modifier rule)
//
// Flat (internal) form that rule constructors consume:
//
//	target: database/sql
//	version: "v1.0.0,v2.0.0"
//	func: Open
//	before: BeforeOpen
//	path: "github.com/example/sql"
//	where:
//	  file:
//	    has_func: init
//	imports:
//	  fmt: fmt
func Normalize(fields map[string]any) ([]map[string]any, error) {
	_, hasWhere := fields["where"]
	_, hasDo := fields["do"]
	if !hasWhere && !hasDo {
		return []map[string]any{fields}, nil
	}
	if !hasDo {
		return nil, ex.Newf("structured rule is missing do")
	}

	common := make(map[string]any)

	// Copy top-level fields (e.g. imports, name) that sit outside where/do.
	for k, v := range fields {
		if k != "where" && k != "do" {
			common[k] = v
		}
	}

	if whereRaw, ok := fields["where"]; ok {
		whereMap, isMap := whereRaw.(map[string]any)
		if !isMap {
			return nil, ex.Newf("where must be a map")
		}
		normalizedWhere, err := normalizeWhere(common, whereMap)
		if err != nil {
			return nil, err
		}
		if len(normalizedWhere) > 0 {
			common["where"] = normalizedWhere
		}
	}

	doItems, err := normalizeDo(fields["do"])
	if err != nil {
		return nil, err
	}

	normalized := make([]map[string]any, 0, len(doItems))
	for _, item := range doItems {
		flat := maps.Clone(common)
		maps.Copy(flat, item)
		normalized = append(normalized, flat)
	}

	return normalized, nil
}

// normalizeWhere splits the where map into selectors that get hoisted to the
// flat common fields (rule-level point selectors) and selectors that stay
// nested under where (file predicates, qualifier composition).
//
// target/version are explicitly rejected here because they belong to the
// top-level package selector slot per ADR-0003.
func normalizeWhere(common, where map[string]any) (map[string]any, error) {
	if _, exists := where["target"]; exists {
		return nil, ex.Newf("target must be top-level, not inside where")
	}
	if _, exists := where["version"]; exists {
		return nil, ex.Newf("version must be top-level, not inside where")
	}

	normalized := make(map[string]any)
	for key, value := range where {
		switch key {
		case "func", "recv", "struct", "function_call", "directive", "kind", "identifier":
			common[key] = value
		case "file":
			if _, ok := value.(map[string]any); !ok {
				return nil, ex.Newf("where.file must be a map")
			}
			normalized[key] = value
		case "all-of", "one-of", "not":
			normalized[key] = value
		default:
			return nil, ex.Newf("unsupported where key %q", key)
		}
	}

	return normalized, nil
}

// normalizeDo accepts the two YAML shapes for do:
//
//   - sequence of single-key modifier maps (canonical);
//   - single-key map (sugar for a one-element sequence).
//
// Both shapes produce the same internal representation: an ordered list of
// modifier-payload maps, one per modifier entry.
func normalizeDo(doRaw any) ([]map[string]any, error) {
	switch typed := doRaw.(type) {
	case []any:
		return normalizeDoSequence(typed)
	case map[string]any:
		return normalizeDoMap(typed)
	default:
		return nil, ex.Newf("do must be a single-key map or a non-empty list of single-key modifier objects")
	}
}

func normalizeDoSequence(items []any) ([]map[string]any, error) {
	if len(items) == 0 {
		return nil, ex.Newf("do must not be empty")
	}

	normalized := make([]map[string]any, 0, len(items))
	for idx, item := range items {
		modifierMap, isMap := item.(map[string]any)
		if !isMap {
			return nil, ex.Newf("do[%d] must be a single-key modifier object", idx)
		}
		if len(modifierMap) != 1 {
			return nil, ex.Newf("do[%d] must contain exactly one modifier key", idx)
		}
		for _, modifierRaw := range modifierMap {
			modifierFields, hasModifierFields := modifierRaw.(map[string]any)
			if !hasModifierFields {
				return nil, ex.Newf("do[%d] modifier payload must be a map", idx)
			}
			normalized = append(normalized, maps.Clone(modifierFields))
		}
	}

	return normalized, nil
}

func normalizeDoMap(modifier map[string]any) ([]map[string]any, error) {
	if len(modifier) == 0 {
		return nil, ex.Newf("do must not be empty")
	}
	if len(modifier) != 1 {
		return nil, ex.Newf(
			"do must contain exactly one modifier key when written as a map; " +
				"use the sequence form for multiple modifiers",
		)
	}
	for _, modifierRaw := range modifier {
		modifierFields, ok := modifierRaw.(map[string]any)
		if !ok {
			return nil, ex.Newf("do modifier payload must be a map")
		}
		return []map[string]any{maps.Clone(modifierFields)}, nil
	}
	// Unreachable: len-1 map iteration guaranteed above.
	return nil, ex.Newf("do reached an unsupported shape")
}
