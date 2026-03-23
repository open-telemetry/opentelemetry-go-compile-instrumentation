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
			name: "rule with result_implements",
			yaml: `
func: MyFunc
target: example.com/pkg
before: MyBefore
result_implements: error
`,
			check: func(t *testing.T, r *InstFuncRule) {
				assert.Equal(t, "error", r.ResultImplements)
			},
		},
		{
			name: "rule with final_result_implements",
			yaml: `
func: MyFunc
target: example.com/pkg
before: MyBefore
final_result_implements: error
`,
			check: func(t *testing.T, r *InstFuncRule) {
				assert.Equal(t, "error", r.FinalResultImplements)
			},
		},
		{
			name: "rule with argument_implements",
			yaml: `
func: MyFunc
target: example.com/pkg
before: MyBefore
argument_implements: context.Context
`,
			check: func(t *testing.T, r *InstFuncRule) {
				assert.Equal(t, "context.Context", r.ArgumentImplements)
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
result_implements: error
final_result_implements: error
argument_implements: string
`,
			check: func(t *testing.T, r *InstFuncRule) {
				require.NotNil(t, r.Signature)
				require.NotNil(t, r.SignatureContains)
				assert.Equal(t, "error", r.ResultImplements)
				assert.Equal(t, "error", r.FinalResultImplements)
				assert.Equal(t, "string", r.ArgumentImplements)
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
