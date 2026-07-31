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
			name: "minimal valid rule without fields",
			yaml: `
target: main
struct: MyStruct
`,
			ruleName: "instrument_mystruct",
			check: func(t *testing.T, r *InstStructRule) {
				assert.Equal(t, "instrument_mystruct", r.Name)
				assert.Equal(t, "main", r.Target)
				assert.Equal(t, "MyStruct", r.Struct)
				assert.Empty(t, r.NewField)
			},
		},
		{
			name: "valid rule with one new field",
			yaml: `
target: main
struct: MyStruct
new_field:
  - name: TraceID
    type: string
`,
			ruleName: "add_trace_id",
			check: func(t *testing.T, r *InstStructRule) {
				require.Len(t, r.NewField, 1)
				assert.Equal(t, "TraceID", r.NewField[0].Name)
				assert.Equal(t, "string", r.NewField[0].Type)
			},
		},
		{
			name: "valid rule with multiple new fields",
			yaml: `
target: main
struct: MyStruct
new_field:
  - name: TraceID
    type: string
  - name: SpanCount
    type: int
`,
			ruleName: "add_fields",
			check: func(t *testing.T, r *InstStructRule) {
				require.Len(t, r.NewField, 2)
				assert.Equal(t, "TraceID", r.NewField[0].Name)
				assert.Equal(t, "string", r.NewField[0].Type)
				assert.Equal(t, "SpanCount", r.NewField[1].Name)
				assert.Equal(t, "int", r.NewField[1].Type)
			},
		},
		{
			name: "name from YAML overrides ruleName argument",
			yaml: `
name: yaml_name
target: main
struct: MyStruct
`,
			ruleName: "arg_name",
			check: func(t *testing.T, r *InstStructRule) {
				assert.Equal(t, "yaml_name", r.Name)
			},
		},
		{
			name: "empty struct",
			yaml: `
target: main
struct: ""
`,
			ruleName:    "bad_rule",
			wantErr:     true,
			errContains: "struct cannot be empty",
		},
		{
			name: "whitespace-only struct",
			yaml: `
target: main
struct: "   "
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
