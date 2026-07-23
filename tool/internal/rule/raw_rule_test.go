// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstRawRule(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		ruleName    string
		wantErr     bool
		errContains string
		check       func(*testing.T, *InstRawRule)
	}{
		{
			name: "valid rule with before placement",
			yaml: `
target: example.com/pkg
func: MyFunc
raw: "println(\"hello\")"
placement: before
`,
			ruleName: "inject_code",
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "inject_code", r.Name)
				assert.Equal(t, "example.com/pkg", r.Target)
				assert.Equal(t, "MyFunc", r.Func)
				assert.Equal(t, "before", r.Placement)
			},
		},
		{
			name: "valid rule with after placement",
			yaml: `
target: example.com/pkg
func: MyFunc
raw: "println(\"bye\")"
placement: after
`,
			ruleName: "inject_after",
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "after", r.Placement)
			},
		},
		{
			name: "valid rule with regex pattern",
			yaml: `
target: example.com/pkg
func: MyFunc
raw: "println(\"injected\")"
pattern: "^name := getName\\(\\)$"
`,
			ruleName: "inject_with_pattern",
			check: func(t *testing.T, r *InstRawRule) {
				assert.NotEmpty(t, r.Pattern)
			},
		},
		{
			name: "name from YAML overrides argument",
			yaml: `
name: yaml_name
target: example.com/pkg
func: MyFunc
raw: "println(\"hello\")"
`,
			ruleName: "arg_name",
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "yaml_name", r.Name)
			},
		},
		{
			name: "raw field empty",
			yaml: `
target: example.com/pkg
func: MyFunc
raw: ""
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "raw cannot be empty",
		},
		{
			name: "invalid placement value",
			yaml: `
target: example.com/pkg
func: MyFunc
raw: "println(\"hello\")"
placement: middle
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "invalid placement value",
		},
		{
			name: "invalid regex pattern",
			yaml: `
target: example.com/pkg
func: MyFunc
raw: "println(\"hello\")"
pattern: "[invalid"
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "invalid regex pattern",
		},
		{
			name:     "invalid yaml",
			yaml:     `{bad yaml [`,
			ruleName: "bad_rule",
			wantErr:  true,
		},
		{
			name: "empty target",
			yaml: `
target: ""
func: MyFunc
raw: "println(\"hello\")"
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "target cannot be empty",
		},
		{
			name: "whitespace-only target",
			yaml: `
target: "   "
func: MyFunc
raw: "println(\"hello\")"
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "target cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewInstRawRule([]byte(tt.yaml), tt.ruleName)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
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
