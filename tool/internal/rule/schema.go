// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import "slices"

// Selector names the structured where-key selectors accepted by normalizeWhere.
// These keys are hoisted into the flat rule form consumed by the existing rule
// constructors.
type Selector string

// Modifier names the structured do-key modifiers accepted by normalizeDo.
// The current implementation validates these names during normalization but
// still derives the concrete rule type from field presence; see #546.
type Modifier string

// Structured top-level keys.
const (
	KeyWhere = "where"
	KeyDo    = "do"
)

// Package selectors that must stay top-level rather than inside where.
const (
	SelTarget  = "target"
	SelVersion = "version"
)

// Valid structured where selectors.
const (
	SelectorFunc         Selector = "func"
	SelectorRecv         Selector = "recv"
	SelectorStruct       Selector = "struct"
	SelectorFunctionCall Selector = "function_call"
	SelectorDirective    Selector = "directive"
	SelectorKind         Selector = "kind"
	SelectorIdentifier   Selector = "identifier"

	// Signature match-narrowing selectors for func rules (see InstFuncRule).
	SelectorSignature         Selector = "signature"
	SelectorSignatureContains Selector = "signature_contains"
	SelectorResult            Selector = "result"
	SelectorLastResult        Selector = "last_result"
	SelectorParam             Selector = "param"

	// Raw match-narrowing selectors for raw rules (see InstRawRule).
	SelectorPattern   Selector = "pattern"
	SelectorPlacement Selector = "placement"
)

// String aliases preserved for existing call sites.
const (
	SelFunc         = string(SelectorFunc)
	SelRecv         = string(SelectorRecv)
	SelStruct       = string(SelectorStruct)
	SelFunctionCall = string(SelectorFunctionCall)
	SelDirective    = string(SelectorDirective)
	SelKind         = string(SelectorKind)
	SelIdentifier   = string(SelectorIdentifier)

	SelSignature         = string(SelectorSignature)
	SelSignatureContains = string(SelectorSignatureContains)
	SelResult            = string(SelectorResult)
	SelLastResult        = string(SelectorLastResult)
	SelParam             = string(SelectorParam)

	SelPattern   = string(SelectorPattern)
	SelPlacement = string(SelectorPlacement)
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

// Valid structured do modifiers.
const (
	ModifierInjectHooks     Modifier = "inject_hooks"
	ModifierInjectCode      Modifier = "inject_code"
	ModifierAddStructFields Modifier = "add_struct_fields"
	ModifierAddFile         Modifier = "add_file"
	ModifierWrapCall        Modifier = "wrap_call"
	ModifierExpandDirective Modifier = "expand_directive"
	ModifierAssignValue     Modifier = "assign_value"
)

var allSelectors = []Selector{ //nolint:gochecknoglobals // immutable schema registry
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
}

var allModifiers = []Modifier{ //nolint:gochecknoglobals // immutable schema registry
	ModifierInjectHooks,
	ModifierInjectCode,
	ModifierAddStructFields,
	ModifierAddFile,
	ModifierWrapCall,
	ModifierExpandDirective,
	ModifierAssignValue,
}

var validSelectors = map[string]struct{}{ //nolint:gochecknoglobals // immutable schema lookup
	SelFunc:              {},
	SelRecv:              {},
	SelStruct:            {},
	SelFunctionCall:      {},
	SelDirective:         {},
	SelKind:              {},
	SelIdentifier:        {},
	SelSignature:         {},
	SelSignatureContains: {},
	SelResult:            {},
	SelLastResult:        {},
	SelParam:             {},
	SelPattern:           {},
	SelPlacement:         {},
}

var validModifiers = map[string]struct{}{ //nolint:gochecknoglobals // immutable schema lookup
	string(ModifierInjectHooks):     {},
	string(ModifierInjectCode):      {},
	string(ModifierAddStructFields): {},
	string(ModifierAddFile):         {},
	string(ModifierWrapCall):        {},
	string(ModifierExpandDirective): {},
	string(ModifierAssignValue):     {},
}

// IsValidSelector reports whether s is a supported structured where selector.
func IsValidSelector(s string) bool {
	_, ok := validSelectors[s]
	return ok
}

// IsValidModifier reports whether s is a supported structured do modifier.
func IsValidModifier(s string) bool {
	_, ok := validModifiers[s]
	return ok
}

// AllSelectors returns the supported structured where selectors in stable order.
func AllSelectors() []Selector {
	return slices.Clone(allSelectors)
}

// AllModifiers returns the supported structured do modifiers in stable order.
func AllModifiers() []Modifier {
	return slices.Clone(allModifiers)
}

// HasWhereSelectors reports whether where carries any non-file point selectors.
// Top-level where combinators are checked separately by the setup filter.
func HasWhereSelectors(where *WhereDef) bool {
	if where == nil {
		return false
	}

	return where.Func != "" || where.Recv != "" || where.Struct != "" ||
		where.FunctionCall != "" || where.Directive != "" ||
		where.Kind != "" || where.Identifier != ""
}
