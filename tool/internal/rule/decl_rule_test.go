// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNewInstDeclRule_Valid(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantDecl string
		wantKind string
	}{
		{
			name: "var with assign_value",
			yaml: `
target: mypkg
declaration_of: GlobalVar
decl_kind: var
assign_value: '"replaced"'
`,
			wantDecl: "GlobalVar",
			wantKind: "var",
		},
		{
			name: "empty decl_kind matches any",
			yaml: `
target: mypkg
declaration_of: AnyDecl
`,
			wantDecl: "AnyDecl",
			wantKind: "",
		},
		{
			name: "func kind without assign_value",
			yaml: `
target: mypkg
declaration_of: MyFunc
decl_kind: func
`,
			wantDecl: "MyFunc",
			wantKind: "func",
		},
		{
			name: "const kind with assign_value",
			yaml: `
target: mypkg
declaration_of: MaxRetries
decl_kind: const
assign_value: "10"
`,
			wantDecl: "MaxRetries",
			wantKind: "const",
		},
		{
			name: "type kind without assign_value",
			yaml: `
target: mypkg
declaration_of: MyInterface
decl_kind: type
`,
			wantDecl: "MyInterface",
			wantKind: "type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(tt.yaml), &fields))
			data, _ := yaml.Marshal(fields)
			r, err := NewInstDeclRule(data, "test-rule")
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.Equal(t, "test-rule", r.GetName())
			assert.Equal(t, tt.wantDecl, r.DeclarationOf)
			assert.Equal(t, tt.wantKind, r.DeclKind)
		})
	}
}

func TestNewInstDeclRule_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "empty declaration_of",
			yaml: `
target: mypkg
declaration_of: ""
`,
			wantErr: "declaration_of cannot be empty",
		},
		{
			name: "invalid decl_kind",
			yaml: `
target: mypkg
declaration_of: Foo
decl_kind: method
`,
			wantErr: "decl_kind",
		},
		{
			name: "assign_value with decl_kind func",
			yaml: `
target: mypkg
declaration_of: MyFunc
decl_kind: func
assign_value: "42"
`,
			wantErr: "assign_value is not valid when decl_kind",
		},
		{
			name: "assign_value with decl_kind type",
			yaml: `
target: mypkg
declaration_of: MyType
decl_kind: type
assign_value: "42"
`,
			wantErr: "assign_value is not valid when decl_kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fields map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(tt.yaml), &fields))
			data, _ := yaml.Marshal(fields)
			_, err := NewInstDeclRule(data, "test-rule")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
