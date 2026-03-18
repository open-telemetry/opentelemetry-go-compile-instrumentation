// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"log/slog"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func newTestPhase() *InstrumentPhase { return &InstrumentPhase{logger: slog.Default()} }

func TestParseValueExpr_Bool(t *testing.T) {
	expr, err := parseValueExpr("true")
	require.NoError(t, err)
	ident, ok := expr.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "true", ident.Name)
}

func TestParseValueExpr_StringLiteral(t *testing.T) {
	expr, err := parseValueExpr(`"hello"`)
	require.NoError(t, err)
	lit, ok := expr.(*dst.BasicLit)
	require.True(t, ok)
	assert.Equal(t, token.STRING, lit.Kind)
	assert.Equal(t, `"hello"`, lit.Value)
}

func TestParseValueExpr_InvalidSyntax(t *testing.T) {
	_, err := parseValueExpr("func(")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse assign_value expression")
}

// varFile returns a minimal *dst.File containing a single var declaration.
func varFile(name string, value dst.Expr) *dst.File {
	spec := &dst.ValueSpec{
		Names: []*dst.Ident{{Name: name}},
	}
	if value != nil {
		spec.Values = []dst.Expr{value}
	}
	return &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{spec}},
		},
	}
}

func TestApplyDeclRule_VarAssignValue(t *testing.T) {
	file := varFile("GlobalVar", &dst.BasicLit{Kind: token.STRING, Value: `"original"`})
	spec := file.Decls[0].(*dst.GenDecl).Specs[0].(*dst.ValueSpec)

	r := &rule.InstDeclRule{
		InstBaseRule:  rule.InstBaseRule{Name: "test"},
		DeclarationOf: "GlobalVar",
		DeclKind:      "var",
		AssignValue:   `"replaced"`,
	}
	require.NoError(t, newTestPhase().applyDeclRule(context.Background(), r, file))

	require.Len(t, spec.Values, 1)
	lit, ok := spec.Values[0].(*dst.BasicLit)
	require.True(t, ok)
	assert.Equal(t, `"replaced"`, lit.Value)
}

func TestApplyDeclRule_MultiNameVarAssignsAll(t *testing.T) {
	// var a, b string — two names in one ValueSpec; both should receive the value.
	spec := &dst.ValueSpec{
		Names: []*dst.Ident{{Name: "a"}, {Name: "b"}},
		Type:  &dst.Ident{Name: "string"},
	}
	file := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{spec}},
		},
	}

	r := &rule.InstDeclRule{
		InstBaseRule:  rule.InstBaseRule{Name: "test"},
		DeclarationOf: "a",
		DeclKind:      "var",
		AssignValue:   `"x"`,
	}
	require.NoError(t, newTestPhase().applyDeclRule(context.Background(), r, file))

	require.Len(t, spec.Values, 2)
	for i, v := range spec.Values {
		lit, ok := v.(*dst.BasicLit)
		require.Truef(t, ok, "Values[%d] is not a BasicLit", i)
		assert.Equal(t, `"x"`, lit.Value)
	}
}

func TestApplyDeclRule_NoAssignValue_NoOp(t *testing.T) {
	// When AssignValue is empty, the rule is a no-op on the value.
	file := varFile("GlobalVar", &dst.BasicLit{Kind: token.STRING, Value: `"original"`})
	spec := file.Decls[0].(*dst.GenDecl).Specs[0].(*dst.ValueSpec)

	r := &rule.InstDeclRule{
		InstBaseRule:  rule.InstBaseRule{Name: "test"},
		DeclarationOf: "GlobalVar",
		DeclKind:      "var",
	}
	require.NoError(t, newTestPhase().applyDeclRule(context.Background(), r, file))

	// Value unchanged
	require.Len(t, spec.Values, 1)
	lit, ok := spec.Values[0].(*dst.BasicLit)
	require.True(t, ok)
	assert.Equal(t, `"original"`, lit.Value)
}

func TestApplyDeclRule_NotFound(t *testing.T) {
	file := varFile("OtherVar", nil)

	r := &rule.InstDeclRule{
		InstBaseRule:  rule.InstBaseRule{Name: "test"},
		DeclarationOf: "MissingVar",
		DeclKind:      "var",
	}
	err := newTestPhase().applyDeclRule(context.Background(), r, file)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot find declaration")
}

func TestApplyDeclRule_AssignValueOnFuncErrors(t *testing.T) {
	// assign_value is not valid for func declarations; the apply layer rejects it.
	file := &dst.File{
		Decls: []dst.Decl{
			&dst.FuncDecl{
				Name: &dst.Ident{Name: "MyFunc"},
				Type: &dst.FuncType{},
				Body: &dst.BlockStmt{},
			},
		},
	}
	r := &rule.InstDeclRule{
		InstBaseRule:  rule.InstBaseRule{Name: "test"},
		DeclarationOf: "MyFunc",
		DeclKind:      "func",
		AssignValue:   "42",
	}
	err := newTestPhase().applyDeclRule(context.Background(), r, file)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assign_value requires a var or const declaration")
}
