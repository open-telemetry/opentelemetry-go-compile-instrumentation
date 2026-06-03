// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewInstFuncRule(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(*testing.T, *InstFuncRule)
	}{
		{
			name: "minimal valid rule",
			yaml: `
func: MyFunc
target: example.com/pkg
before: MyBefore
`,
			check: func(t *testing.T, r *InstFuncRule) {
				assert.Equal(t, "MyFunc", r.Func)
				assert.Equal(t, "MyBefore", r.Before)
				assert.Nil(t, r.Signature)
			},
		},
		{
			name: "rule with exact signature",
			yaml: `
func: MyFunc
target: example.com/pkg
before: MyBefore
signature:
  args: [context.Context, string]
  returns: [error]
`,
			check: func(t *testing.T, r *InstFuncRule) {
				require.NotNil(t, r.Signature)
				assert.Equal(t, []string{"context.Context", "string"}, r.Signature.Args)
				assert.Equal(t, []string{"error"}, r.Signature.Returns)
			},
		},
		{
			name: "rule with signature_contains",
			yaml: `
func: MyFunc
target: example.com/pkg
after: MyAfter
signature_contains:
  args: [context.Context]
`,
			check: func(t *testing.T, r *InstFuncRule) {
				require.NotNil(t, r.SignatureContains)
				assert.Equal(t, []string{"context.Context"}, r.SignatureContains.Args)
				assert.Nil(t, r.SignatureContains.Returns)
			},
		},
		{
			name: "rule with result",
			yaml: `
func: MyFunc
target: example.com/pkg
before: MyBefore
result: error
`,
			check: func(t *testing.T, r *InstFuncRule) {
				assert.Equal(t, "error", r.Result)
			},
		},
		{
			name: "rule with last_result",
			yaml: `
func: MyFunc
target: example.com/pkg
before: MyBefore
last_result: error
`,
			check: func(t *testing.T, r *InstFuncRule) {
				assert.Equal(t, "error", r.LastResult)
			},
		},
		{
			name: "rule with param",
			yaml: `
func: MyFunc
target: example.com/pkg
before: MyBefore
param: context.Context
`,
			check: func(t *testing.T, r *InstFuncRule) {
				assert.Equal(t, "context.Context", r.Param)
			},
		},
		{
			name: "all signature sub-filters together",
			yaml: `
func: MyFunc
target: example.com/pkg
before: MyBefore
signature:
  args: [string]
  returns: [error]
signature_contains:
  returns: [error]
result: error
last_result: error
param: string
`,
			check: func(t *testing.T, r *InstFuncRule) {
				require.NotNil(t, r.Signature)
				require.NotNil(t, r.SignatureContains)
				assert.Equal(t, "error", r.Result)
				assert.Equal(t, "error", r.LastResult)
				assert.Equal(t, "string", r.Param)
			},
		},
		{
			name:    "missing func field",
			yaml:    `target: example.com/pkg\nbefore: MyBefore`,
			wantErr: true,
		},
		{
			name:    "missing before and after",
			yaml:    `func: MyFunc\ntarget: example.com/pkg`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields map[string]any
			_ = yaml.Unmarshal([]byte(tt.yaml), &fields)
			data, _ := yaml.Marshal(fields)

			r, err := NewInstFuncRule(data, tt.name)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, r)
			if tt.check != nil {
				tt.check(t, r)
			}
		})
	}
}

// TestInstFuncRule_String pins the identity used to derive trampoline and
// HookContext names. Two modifiers of the same do sequence share a rule name
// but must produce distinct identities, otherwise their generated declarations
// collide (issue #544). Index 0 must keep the bare name so single-modifier and
// legacy rules retain their historical generated names.
func TestInstFuncRule_String(t *testing.T) {
	tests := []struct {
		name    string
		doIndex int
		want    string
	}{
		{name: "open_rule", doIndex: 0, want: "open_rule"},
		{name: "open_rule", doIndex: 1, want: "open_rule_1"},
		{name: "open_rule", doIndex: 2, want: "open_rule_2"},
	}
	for _, tt := range tests {
		r := &InstFuncRule{
			InstBaseRule: InstBaseRule{Name: tt.name},
			DoIndex:      tt.doIndex,
		}
		got := r.String()
		assert.Equal(t, tt.want, got, "String() for do_index %d", tt.doIndex)
	}

	// Distinct do indices on the same rule name must never collide.
	first := (&InstFuncRule{InstBaseRule: InstBaseRule{Name: "shared"}, DoIndex: 0}).String()
	second := (&InstFuncRule{InstBaseRule: InstBaseRule{Name: "shared"}, DoIndex: 1}).String()
	assert.NotEqual(t, first, second, "do-sequence modifiers must have distinct identities")
}

// TestNormalize_DoSequenceStampsIndex verifies that Normalize records the
// zero-based do-sequence position on each expanded modifier (index 0 omitted),
// preserving order. This index is what keeps trampoline names unique when
// several modifiers target the same function.
func TestNormalize_DoSequenceStampsIndex(t *testing.T) {
	fields := map[string]any{
		"target": "database/sql",
		"where":  map[string]any{"func": "Open"},
		"do": []any{
			map[string]any{"inject_hooks": map[string]any{"before": "BeforeOpen"}},
			map[string]any{"inject_hooks": map[string]any{"after": "AfterOpen"}},
			map[string]any{"inject_hooks": map[string]any{"after": "AfterOpen2"}},
		},
	}

	got, err := Normalize(fields)
	require.NoError(t, err)
	require.Len(t, got, 3)

	// Index 0 carries no discriminator so legacy names are preserved.
	_, has0 := got[0][KeyDoIndex]
	assert.False(t, has0, "do[0] must not carry a do_index")
	assert.Equal(t, "BeforeOpen", got[0]["before"])

	assert.Equal(t, 1, got[1][KeyDoIndex])
	assert.Equal(t, "AfterOpen", got[1]["after"])

	assert.Equal(t, 2, got[2][KeyDoIndex])
	assert.Equal(t, "AfterOpen2", got[2]["after"])
}
