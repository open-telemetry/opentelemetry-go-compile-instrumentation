// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"
)

// replacePlaceholder replaces all occurrences of _.PLACEHOLDER_0 in the AST
// with the given node. This is used to inject the original call expression
// into the template-generated code.
func replacePlaceholder(ast, node dst.Node) (dst.Node, bool) {
	replaced := false
	result := dstutil.Apply(
		ast,
		func(cursor *dstutil.Cursor) bool {
			selectorExpr, ok := cursor.Node().(*dst.SelectorExpr)
			if !ok {
				return true
			}

			// Check if this is _.PLACEHOLDER_0
			ident, ok := selectorExpr.X.(*dst.Ident)
			if !ok || ident.Name != "_" {
				return true
			}

			if selectorExpr.Sel.Name == "PLACEHOLDER_0" {
				cursor.Replace(node)
				replaced = true
				return false
			}

			return true
		},
		nil,
	)
	return result, replaced
}
