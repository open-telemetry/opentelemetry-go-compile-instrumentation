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
		ruleID      string
		wantErr     bool
		errContains string
		check       func(*testing.T, *InstStructRule)
	}{
		{
			name:   "minimal valid rule",
			ruleID: "struct1",
			yaml: `
target: main
struct: MyStruct
new_field:
  - name: NewField
    type: string
`,
			check: func(t *testing.T, r *InstStructRule) {
				assert.Equal(t, "struct1", r.Name)
				assert.Equal(t, "main", r.Target.String())
				assert.Equal(t, "MyStruct", r.Struct)
				require.Len(t, r.NewField, 1)
				assert.Equal(t, "NewField", r.NewField[0].Name)
				assert.Equal(t, "string", r.NewField[0].Type)
			},
		},
		{
			name:   "explicit name is preserved",
			ruleID: "fallback",
			yaml: `
name: explicit
target: main
struct: MyStruct
`,
			check: func(t *testing.T, r *InstStructRule) {
				assert.Equal(t, "explicit", r.Name)
			},
		},
		{
			name:   "multiple new fields",
			ruleID: "struct2",
			yaml: `
target: main
struct: MyStruct
new_field:
  - name: A
    type: int
  - name: B
    type: bool
`,
			check: func(t *testing.T, r *InstStructRule) {
				require.Len(t, r.NewField, 2)
				assert.Equal(t, "A", r.NewField[0].Name)
				assert.Equal(t, "B", r.NewField[1].Name)
			},
		},
		{
			name:    "empty struct is rejected",
			ruleID:  "struct3",
			yaml:    "target: main\nstruct: \"  \"",
			wantErr: true,
		},
		{
			name:    "missing struct is rejected",
			ruleID:  "struct4",
			yaml:    "target: main",
			wantErr: true,
		},
		{
			name:    "malformed yaml is rejected",
			ruleID:  "struct5",
			yaml:    "target: main\n  bad: [unterminated",
			wantErr: true,
		},
		{
			name:   "null new_field entry is rejected",
			ruleID: "struct_null",
			yaml: `
target: main
struct: MyStruct
new_field:
  - null
`,
			wantErr:     true,
			errContains: "new_field[0] cannot be empty",
		},
		{
			name:   "empty new_field name is rejected",
			ruleID: "struct_empty_name",
			yaml: `
target: main
struct: MyStruct
new_field:
  - name: ""
    type: string
`,
			wantErr:     true,
			errContains: "new_field[0].name cannot be empty",
		},
		{
			name:   "empty new_field type is rejected",
			ruleID: "struct_empty_type",
			yaml: `
target: main
struct: MyStruct
new_field:
  - name: Valid
    type: ""
`,
			wantErr:     true,
			errContains: "new_field[0].type cannot be empty",
		},
		{
			name:   "duplicate new_field is rejected",
			ruleID: "struct_dup",
			yaml: `
target: main
struct: MyStruct
new_field:
  - name: A
    type: string
  - name: A
    type: int
`,
			wantErr:     true,
			errContains: `new_field "A" is declared more than once`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewInstStructRule([]byte(tt.yaml), tt.ruleID)
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
