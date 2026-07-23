// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstStructRule(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		ruleName    string
		wantErr     bool
		errContains string
		check       func(*testing.T, *InstStructRule)
	}{
		{
			name: "valid rule",
			yaml: `
target: example.com/pkg
struct: MyStruct
new_field:
  - name: NewField
    type: string
`,
			ruleName: "add_field",
			check: func(t *testing.T, r *InstStructRule) {
				assert.Equal(t, "add_field", r.Name)
				assert.Equal(t, "example.com/pkg", r.Target)
				assert.Equal(t, "MyStruct", r.Struct)
				require.Len(t, r.NewField, 1)
				assert.Equal(t, "NewField", r.NewField[0].Name)
				assert.Equal(t, "string", r.NewField[0].Type)
			},
		},
		{
			name: "valid rule without new_field",
			yaml: `
target: example.com/pkg
struct: MyStruct
`,
			ruleName: "bare_struct",
			check: func(t *testing.T, r *InstStructRule) {
				assert.Equal(t, "MyStruct", r.Struct)
				assert.Empty(t, r.NewField)
			},
		},
		{
			name: "name from YAML overrides argument",
			yaml: `
name: yaml_name
target: example.com/pkg
struct: MyStruct
`,
			ruleName: "arg_name",
			check: func(t *testing.T, r *InstStructRule) {
				assert.Equal(t, "yaml_name", r.Name)
			},
		},
		{
			name: "struct field empty",
			yaml: `
target: example.com/pkg
struct: ""
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "struct cannot be empty",
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
struct: MyStruct
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "target cannot be empty",
		},
		{
			name: "whitespace-only target",
			yaml: `
target: "   "
struct: MyStruct
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "target cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewInstStructRule([]byte(tt.yaml), tt.ruleName)
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
