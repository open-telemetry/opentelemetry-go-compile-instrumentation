// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

// Selector is a where-clause key recognized by the structured rule schema
// (ADR-0003). Point selectors are hoisted to flat fields by Normalize; file
// and combinator keys stay nested under where.
type Selector string

// Modifier is a do-clause key that names the instrumentation action and,
// per docs/rules.md, declares the rule type.
type Modifier string

// Selector keys in the structured rule schema (where selectors, file/combinator keys, plus top-level target/version).
const (
	SelectorTarget            Selector = "target"
	SelectorVersion           Selector = "version"
	SelectorFunc              Selector = "func"
	SelectorRecv              Selector = "recv"
	SelectorStruct            Selector = "struct"
	SelectorFunctionCall      Selector = "function_call"
	SelectorDirective         Selector = "directive"
	SelectorKind              Selector = "kind"
	SelectorIdentifier        Selector = "identifier"
	SelectorSignature         Selector = "signature"
	SelectorSignatureContains Selector = "signature_contains"
	SelectorResult            Selector = "result"
	SelectorLastResult        Selector = "last_result"
	SelectorParam             Selector = "param"
	SelectorPattern           Selector = "pattern"
	SelectorPlacement         Selector = "placement"
	SelectorFile              Selector = "file"
	SelectorAllOf             Selector = "all-of"
	SelectorOneOf             Selector = "one-of"
	SelectorNot               Selector = "not"
)

// do modifiers (docs/rules.md § modifier names → rule types).
const (
	ModifierInjectHooks     Modifier = "inject_hooks"
	ModifierInjectCode      Modifier = "inject_code"
	ModifierAddStructFields Modifier = "add_struct_fields"
	ModifierAddFile         Modifier = "add_file"
	ModifierWrapCall        Modifier = "wrap_call"
	ModifierExpandDirective Modifier = "expand_directive"
	ModifierAssignValue     Modifier = "assign_value"
)

// String forms used as YAML / map keys. Existing call sites (normalize,
// match) keep using these aliases so the registry is a single source of truth
// without forcing every consumer onto the typed Selector/Modifier APIs.
const (
	SelTarget            = string(SelectorTarget)
	SelVersion           = string(SelectorVersion)
	SelFunc              = string(SelectorFunc)
	SelRecv              = string(SelectorRecv)
	SelStruct            = string(SelectorStruct)
	SelFunctionCall      = string(SelectorFunctionCall)
	SelDirective         = string(SelectorDirective)
	SelKind              = string(SelectorKind)
	SelIdentifier        = string(SelectorIdentifier)
	SelSignature         = string(SelectorSignature)
	SelSignatureContains = string(SelectorSignatureContains)
	SelResult            = string(SelectorResult)
	SelLastResult        = string(SelectorLastResult)
	SelParam             = string(SelectorParam)
	SelPattern           = string(SelectorPattern)
	SelPlacement         = string(SelectorPlacement)

	WhereFile = string(SelectorFile)
	CombAllOf = string(SelectorAllOf)
	CombOneOf = string(SelectorOneOf)
	CombNot   = string(SelectorNot)

	// RawField is the modifier-output key produced by normalize for raw rules
	// (inject_code payload). It is not a where selector; tool/internal/setup/match.go shares it.
	RawField = "raw"
)

// Structured top-level keys.
const (
	KeyWhere = "where"
	KeyDo    = "do"
)

// selectors lists every key that may appear inside where (point selectors,
// file group, and combinators). target/version are intentionally included so
// IsValidSelector can distinguish "known but misplaced" from "unknown";
// normalizeWhere still rejects them inside where.

//nolint:gochecknoglobals // lookup set derived from AllSelectors(); intentionally package-level so IsValidSelector can query it without reconstruction
var selectors = toSet(AllSelectors())

// toSet builds a lookup set from an ordered slice, so the set can never
// drift from the slice it is derived from.
func toSet[T comparable](items []T) map[T]struct{} {
	set := make(map[T]struct{}, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}
	return set
}

//nolint:gochecknoglobals // lookup set derived from AllModifiers(); intentionally package-level so IsValidModifier can query it without reconstruction
var modifiers = toSet(AllModifiers())

// IsValidSelector reports whether s is part of the rule schema's selector
// vocabulary. This includes target/version, which are valid selectors but
// are rejected specifically when used inside a where block — see
// normalizeWhere.
func IsValidSelector(s string) bool {
	_, ok := selectors[Selector(s)]
	return ok
}

// IsValidModifier reports whether s is a known do-clause modifier name.
func IsValidModifier(s string) bool {
	_, ok := modifiers[Modifier(s)]
	return ok
}

// AllSelectors returns every registered where selector in a stable order.
func AllSelectors() []Selector {
	return []Selector{
		SelectorTarget,
		SelectorVersion,
		SelectorFunc,
		SelectorRecv,
		SelectorStruct,
		SelectorFunctionCall,
		SelectorDirective,
		SelectorKind,
		SelectorIdentifier,
		SelectorSignature,
		SelectorSignatureContains,
		SelectorResult,
		SelectorLastResult,
		SelectorParam,
		SelectorPattern,
		SelectorPlacement,
		SelectorFile,
		SelectorAllOf,
		SelectorOneOf,
		SelectorNot,
	}
}

// AllModifiers returns every registered do modifier in a stable order.
func AllModifiers() []Modifier {
	return []Modifier{
		ModifierInjectHooks,
		ModifierInjectCode,
		ModifierAddStructFields,
		ModifierAddFile,
		ModifierWrapCall,
		ModifierExpandDirective,
		ModifierAssignValue,
	}
}
