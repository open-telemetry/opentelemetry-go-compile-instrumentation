// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
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

func TestMatchesType_Pointer_Qualified(t *testing.T) {
	expr := &dst.StarExpr{
		X: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "http", Path: "net/http"},
			Sel: &dst.Ident{Name: "Request"},
		},
	}
	assert.True(t, matchesType(expr, "net/http", "Request", true, nil))
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
