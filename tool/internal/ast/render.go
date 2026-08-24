// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/format"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// RenderExpr renders expr back into Go source text.
func RenderExpr(expr dst.Expr) (string, error) {
	cloned := util.AssertType[dst.Expr](dst.Clone(expr))
	synthetic := &dst.File{
		Name: Ident("_"),
		Decls: []dst.Decl{
			&dst.FuncDecl{
				Name: Ident("_"),
				Type: &dst.FuncType{Params: &dst.FieldList{}},
				Body: &dst.BlockStmt{List: []dst.Stmt{&dst.ExprStmt{X: cloned}}},
			},
		},
	}

	restorer := decorator.NewRestorer()
	if _, err := restorer.RestoreFile(synthetic); err != nil {
		return "", ex.Wrapf(err, "failed to restore expression to source")
	}
	return RenderNode(restorer, cloned)
}

// RenderNode looks up node's restored counterpart in restorer and renders it
// back to Go source text.
func RenderNode(restorer *decorator.Restorer, node dst.Node) (string, error) {
	astNode, ok := restorer.Ast.Nodes[node]
	if !ok {
		return "", ex.New("failed to locate restored node")
	}
	var buf strings.Builder
	if err := format.Node(&buf, restorer.Fset, astNode); err != nil {
		return "", ex.Wrapf(err, "failed to format node")
	}
	return buf.String(), nil
}
