// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidSelector(t *testing.T) {
	for _, sel := range allSelectors() {
		assert.Truef(t, isValidSelector(string(sel)), "allSelectors entry %q must be valid", sel)
	}
	assert.False(t, isValidSelector("bogus"))
	assert.False(t, isValidSelector(""))
	assert.False(t, isValidSelector("inject_hooks")) // modifier, not selector
}

func TestIsValidModifier(t *testing.T) {
	for _, mod := range allModifiers() {
		assert.Truef(t, isValidModifier(string(mod)), "allModifiers entry %q must be valid", mod)
	}
	assert.False(t, isValidModifier("bogus"))
	assert.False(t, isValidModifier(""))
	assert.False(t, isValidModifier("inject_hook")) // typo of inject_hooks
	assert.False(t, isValidModifier("func"))        // selector, not modifier
}

func TestAllSelectorsComplete(t *testing.T) {
	got := allSelectors()
	require.Len(t, got, len(selectors))
	seen := make(map[selector]struct{}, len(got))
	for _, sel := range got {
		_, dup := seen[sel]
		assert.Falsef(t, dup, "duplicate selector %q in allSelectors", sel)
		seen[sel] = struct{}{}
		_, registered := selectors[sel]
		assert.Truef(t, registered, "allSelectors entry %q missing from selectors map", sel)
	}
}

func TestAllModifiersComplete(t *testing.T) {
	got := allModifiers()
	require.Len(t, got, len(modifiers))
	seen := make(map[modifier]struct{}, len(got))
	for _, mod := range got {
		_, dup := seen[mod]
		assert.Falsef(t, dup, "duplicate modifier %q in allModifiers", mod)
		seen[mod] = struct{}{}
		_, registered := modifiers[mod]
		assert.Truef(t, registered, "allModifiers entry %q missing from modifiers map", mod)
	}
}

func TestStringAliasesMatchTypedConstants(t *testing.T) {
	selectorCases := []struct {
		alias string
		typed selector
	}{
		{selTarget, selectorTarget},
		{selVersion, selectorVersion},
		{SelFunc, selectorFunc},
		{selRecv, selectorRecv},
		{SelStruct, selectorStruct},
		{SelFunctionCall, selectorFunctionCall},
		{SelDirective, selectorDirective},
		{selKind, selectorKind},
		{SelIdentifier, selectorIdentifier},
		{selSignature, selectorSignature},
		{selSignatureContains, selectorSignatureContains},
		{selResult, selectorResult},
		{selLastResult, selectorLastResult},
		{selParam, selectorParam},
		{selPattern, selectorPattern},
		{selPlacement, selectorPlacement},
		{WhereFile, selectorFile},
		{combAllOf, selectorAllOf},
		{combOneOf, selectorOneOf},
		{combNot, selectorNot},
	}
	for _, c := range selectorCases {
		assert.Equalf(t, string(c.typed), c.alias, "alias must equal string(%v)", c.typed)
	}
	modifierCases := []struct {
		alias string
		typed modifier
	}{
		{"inject_hooks", modifierInjectHooks},
		{"inject_code", modifierInjectCode},
		{"add_struct_fields", modifierAddStructFields},
		{"add_file", modifierAddFile},
		{"wrap_call", modifierWrapCall},
		{"expand_directive", modifierExpandDirective},
		{"assign_value", modifierAssignValue},
	}
	for _, c := range modifierCases {
		assert.Equalf(t, string(c.typed), c.alias, "alias must equal string(%v)", c.typed)
	}
}
