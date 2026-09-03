// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

// selector is a where-clause key recognized by the structured rule schema
// (ADR-0003). Point selectors are hoisted to flat fields by Normalize; file
// and combinator keys stay nested under where.
type selector string

// modifier is a do-clause key that names the instrumentation action and,
// per docs/rules.md, declares the rule type.
type modifier string

// selector keys in the structured rule schema (where selectors, file/combinator keys, plus top-level target/version).
const (
	selectorTarget            selector = "target"
	selectorVersion           selector = "version"
	selectorFunc              selector = "func"
	selectorRecv              selector = "recv"
	selectorStruct            selector = "struct"
	selectorStructLiteral     selector = "struct_literal"
	selectorFunctionCall      selector = "function_call"
	selectorDirective         selector = "directive"
	selectorKind              selector = "kind"
	selectorIdentifier        selector = "identifier"
	selectorSignature         selector = "signature"
	selectorSignatureContains selector = "signature_contains"
	selectorResult            selector = "result"
	selectorLastResult        selector = "last_result"
	selectorParam             selector = "param"
	selectorPattern           selector = "pattern"
	selectorPlacement         selector = "placement"
	selectorFile              selector = "file"
	selectorAllOf             selector = "all-of"
	selectorOneOf             selector = "one-of"
	selectorNot               selector = "not"
)

// do modifiers (docs/rules.md § modifier names → rule types).
const (
	modifierInjectHooks     modifier = "inject_hooks"
	modifierInjectCode      modifier = "inject_code"
	modifierAddStructFields modifier = "add_struct_fields"
	modifierAddFile         modifier = "add_file"
	modifierWrapCall        modifier = "wrap_call"
	modifierExpandDirective modifier = "expand_directive"
	modifierAssignValue     modifier = "assign_value"
	modifierSetFields       modifier = "set_fields"
)

// String forms used as YAML / map keys. Existing call sites (normalize,
// match) keep using these aliases so the registry is a single source of truth
// without forcing every consumer onto the typed selector/modifier APIs.
const (
	selTarget            = string(selectorTarget)
	selVersion           = string(selectorVersion)
	SelFunc              = string(selectorFunc)
	selRecv              = string(selectorRecv)
	SelStruct            = string(selectorStruct)
	SelStructLiteral     = string(selectorStructLiteral)
	SelFunctionCall      = string(selectorFunctionCall)
	SelDirective         = string(selectorDirective)
	selKind              = string(selectorKind)
	SelIdentifier        = string(selectorIdentifier)
	selSignature         = string(selectorSignature)
	selSignatureContains = string(selectorSignatureContains)
	selResult            = string(selectorResult)
	selLastResult        = string(selectorLastResult)
	selParam             = string(selectorParam)
	selPattern           = string(selectorPattern)
	selPlacement         = string(selectorPlacement)

	WhereFile = string(selectorFile)
	combAllOf = string(selectorAllOf)
	combOneOf = string(selectorOneOf)
	combNot   = string(selectorNot)

	// RawField is the modifier-output key produced by normalize for raw rules
	// (inject_code payload). It is not a where selector; tool/internal/setup/match.go shares it.
	RawField = "raw"
)

// Structured top-level keys.
const (
	keyWhere = "where"
	keyDo    = "do"
)

// selectors lists every key that may appear inside where (point selectors,
// file group, and combinators). target/version are intentionally included so
// isValidSelector can distinguish "known but misplaced" from "unknown";
// normalizeWhere still rejects them inside where.

//nolint:gochecknoglobals // lookup set derived from allSelectors(); intentionally package-level so isValidSelector can query it without reconstruction
var selectors = toSet(allSelectors())

// toSet builds a lookup set from an ordered slice, so the set can never
// drift from the slice it is derived from.
func toSet[T comparable](items []T) map[T]struct{} {
	set := make(map[T]struct{}, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}
	return set
}

//nolint:gochecknoglobals // lookup set derived from allModifiers(); intentionally package-level so isValidModifier can query it without reconstruction
var modifiers = toSet(allModifiers())

// isValidSelector reports whether s is part of the rule schema's selector
// vocabulary. This includes target/version, which are valid selectors but
// are rejected specifically when used inside a where block — see
// normalizeWhere.
func isValidSelector(s string) bool {
	_, ok := selectors[selector(s)]
	return ok
}

// isValidModifier reports whether s is a known do-clause modifier name.
func isValidModifier(s string) bool {
	_, ok := modifiers[modifier(s)]
	return ok
}

// allSelectors returns every registered where selector in a stable order.
func allSelectors() []selector {
	return []selector{
		selectorTarget,
		selectorVersion,
		selectorFunc,
		selectorRecv,
		selectorStruct,
		selectorStructLiteral,
		selectorFunctionCall,
		selectorDirective,
		selectorKind,
		selectorIdentifier,
		selectorSignature,
		selectorSignatureContains,
		selectorResult,
		selectorLastResult,
		selectorParam,
		selectorPattern,
		selectorPlacement,
		selectorFile,
		selectorAllOf,
		selectorOneOf,
		selectorNot,
	}
}

// allModifiers returns every registered do modifier in a stable order.
func allModifiers() []modifier {
	return []modifier{
		modifierInjectHooks,
		modifierInjectCode,
		modifierAddStructFields,
		modifierAddFile,
		modifierWrapCall,
		modifierExpandDirective,
		modifierAssignValue,
		modifierSetFields,
	}
}
