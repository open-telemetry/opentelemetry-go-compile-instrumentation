// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderExpr(t *testing.T) {
	tests := []struct {
		name string
		expr dst.Expr
		want string
	}{
		{
			name: "identifier",
			expr: &dst.Ident{Name: "foo"},
			want: "foo",
		},
		{
			name: "call expression",
			expr: &dst.CallExpr{
				Fun:  &dst.Ident{Name: "f"},
				Args: []dst.Expr{&dst.Ident{Name: "a"}, &dst.Ident{Name: "b"}},
			},
			want: "f(a, b)",
		},
		{
			name: "binary expression",
			expr: &dst.BinaryExpr{
				X:  &dst.Ident{Name: "a"},
				Op: token.ADD,
				Y:  &dst.Ident{Name: "b"},
			},
			want: "a + b",
		},
		{
			name: "selector expression",
			expr: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "pkg"},
				Sel: &dst.Ident{Name: "Func"},
			},
			want: "pkg.Func",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, err := RenderExpr(tt.expr)

			require.NoError(t, err)
			assert.Equal(t, tt.want, text)
		})
	}
}

func TestRenderNode(t *testing.T) {
	node := &dst.Ident{Name: "a"}
	synthetic := &dst.File{
		Name: Ident("_"),
		Decls: []dst.Decl{
			&dst.FuncDecl{
				Name: Ident("_"),
				Type: &dst.FuncType{Params: &dst.FieldList{}},
				Body: &dst.BlockStmt{List: []dst.Stmt{&dst.ExprStmt{X: node}}},
			},
		},
	}
	restorer := decorator.NewRestorer()
	_, err := restorer.RestoreFile(synthetic)
	require.NoError(t, err)

	t.Run("known node renders its source text", func(t *testing.T) {
		text, txtErr := RenderNode(restorer, node)

		require.NoError(t, txtErr)
		assert.Equal(t, "a", text)
	})

	t.Run("node not present in the restorer errors", func(t *testing.T) {
		unrelated := &dst.Ident{Name: "unrelated"}

		_, txtErr := RenderNode(restorer, unrelated)

		require.Error(t, txtErr)
		assert.Contains(t, txtErr.Error(), "failed to locate restored node")
	})
}
