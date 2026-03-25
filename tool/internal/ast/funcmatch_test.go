// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

// makeFuncDecl builds a minimal *dst.FuncDecl for testing.
func makeFuncDecl(params, results []*dst.Field) *dst.FuncDecl {
	ft := &dst.FuncType{}
	if len(params) > 0 {
		ft.Params = &dst.FieldList{List: params}
	}
	if len(results) > 0 {
		ft.Results = &dst.FieldList{List: results}
	}
	return &dst.FuncDecl{
		Name: dst.NewIdent("TestFunc"),
		Type: ft,
		Body: &dst.BlockStmt{},
	}
}

func field(typeExpr dst.Expr) *dst.Field {
	return &dst.Field{Type: typeExpr}
}

func strPtr(s string) *string { return &s }

func ident(name string) *dst.Ident { return &dst.Ident{Name: name} }

func selector(pkg, name string) *dst.SelectorExpr {
	return &dst.SelectorExpr{X: ident(pkg), Sel: ident(name)}
}

func TestFuncDeclMatchesFilters_NoFilters(t *testing.T) {
	decl := makeFuncDecl(
		[]*dst.Field{field(ident("string"))},
		[]*dst.Field{field(ident("error"))},
	)
	r := &rule.InstFuncRule{}
	assert.True(t, FuncDeclMatchesFilters(decl, r), "no filters should always match")
}

func TestFuncDeclMatchesFilters_ExactSignature(t *testing.T) {
	// func(string, int) (float32, error)
	decl := makeFuncDecl(
		[]*dst.Field{field(ident("string")), field(ident("int"))},
		[]*dst.Field{field(ident("float32")), field(ident("error"))},
	)

	tests := []struct {
		name string
		sig  rule.FuncSignature
		want bool
	}{
		{
			name: "exact match",
			sig:  rule.FuncSignature{Args: []string{"string", "int"}, Returns: []string{"float32", "error"}},
			want: true,
		},
		{
			name: "wrong arg type",
			sig:  rule.FuncSignature{Args: []string{"int", "string"}, Returns: []string{"float32", "error"}},
			want: false,
		},
		{
			name: "wrong arg count",
			sig:  rule.FuncSignature{Args: []string{"string"}, Returns: []string{"float32", "error"}},
			want: false,
		},
		{
			name: "wrong return type",
			sig:  rule.FuncSignature{Args: []string{"string", "int"}, Returns: []string{"error"}},
			want: false,
		},
		{
			name: "no args filter only checks returns",
			sig:  rule.FuncSignature{Returns: []string{"float32", "error"}},
			want: false, // sig.Args==nil means 0 expected, but decl has 2 params
		},
		{
			name: "args only, no returns check",
			sig:  rule.FuncSignature{Args: []string{"string", "int"}},
			want: false, // sig.Returns==nil means 0 expected, but decl has 2 results
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := tt.sig
			r := &rule.InstFuncRule{Signature: &sig}
			assert.Equal(t, tt.want, FuncDeclMatchesFilters(decl, r))
		})
	}
}

func TestFuncDeclMatchesFilters_SignatureContains(t *testing.T) {
	// func(context.Context, string) error
	decl := makeFuncDecl(
		[]*dst.Field{field(selector("context", "Context")), field(ident("string"))},
		[]*dst.Field{field(ident("error"))},
	)

	tests := []struct {
		name string
		sig  rule.FuncSignature
		want bool
	}{
		{
			name: "arg match triggers true",
			sig:  rule.FuncSignature{Args: []string{"context.Context"}},
			want: true,
		},
		{
			name: "return match triggers true",
			sig:  rule.FuncSignature{Returns: []string{"error"}},
			want: true,
		},
		{
			name: "no match",
			sig:  rule.FuncSignature{Args: []string{"int"}, Returns: []string{"bool"}},
			want: false,
		},
		{
			name: "second arg matches",
			sig:  rule.FuncSignature{Args: []string{"string"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := tt.sig
			r := &rule.InstFuncRule{SignatureContains: &sig}
			assert.Equal(t, tt.want, FuncDeclMatchesFilters(decl, r))
		})
	}
}

func TestFuncDeclMatchesFilters_ResultType(t *testing.T) {
	// func() (io.Reader, error)
	decl := makeFuncDecl(
		nil,
		[]*dst.Field{field(selector("io", "Reader")), field(ident("error"))},
	)

	assert.True(t, FuncDeclMatchesFilters(decl, &rule.InstFuncRule{ResultType: strPtr("error")}))
	assert.True(t, FuncDeclMatchesFilters(decl, &rule.InstFuncRule{ResultType: strPtr("io.Reader")}))
	assert.False(t, FuncDeclMatchesFilters(decl, &rule.InstFuncRule{ResultType: strPtr("io.Writer")}))
	assert.False(t, FuncDeclMatchesFilters(decl, &rule.InstFuncRule{ResultType: strPtr("string")}))
}

func TestFuncDeclMatchesFilters_LastResultType(t *testing.T) {
	// func() (io.Reader, error)
	decl := makeFuncDecl(
		nil,
		[]*dst.Field{field(selector("io", "Reader")), field(ident("error"))},
	)

	// error is the final result
	assert.True(t, FuncDeclMatchesFilters(decl, &rule.InstFuncRule{LastResultType: strPtr("error")}))
	// io.Reader is NOT the final result
	assert.False(t, FuncDeclMatchesFilters(decl, &rule.InstFuncRule{LastResultType: strPtr("io.Reader")}))
}

func TestFuncDeclMatchesFilters_ArgumentType(t *testing.T) {
	// func(context.Context, string) error
	decl := makeFuncDecl(
		[]*dst.Field{field(selector("context", "Context")), field(ident("string"))},
		[]*dst.Field{field(ident("error"))},
	)

	assert.True(t, FuncDeclMatchesFilters(decl, &rule.InstFuncRule{ArgumentType: strPtr("context.Context")}))
	assert.True(t, FuncDeclMatchesFilters(decl, &rule.InstFuncRule{ArgumentType: strPtr("string")}))
	assert.False(t, FuncDeclMatchesFilters(decl, &rule.InstFuncRule{ArgumentType: strPtr("int")}))
}

func TestFuncDeclMatchesFilters_CombinedFilters(t *testing.T) {
	// func(context.Context, string) (io.Reader, error)
	decl := makeFuncDecl(
		[]*dst.Field{field(selector("context", "Context")), field(ident("string"))},
		[]*dst.Field{field(selector("io", "Reader")), field(ident("error"))},
	)

	// All filters match → true
	sig := rule.FuncSignature{Args: []string{"context.Context", "string"}, Returns: []string{"io.Reader", "error"}}
	r := &rule.InstFuncRule{
		Signature:      &sig,
		ResultType:     strPtr("error"),
		LastResultType: strPtr("error"),
		ArgumentType:   strPtr("context.Context"),
	}
	assert.True(t, FuncDeclMatchesFilters(decl, r))

	// Signature matches but ArgumentType doesn't → false
	r2 := &rule.InstFuncRule{
		Signature:    &sig,
		ArgumentType: strPtr("int"),
	}
	assert.False(t, FuncDeclMatchesFilters(decl, r2))
}

func TestFuncDeclMatchesFilters_NoParams(t *testing.T) {
	// func() error
	decl := makeFuncDecl(nil, []*dst.Field{field(ident("error"))})

	// Empty signature matches
	r := &rule.InstFuncRule{Signature: &rule.FuncSignature{Returns: []string{"error"}}}
	assert.True(t, FuncDeclMatchesFilters(decl, r))

	// Non-empty args don't match
	r2 := &rule.InstFuncRule{Signature: &rule.FuncSignature{Args: []string{"string"}, Returns: []string{"error"}}}
	assert.False(t, FuncDeclMatchesFilters(decl, r2))
}
