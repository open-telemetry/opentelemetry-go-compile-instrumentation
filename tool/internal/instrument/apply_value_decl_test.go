// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func TestMatchesType_BuiltIn(t *testing.T) {
	expr := &dst.Ident{Name: "bool"}
	assert.True(t, matchesType(expr, "", "bool", false, nil))
}

func TestMatchesType_BuiltIn_Mismatch(t *testing.T) {
	expr := &dst.Ident{Name: "string"}
	assert.False(t, matchesType(expr, "", "bool", false, nil))
}

func TestMatchesType_BuiltIn_NotIdent(t *testing.T) {
	expr := &dst.StarExpr{X: &dst.Ident{Name: "bool"}}
	assert.False(t, matchesType(expr, "", "bool", false, nil))
}

func TestMatchesType_Qualified_WithPath(t *testing.T) {
	expr := &dst.SelectorExpr{
		X:   &dst.Ident{Name: "http", Path: "net/http"},
		Sel: &dst.Ident{Name: "Client"},
	}
	assert.True(t, matchesType(expr, "net/http", "Client", false, nil))
}

func TestMatchesType_Qualified_WithImportMap(t *testing.T) {
	imports := map[string]string{"http": "net/http"}
	expr := &dst.SelectorExpr{
		X:   &dst.Ident{Name: "http"},
		Sel: &dst.Ident{Name: "Client"},
	}
	assert.True(t, matchesType(expr, "net/http", "Client", false, imports))
}

func TestMatchesType_Qualified_WrongTypeName(t *testing.T) {
	expr := &dst.SelectorExpr{
		X:   &dst.Ident{Name: "http", Path: "net/http"},
		Sel: &dst.Ident{Name: "Response"},
	}
	assert.False(t, matchesType(expr, "net/http", "Client", false, nil))
}

func TestMatchesType_Qualified_WrongPath(t *testing.T) {
	imports := map[string]string{"other": "other/pkg"}
	expr := &dst.SelectorExpr{
		X:   &dst.Ident{Name: "other"},
		Sel: &dst.Ident{Name: "Client"},
	}
	assert.False(t, matchesType(expr, "net/http", "Client", false, imports))
}

func TestMatchesType_Qualified_UnknownAlias(t *testing.T) {
	// Alias not in import map → no match
	expr := &dst.SelectorExpr{
		X:   &dst.Ident{Name: "http"},
		Sel: &dst.Ident{Name: "Client"},
	}
	assert.False(t, matchesType(expr, "net/http", "Client", false, map[string]string{}))
}

func TestMatchesType_Pointer_Match(t *testing.T) {
	expr := &dst.StarExpr{X: &dst.Ident{Name: "bool"}}
	assert.True(t, matchesType(expr, "", "bool", true, nil))
}

func TestMatchesType_Pointer_Mismatch_NotStar(t *testing.T) {
	// Rule expects pointer, expr is not pointer
	expr := &dst.Ident{Name: "bool"}
	assert.False(t, matchesType(expr, "", "bool", true, nil))
}

func TestMatchesType_NonPointerRule_PointerExpr(t *testing.T) {
	// Rule is non-pointer, but expression is a pointer — must not match
	expr := &dst.StarExpr{X: &dst.Ident{Name: "bool"}}
	assert.False(t, matchesType(expr, "", "bool", false, nil))
}

func TestMatchesType_BuiltIn_NonIdentExpr(t *testing.T) {
	// importPath is empty but expr is a SelectorExpr (not an Ident) — must not match
	expr := &dst.SelectorExpr{
		X:   &dst.Ident{Name: "pkg"},
		Sel: &dst.Ident{Name: "bool"},
	}
	assert.False(t, matchesType(expr, "", "bool", false, nil))
}

func TestMatchesType_Pointer_Qualified(t *testing.T) {
	expr := &dst.StarExpr{
		X: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "http", Path: "net/http"},
			Sel: &dst.Ident{Name: "Request"},
		},
	}
	assert.True(t, matchesType(expr, "net/http", "Request", true, nil))
}

func TestApplyValueDeclRule_BuiltInType(t *testing.T) {
	// var enabled bool = false  → should become true
	// var untyped = false       → should be skipped (no type annotation)
	// var count int = 0         → should be skipped (different type)
	enabledSpec := &dst.ValueSpec{
		Names:  []*dst.Ident{{Name: "enabled"}},
		Type:   &dst.Ident{Name: "bool"},
		Values: []dst.Expr{&dst.Ident{Name: "false"}},
	}
	untypedSpec := &dst.ValueSpec{
		Names:  []*dst.Ident{{Name: "untyped"}},
		Values: []dst.Expr{&dst.Ident{Name: "false"}},
	}
	countSpec := &dst.ValueSpec{
		Names:  []*dst.Ident{{Name: "count"}},
		Type:   &dst.Ident{Name: "int"},
		Values: []dst.Expr{&dst.BasicLit{Kind: token.INT, Value: "0"}},
	}
	file := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{enabledSpec, untypedSpec, countSpec}},
		},
	}

	r := &rule.InstValueDeclRule{
		InstBaseRule:     rule.InstBaseRule{Name: "test"},
		ValueDeclaration: "bool",
		AssignValue:      "true",
		TypeIdent:        "bool",
	}
	require.NoError(t, newTestPhase().applyValueDeclRule(context.Background(), r, file))

	// enabled: replaced
	require.Len(t, enabledSpec.Values, 1)
	ident, ok := enabledSpec.Values[0].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "true", ident.Name)

	// untyped: unchanged (no Type annotation)
	require.Len(t, untypedSpec.Values, 1)
	assert.Equal(t, "false", untypedSpec.Values[0].(*dst.Ident).Name)

	// count: unchanged (int, not bool)
	require.Len(t, countSpec.Values, 1)
	assert.Equal(t, "0", countSpec.Values[0].(*dst.BasicLit).Value)
}

func TestApplyValueDeclRule_NoMatch_NoOp(t *testing.T) {
	spec := &dst.ValueSpec{
		Names:  []*dst.Ident{{Name: "count"}},
		Type:   &dst.Ident{Name: "int"},
		Values: []dst.Expr{&dst.BasicLit{Kind: token.INT, Value: "42"}},
	}
	file := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{spec}},
		},
	}

	r := &rule.InstValueDeclRule{
		InstBaseRule:     rule.InstBaseRule{Name: "test"},
		ValueDeclaration: "bool",
		AssignValue:      "true",
		TypeIdent:        "bool",
	}
	require.NoError(t, newTestPhase().applyValueDeclRule(context.Background(), r, file))

	// Unchanged
	require.Len(t, spec.Values, 1)
	assert.Equal(t, "42", spec.Values[0].(*dst.BasicLit).Value)
}

func TestApplyValueDeclRule_MultiNameSpec(t *testing.T) {
	// var a, b bool — both names should get the replacement expression (cloned).
	spec := &dst.ValueSpec{
		Names: []*dst.Ident{{Name: "a"}, {Name: "b"}},
		Type:  &dst.Ident{Name: "bool"},
	}
	file := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{spec}},
		},
	}

	r := &rule.InstValueDeclRule{
		InstBaseRule:     rule.InstBaseRule{Name: "test"},
		ValueDeclaration: "bool",
		AssignValue:      "true",
		TypeIdent:        "bool",
	}
	require.NoError(t, newTestPhase().applyValueDeclRule(context.Background(), r, file))

	require.Len(t, spec.Values, 2)
	for i, v := range spec.Values {
		ident, ok := v.(*dst.Ident)
		require.Truef(t, ok, "Values[%d] is not *dst.Ident", i)
		assert.Equal(t, "true", ident.Name)
	}
}

func TestApplyValueDeclRule_InvalidAssignValue_Error(t *testing.T) {
	spec := &dst.ValueSpec{
		Names: []*dst.Ident{{Name: "x"}},
		Type:  &dst.Ident{Name: "bool"},
	}
	file := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{spec}},
		},
	}

	r := &rule.InstValueDeclRule{
		InstBaseRule:     rule.InstBaseRule{Name: "test"},
		ValueDeclaration: "bool",
		AssignValue:      "func(", // invalid Go expression
		TypeIdent:        "bool",
	}
	err := newTestPhase().applyValueDeclRule(context.Background(), r, file)
	require.Error(t, err)
}

func TestMatchesQualifiedSelector_ChainedX(t *testing.T) {
	// sel.X is a SelectorExpr (chained call like a.b.Func), not a simple Ident — must not match.
	expr := &dst.SelectorExpr{
		X: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "a"},
			Sel: &dst.Ident{Name: "b"},
		},
		Sel: &dst.Ident{Name: "Client"},
	}
	assert.False(t, matchesQualifiedSelector(expr, "net/http", "Client", nil))
}

func TestApplyValueDeclRule_SkipsNonValueSpec(t *testing.T) {
	// A GenDecl with token.VAR that somehow contains a non-ValueSpec should be skipped.
	file := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{&dst.TypeSpec{Name: &dst.Ident{Name: "T"}}}},
		},
	}
	r := &rule.InstValueDeclRule{
		InstBaseRule:     rule.InstBaseRule{Name: "test"},
		ValueDeclaration: "bool",
		AssignValue:      "true",
		TypeIdent:        "bool",
	}
	// Should be a no-op (no match), no error.
	require.NoError(t, newTestPhase().applyValueDeclRule(context.Background(), r, file))
}

// collectImportAliases helpers already tested via TestMatchesCallRule_* in apply_call_test.go.
// The following tests confirm matchesType works end-to-end with a real file's import aliases.
func TestMatchesType_WithCollectedAliases(t *testing.T) {
	file := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok: token.IMPORT,
				Specs: []dst.Spec{
					&dst.ImportSpec{
						Path: &dst.BasicLit{Value: `"net/http"`},
					},
				},
			},
		},
	}
	imports := collectImportAliases(file)

	expr := &dst.SelectorExpr{
		X:   &dst.Ident{Name: "http"},
		Sel: &dst.Ident{Name: "Client"},
	}
	assert.True(t, matchesType(expr, "net/http", "Client", false, imports))
}
