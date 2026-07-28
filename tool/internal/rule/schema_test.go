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
	assert.Equal(t, string(SelectorFunc), SelFunc)
	assert.Equal(t, string(SelectorFile), WhereFile)
	assert.Equal(t, string(SelectorAllOf), CombAllOf)
	assert.Equal(t, string(ModifierInjectHooks), "inject_hooks")
	assert.Equal(t, string(ModifierInjectCode), "inject_code")
	assert.Equal(t, string(ModifierAddStructFields), "add_struct_fields")
	assert.Equal(t, string(ModifierAddFile), "add_file")
	assert.Equal(t, string(ModifierWrapCall), "wrap_call")
	assert.Equal(t, string(ModifierExpandDirective), "expand_directive")
	assert.Equal(t, string(ModifierAssignValue), "assign_value")
}
