// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTypeName(t *testing.T) {
	tests := []struct {
		input      string
		wantErr    bool
		wantImport string
		wantName   string
		wantPtr    bool
	}{
		{input: "error", wantName: "error"},
		{input: "int", wantName: "int"},
		{input: "float32", wantName: "float32"},
		{input: "any", wantName: "any"},
		{input: "context.Context", wantImport: "context", wantName: "Context"},
		{input: "io.Reader", wantImport: "io", wantName: "Reader"},
		{input: "*http.Request", wantImport: "http", wantName: "Request", wantPtr: true},
		{input: "*T", wantName: "T", wantPtr: true},
		{input: "example.com/pkg.Type", wantImport: "example.com/pkg", wantName: "Type"},
		{input: "", wantErr: true},
		{input: "[]string", wantErr: true},
		{input: "map[string]int", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseTypeName(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantImport, got.importPath)
			assert.Equal(t, tt.wantName, got.name)
			assert.Equal(t, tt.wantPtr, got.pointer)
		})
	}
}

func TestTypeNameMatches(t *testing.T) {
	tests := []struct {
		name    string
		typeStr string
		node    dst.Expr
		want    bool
	}{
		{
			name:    "builtin ident matches",
			typeStr: "error",
			node:    &dst.Ident{Name: "error", Path: ""},
			want:    true,
		},
		{
			name:    "builtin ident mismatch",
			typeStr: "error",
			node:    &dst.Ident{Name: "string", Path: ""},
			want:    false,
		},
		{
			name:    "selector matches",
			typeStr: "context.Context",
			node: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "context", Path: ""},
				Sel: &dst.Ident{Name: "Context"},
			},
			want: true,
		},
		{
			name:    "selector package mismatch",
			typeStr: "io.Reader",
			node: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "context", Path: ""},
				Sel: &dst.Ident{Name: "Reader"},
			},
			want: false,
		},
		{
			name:    "pointer matches",
			typeStr: "*http.Request",
			node: &dst.StarExpr{
				X: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "http", Path: ""},
					Sel: &dst.Ident{Name: "Request"},
				},
			},
			want: true,
		},
		{
			name:    "pointer type does not match non-pointer",
			typeStr: "*T",
			node:    &dst.Ident{Name: "T", Path: ""},
			want:    false,
		},
		{
			name:    "non-pointer does not match pointer",
			typeStr: "T",
			node:    &dst.StarExpr{X: &dst.Ident{Name: "T", Path: ""}},
			want:    false,
		},
		{
			name:    "any matches empty interface",
			typeStr: "any",
			node:    &dst.InterfaceType{Methods: &dst.FieldList{}},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tn, err := parseTypeName(tt.typeStr)
			require.NoError(t, err)
			assert.Equal(t, tt.want, tn.matches(tt.node))
		})
	}
}

func TestFieldListContainsType(t *testing.T) {
	fields := &dst.FieldList{
		List: []*dst.Field{
			{Type: &dst.Ident{Name: "string"}},
			{
				Type: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "context"},
					Sel: &dst.Ident{Name: "Context"},
				},
			},
			{Type: &dst.Ident{Name: "error"}},
		},
	}

	assert.True(t, fieldListContainsType(fields, "string"))
	assert.True(t, fieldListContainsType(fields, "context.Context"))
	assert.True(t, fieldListContainsType(fields, "error"))
	assert.False(t, fieldListContainsType(fields, "int"))
	assert.False(t, fieldListContainsType(fields, "io.Reader"))
	assert.False(t, fieldListContainsType(nil, "error"))
	assert.False(t, fieldListContainsType(&dst.FieldList{}, "error"))
	assert.False(t, fieldListContainsType(fields, "[]invalid"))
}
