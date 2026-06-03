// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"maps"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// Structured top-level keys.
const (
	KeyWhere = "where"
	KeyDo    = "do"

	// KeyDoIndex is an internal field stamped onto each flat rule map produced by
	// expanding a do sequence. It records the zero-based position of the modifier
	// within the do list so that downstream name generation can disambiguate
	// multiple modifiers that target the same function (see InstFuncRule.String).
	// It is not part of the user-facing rule schema.
	KeyDoIndex = "do_index"
)

// where selectors (hoisted to flat by normalizeWhere).
const (
	SelTarget       = "target"
	SelVersion      = "version"
	SelFunc         = "func"
	SelRecv         = "recv"
	SelStruct       = "struct"
	SelFunctionCall = "function_call"
	SelDirective    = "directive"
	SelKind         = "kind"
	SelIdentifier   = "identifier"

	// Signature match-narrowing selectors for func rules (see InstFuncRule).
	SelSignature         = "signature"
	SelSignatureContains = "signature_contains"
	SelResult            = "result"
	SelLastResult        = "last_result"
	SelParam             = "param"

	// Raw match-narrowing selector for raw rules (see InstRawRule).
	SelPattern   = "pattern"
	SelPlacement = "placement"
)

// where sub-groups / combinators (preserved nested under flat).
const (
	WhereFile = "file"
	CombAllOf = "all-of"
	CombOneOf = "one-of"
	CombNot   = "not"
)

// RawField is the modifier-output key produced by normalize for raw rules.
// It is not a where selector; exposed here so match.go can share the literal.
const RawField = "raw"

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
	_, hasWhere := fields[KeyWhere]
	_, hasDo := fields[KeyDo]
	if !hasWhere && !hasDo {
		return []map[string]any{fields}, nil
	}
	if !hasDo {
		return nil, ex.Newf("structured rule is missing do")
	}

	common := make(map[string]any)

	// Copy top-level fields (e.g. imports, name) that sit outside where/do.
	for k, v := range fields {
		if k != KeyWhere && k != KeyDo {
			common[k] = v
		}
	}

	if whereRaw, ok := fields[KeyWhere]; ok {
		whereMap, isMap := whereRaw.(map[string]any)
		if !isMap {
			return nil, ex.Newf("where must be a map")
		}
		normalizedWhere, err := normalizeWhere(common, whereMap)
		if err != nil {
			return nil, err
		}
		if len(normalizedWhere) > 0 {
			common[KeyWhere] = normalizedWhere
		}
	}

	doItems, err := normalizeDo(fields[KeyDo])
	if err != nil {
		return nil, err
	}

	normalized := make([]map[string]any, 0, len(doItems))
	for idx, item := range doItems {
		flat := maps.Clone(common)
		maps.Copy(flat, item)
		// Stamp the do-sequence position so name generation can disambiguate
		// multiple modifiers that target the same function. Index 0 is omitted
		// so that single-modifier rules keep their historical generated names.
		if idx > 0 {
			flat[KeyDoIndex] = idx
		}
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
	if _, exists := where[SelTarget]; exists {
		return nil, ex.Newf("target must be top-level, not inside where")
	}
	if _, exists := where[SelVersion]; exists {
		return nil, ex.Newf("version must be top-level, not inside where")
	}

	normalized := make(map[string]any)
	for key, value := range where {
		switch key {
		case SelFunc, SelRecv, SelStruct, SelFunctionCall, SelDirective, SelKind, SelIdentifier,
			SelSignature, SelSignatureContains, SelResult, SelLastResult, SelParam,
			SelPattern, SelPlacement:
			common[key] = value
		case WhereFile:
			if _, ok := value.(map[string]any); !ok {
				return nil, ex.Newf("where.file must be a map")
			}
			normalized[key] = value
		case CombAllOf, CombOneOf, CombNot:
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
	// The len-1 map check above guarantees the loop body runs exactly once
	// and returns; reaching this point would mean the runtime broke the map
	// iteration contract.
	util.ShouldNotReachHere()
	return nil, nil
}
