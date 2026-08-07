// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidSelector(t *testing.T) {
	for _, sel := range AllSelectors() {
		assert.Truef(t, IsValidSelector(string(sel)), "AllSelectors entry %q must be valid", sel)
	}
	assert.False(t, IsValidSelector("bogus"))
	assert.False(t, IsValidSelector(""))
	assert.False(t, IsValidSelector("inject_hooks")) // modifier, not selector
}

func TestIsValidModifier(t *testing.T) {
	for _, mod := range AllModifiers() {
		assert.Truef(t, IsValidModifier(string(mod)), "AllModifiers entry %q must be valid", mod)
	}
	assert.False(t, IsValidModifier("bogus"))
	assert.False(t, IsValidModifier(""))
	assert.False(t, IsValidModifier("inject_hook")) // typo of inject_hooks
	assert.False(t, IsValidModifier("func"))        // selector, not modifier
}

func TestAllSelectorsComplete(t *testing.T) {
	got := AllSelectors()
	require.Len(t, got, len(selectors))
	seen := make(map[Selector]struct{}, len(got))
	for _, sel := range got {
		_, dup := seen[sel]
		assert.Falsef(t, dup, "duplicate selector %q in AllSelectors", sel)
		seen[sel] = struct{}{}
		_, registered := selectors[sel]
		assert.Truef(t, registered, "AllSelectors entry %q missing from selectors map", sel)
	}
}

func TestAllModifiersComplete(t *testing.T) {
	got := AllModifiers()
	require.Len(t, got, len(modifiers))
	seen := make(map[Modifier]struct{}, len(got))
	for _, mod := range got {
		_, dup := seen[mod]
		assert.Falsef(t, dup, "duplicate modifier %q in AllModifiers", mod)
		seen[mod] = struct{}{}
		_, registered := modifiers[mod]
		assert.Truef(t, registered, "AllModifiers entry %q missing from modifiers map", mod)
	}
}

func TestStringAliasesMatchTypedConstants(t *testing.T) {
	selectorCases := []struct {
		alias string
		typed Selector
	}{
		{SelTarget, SelectorTarget},
		{SelVersion, SelectorVersion},
		{SelFunc, SelectorFunc},
		{SelRecv, SelectorRecv},
		{SelStruct, SelectorStruct},
		{SelFunctionCall, SelectorFunctionCall},
		{SelDirective, SelectorDirective},
		{SelKind, SelectorKind},
		{SelIdentifier, SelectorIdentifier},
		{SelSignature, SelectorSignature},
		{SelSignatureContains, SelectorSignatureContains},
		{SelResult, SelectorResult},
		{SelLastResult, SelectorLastResult},
		{SelParam, SelectorParam},
		{SelPattern, SelectorPattern},
		{SelPlacement, SelectorPlacement},
		{WhereFile, SelectorFile},
		{CombAllOf, SelectorAllOf},
		{CombOneOf, SelectorOneOf},
		{CombNot, SelectorNot},
	}
	for _, c := range selectorCases {
		assert.Equalf(t, string(c.typed), c.alias, "alias must equal string(%v)", c.typed)
	}
	modifierCases := []struct {
		alias string
		typed Modifier
	}{
		{"inject_hooks", ModifierInjectHooks},
		{"inject_code", ModifierInjectCode},
		{"add_struct_fields", ModifierAddStructFields},
		{"add_file", ModifierAddFile},
		{"wrap_call", ModifierWrapCall},
		{"expand_directive", ModifierExpandDirective},
		{"assign_value", ModifierAssignValue},
	}
	for _, c := range modifierCases {
		assert.Equalf(t, string(c.typed), c.alias, "alias must equal string(%v)", c.typed)
	}
}
