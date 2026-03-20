// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewInstValueDeclRule_Valid(t *testing.T) {
	tests := []struct {
		name           string
		yaml           string
		wantImportPath string
		wantTypeIdent  string
		wantPointer    bool
	}{
		{
			name: "built-in type bool",
			yaml: `
target: mypkg
value_declaration: "bool"
assign_value: "true"
`,
			wantImportPath: "",
			wantTypeIdent:  "bool",
			wantPointer:    false,
		},
		{
			name: "built-in type string",
			yaml: `
target: mypkg
value_declaration: "string"
assign_value: '"hello"'
`,
			wantImportPath: "",
			wantTypeIdent:  "string",
			wantPointer:    false,
		},
		{
			name: "qualified type",
			yaml: `
target: mypkg
value_declaration: "net/http.Client"
assign_value: "http.Client{}"
`,
			wantImportPath: "net/http",
			wantTypeIdent:  "Client",
			wantPointer:    false,
		},
		{
			name: "pointer to qualified type",
			yaml: `
target: mypkg
value_declaration: "*net/http.Request"
assign_value: "nil"
`,
			wantImportPath: "net/http",
			wantTypeIdent:  "Request",
			wantPointer:    true,
		},
		{
			name: "pointer to built-in type",
			yaml: `
target: mypkg
value_declaration: "*int"
assign_value: "nil"
`,
			wantImportPath: "",
			wantTypeIdent:  "int",
			wantPointer:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(tt.yaml), &fields))
			data, _ := yaml.Marshal(fields)
			r, err := NewInstValueDeclRule(data, "test-rule")
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.Equal(t, "test-rule", r.GetName())
			assert.Equal(t, tt.wantImportPath, r.TypeImportPath)
			assert.Equal(t, tt.wantTypeIdent, r.TypeIdent)
			assert.Equal(t, tt.wantPointer, r.TypePointer)
		})
	}
}

func TestNewInstValueDeclRule_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "empty value_declaration",
			yaml: `
target: mypkg
value_declaration: ""
assign_value: "true"
`,
			wantErr: "value_declaration cannot be empty",
		},
		{
			name: "empty assign_value",
			yaml: `
target: mypkg
value_declaration: "bool"
assign_value: ""
`,
			wantErr: "assign_value cannot be empty",
		},
		{
			name: "malformed type starts with digit",
			yaml: `
target: mypkg
value_declaration: "123bad"
assign_value: "true"
`,
			wantErr: "invalid value_declaration format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(tt.yaml), &fields))
			data, _ := yaml.Marshal(fields)
			_, err := NewInstValueDeclRule(data, "test-rule")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestInstValueDeclRule_UnmarshalJSON(t *testing.T) {
	original := &InstValueDeclRule{
		InstBaseRule:     InstBaseRule{Name: "my-rule", Target: "mypkg"},
		ValueDeclaration: "net/http.Client",
		AssignValue:      "http.Client{}",
		TypeImportPath:   "net/http",
		TypeIdent:        "Client",
		TypePointer:      false,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal into a new struct without derived fields
	var restored InstValueDeclRule
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, "net/http", restored.TypeImportPath)
	assert.Equal(t, "Client", restored.TypeIdent)
	assert.False(t, restored.TypePointer)
}

func TestInstValueDeclRule_UnmarshalJSON_RepopulatesDerivedFields(t *testing.T) {
	// Simulate JSON with derived fields missing (e.g., older serialization)
	raw := `{"name":"r","target":"t","value_declaration":"*net/http.Request","assign_value":"nil"}`

	var r InstValueDeclRule
	err := json.Unmarshal([]byte(raw), &r)
	require.NoError(t, err)

	assert.Equal(t, "net/http", r.TypeImportPath)
	assert.Equal(t, "Request", r.TypeIdent)
	assert.True(t, r.TypePointer)
}
