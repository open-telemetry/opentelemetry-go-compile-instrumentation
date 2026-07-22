// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseRules_Errors covers the load-time rejections that guard rule files.
// These replace the validation cases the deleted normalize_test.go held; they
// now run against the single ParseRules entry point.
func TestParseRules_Errors(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		errContains string
	}{
		{
			name:        "target inside where",
			yaml:        "r:\n  where:\n    target: net/http\n    func: F\n  do:\n    - inject_code:\n        raw: x\n",
			errContains: "target must be top-level",
		},
		{
			name:        "version inside where",
			yaml:        "r:\n  target: main\n  where:\n    version: v1.0.0\n    func: F\n  do:\n    - inject_code:\n        raw: x\n",
			errContains: "version must be top-level",
		},
		{
			name:        "unsupported where key",
			yaml:        "r:\n  target: main\n  where:\n    bogus: v\n  do:\n    - inject_code:\n        raw: x\n",
			errContains: "unsupported where key",
		},
		{
			name:        "where not a map",
			yaml:        "r:\n  target: main\n  where: nope\n  do:\n    - inject_code:\n        raw: x\n",
			errContains: "where must be a map",
		},
		{
			name:        "missing do",
			yaml:        "r:\n  target: main\n  where:\n    func: F\n",
			errContains: "missing do",
		},
		{
			name:        "empty do sequence",
			yaml:        "r:\n  target: main\n  do: []\n",
			errContains: "do must not be empty",
		},
		{
			name:        "do scalar",
			yaml:        "r:\n  target: main\n  do: 42\n",
			errContains: "do must be a single-key map",
		},
		{
			name:        "do sequence item with two modifiers",
			yaml:        "r:\n  target: main\n  do:\n    - inject_hooks:\n        before: B\n        path: p\n      inject_code:\n        raw: x\n",
			errContains: "exactly one modifier",
		},
		{
			name:        "do map with two modifiers",
			yaml:        "r:\n  target: main\n  do:\n    inject_hooks:\n      before: B\n      path: p\n    inject_code:\n      raw: x\n",
			errContains: "exactly one modifier",
		},
		{
			name:        "unknown modifier",
			yaml:        "r:\n  target: main\n  do:\n    - frobnicate:\n        foo: bar\n",
			errContains: "exactly one modifier",
		},
		{
			name:        "empty target",
			yaml:        "r:\n  target: \"  \"\n  do:\n    - inject_code:\n        raw: x\n",
			errContains: "empty target",
		},
		{
			name:        "missing target",
			yaml:        "r:\n  do:\n    - inject_code:\n        raw: x\n",
			errContains: "empty target",
		},
		{
			name:        "invalid glob target",
			yaml:        "r:\n  target: example.com/[svc\n  do:\n    - inject_code:\n        raw: x\n",
			errContains: "glob",
		},
		{
			name:        "rule body not a mapping",
			yaml:        "r: scalar\n",
			errContains: "must be a mapping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRules([]byte(tt.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

// TestParseRules_DuplicateKeyRejected pins the parity with the previous map
// decode: a rule body with a duplicate top-level key fails loudly rather than
// silently taking the last value.
func TestParseRules_DuplicateKeyRejected(t *testing.T) {
	yaml := "r:\n  target: a\n  target: b\n  do:\n    - inject_code:\n        raw: x\n"
	_, err := ParseRules([]byte(yaml))
	require.Error(t, err)
}

// TestParseRules_DoSequenceOrder verifies a do sequence expands to one rule per
// modifier, in declaration order, with the type selected by the modifier name.
func TestParseRules_DoSequenceOrder(t *testing.T) {
	yaml := "combo:\n" +
		"  target: main\n" +
		"  where:\n" +
		"    func: Example\n" +
		"  do:\n" +
		"    - inject_hooks:\n" +
		"        before: BeforeExample\n" +
		"        path: example.com/hooks\n" +
		"    - inject_code:\n" +
		"        raw: \"_ = 1\"\n"

	rules, err := ParseRules([]byte(yaml))
	require.NoError(t, err)
	require.Len(t, rules, 2)

	fr, ok := rules[0].(*InstFuncRule)
	require.True(t, ok, "first rule should be a func rule from inject_hooks, got %T", rules[0])
	assert.Equal(t, "Example", fr.Func)
	assert.Equal(t, "BeforeExample", fr.Before)
	assert.Equal(t, "combo", fr.Name)

	rr, ok := rules[1].(*InstRawRule)
	require.True(t, ok, "second rule should be a raw rule from inject_code, got %T", rules[1])
	assert.Equal(t, "Example", rr.Func) // where selector reaches the concrete rule
	assert.Equal(t, "_ = 1", rr.Raw)
}

// TestParseRules_DoMapSugar verifies the single-modifier map form.
func TestParseRules_DoMapSugar(t *testing.T) {
	yaml := "r:\n  target: main\n  where:\n    struct: T\n  do:\n    add_struct_fields:\n      new_field:\n        - name: F\n          type: string\n"
	rules, err := ParseRules([]byte(yaml))
	require.NoError(t, err)
	require.Len(t, rules, 1)
	sr, ok := rules[0].(*InstStructRule)
	require.True(t, ok, "got %T", rules[0])
	assert.Equal(t, "T", sr.Struct)
	require.Len(t, sr.NewField, 1)
	assert.Equal(t, "F", sr.NewField[0].Name)
}

// TestParseRules_WhereFileReachesBaseButSelectorsDoNot verifies the where.file
// predicate is routed to base.Where for the setup Filter, while point selectors
// are decoded onto the concrete rule and kept out of base.Where (so filter.Build
// is never fed an unsupported selector).
func TestParseRules_WhereFileReachesBaseButSelectorsDoNot(t *testing.T) {
	yaml := "r:\n  target: main\n  where:\n    func: Open\n    file:\n      has_func: init\n  do:\n    - inject_hooks:\n        before: BeforeOpen\n        path: p\n"
	rules, err := ParseRules([]byte(yaml))
	require.NoError(t, err)
	require.Len(t, rules, 1)
	fr := rules[0].(*InstFuncRule)

	assert.Equal(t, "Open", fr.Func, "point selector must reach the concrete rule")

	where := fr.GetWhere()
	require.NotNil(t, where)
	require.NotNil(t, where.File)
	assert.Equal(t, "init", where.File.HasFunc)
	assert.Empty(t, where.Func, "point selectors must not leak into base.Where")
}

// TestParseRules_NameFieldOverridesEntryKey verifies an explicit name wins over
// the YAML entry key, and the key is used otherwise.
func TestParseRules_NameFieldOverridesEntryKey(t *testing.T) {
	explicit := "entry_key:\n  name: explicit\n  target: main\n  where:\n    func: F\n  do:\n    - inject_hooks:\n        before: B\n        path: p\n"
	rules, err := ParseRules([]byte(explicit))
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, "explicit", rules[0].GetName())

	implicit := "entry_key:\n  target: main\n  where:\n    func: F\n  do:\n    - inject_hooks:\n        before: B\n        path: p\n"
	rules, err = ParseRules([]byte(implicit))
	require.NoError(t, err)
	require.Len(t, rules, 1)
	assert.Equal(t, "entry_key", rules[0].GetName())
}
