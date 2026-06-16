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

// ruleIdentity builds a func rule the way the setup phase does — marshal the
// flat fields and run them through NewInstFuncRule — then returns its Identity.
// This exercises the real path so the identity is computed exactly as in
// production.
func ruleIdentity(t *testing.T, name string, flat map[string]any) string {
	t.Helper()
	data, err := yaml.Marshal(flat)
	require.NoError(t, err)
	r, err := NewInstFuncRule(data, name)
	require.NoError(t, err)
	return r.Identity()
}

// TestInstFuncRule_Identity pins the content-derived identity used to generate
// trampoline and HookContext names. It must (a) distinguish separate modifiers
// of one do sequence, (b) never collide a rule named "<base>#<n>" with "<base>"
// at do position n (issue #560), (c) collapse genuinely duplicate rules to one
// identity (de-duplication), and (d) include signature filters.
func TestInstFuncRule_Identity(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"target": "main", "func": "Func1", "path": "example.com/h"}
	}

	// (a) Separate modifiers of one do sequence differ by content.
	before := base()
	before["before"] = "H1"
	after := base()
	after["after"] = "H2"
	assert.NotEqual(t, ruleIdentity(t, "multi_hook", before), ruleIdentity(t, "multi_hook", after),
		"distinct do-sequence modifiers must have distinct identities")

	// (b) #560: "my_hook" do[1] vs a rule literally named "my_hook#1" at do[0].
	// Under the old "name#index" string identity these collided; content-derived
	// identities do not, because the rule bodies differ.
	myHookDo1 := base()
	myHookDo1["after"] = "H1After"
	namedClash := base()
	namedClash["before"] = "H2Before"
	namedClash["name"] = "my_hook#1"
	assert.NotEqual(t, ruleIdentity(t, "my_hook", myHookDo1), ruleIdentity(t, "my_hook_1", namedClash),
		"#560: a do-sequence modifier must not collide with a like-named rule")

	// (c) De-duplication: identical content under different names is one identity.
	dupA := base()
	dupA["before"] = "H1"
	dupB := base()
	dupB["before"] = "H1"
	assert.Equal(t, ruleIdentity(t, "alpha", dupA), ruleIdentity(t, "beta", dupB),
		"identical rule content must share an identity regardless of name")

	// (d) Signature filters participate in identity (and exercise the signature
	// branch of Identity).
	sigArgsCtx := map[string]any{"args": []any{"context.Context"}, "returns": []any{"error"}}
	sigArgsStr := map[string]any{"args": []any{"string"}, "returns": []any{"error"}}
	sigA := base()
	sigA["before"] = "H1"
	sigA["signature"] = sigArgsCtx
	sigB := base()
	sigB["before"] = "H1"
	sigB["signature"] = sigArgsStr
	assert.NotEqual(t, ruleIdentity(t, "sig", sigA), ruleIdentity(t, "sig", sigB),
		"rules differing only in signature filter must have distinct identities")
	sigC := base()
	sigC["before"] = "H1"
	sigC["signature"] = map[string]any{"args": []any{"context.Context"}, "returns": []any{"error"}}
	assert.Equal(t, ruleIdentity(t, "sig", sigA), ruleIdentity(t, "sig", sigC),
		"identical signature filters must yield identical identity")
}
