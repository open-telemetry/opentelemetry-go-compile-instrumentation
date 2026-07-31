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
			name: "minimal valid rule",
			yaml: `
target: main
raw: 'println("Hello, World!")'
`,
			ruleName: "inject_greeting",
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "inject_greeting", r.Name)
				assert.Equal(t, "main", r.Target)
				assert.Equal(t, `println("Hello, World!")`, r.Raw)
				assert.Empty(t, r.Pattern)
				assert.Empty(t, r.Placement)
			},
		},
		{
			name: "valid rule with all fields",
			yaml: `
target: main
func: Bar
recv: "*Recv"
raw: 'println("hi")'
pattern: "^foo.*bar$"
placement: after
`,
			ruleName: "inject_after_pattern",
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "Bar", r.Func)
				assert.Equal(t, "*Recv", r.Recv)
				assert.Equal(t, `println("hi")`, r.Raw)
				assert.Equal(t, "^foo.*bar$", r.Pattern)
				assert.Equal(t, "after", r.Placement)
			},
		},
		{
			name: "valid rule with placement before",
			yaml: `
target: main
raw: 'println("hi")'
placement: before
`,
			ruleName: "inject_before",
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "before", r.Placement)
			},
		},
		{
			name: "name from YAML overrides ruleName argument",
			yaml: `
name: yaml_name
target: main
raw: 'println("hi")'
`,
			ruleName: "arg_name",
			check: func(t *testing.T, r *InstRawRule) {
				assert.Equal(t, "yaml_name", r.Name)
			},
		},
		{
			name: "empty raw",
			yaml: `
target: main
raw: ""
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "raw cannot be empty",
		},
		{
			name: "whitespace-only raw",
			yaml: `
target: main
raw: "   "
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "raw cannot be empty",
		},
		{
			name: "invalid regex pattern",
			yaml: `
target: main
raw: 'println("hi")'
pattern: "("
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "invalid regex pattern for raw rule",
		},
		{
			name: "invalid placement value",
			yaml: `
target: main
raw: 'println("hi")'
placement: sideways
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "invalid placement value",
		},
		{
			name:     "invalid yaml",
			yaml:     `{bad yaml [`,
			ruleName: "bad_rule",
			wantErr:  true,
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
