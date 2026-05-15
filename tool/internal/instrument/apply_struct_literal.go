// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

// applyStructLiteralRule transforms struct literals by wrapping them with
// code according to the provided template.
func (ip *InstrumentPhase) applyStructLiteralRule(
	ctx context.Context,
	r *rule.InstStructLiteralRule,
	root *dst.File,
) error {
	importAliases := collectImportAliases(root)

	// Determine expected import path and struct name
	parts := strings.Split(r.StructLiteral, ".")
	const minParts = 2
	if len(parts) < minParts {
		return ex.Newf("invalid struct_literal %q, expected pkg.StructName", r.StructLiteral)
	}
	structName := parts[len(parts)-1]
	importPath := strings.Join(parts[:len(parts)-1], ".")

	tmpl, err := newCallTemplate(r.Template)
	if err != nil {
		return ex.Wrapf(err, "invalid template in struct_literal rule")
	}

	modified := false

	dstutil.Apply(root, func(cursor *dstutil.Cursor) bool {
		node := cursor.Node()

		var compLit *dst.CompositeLit
		var pointer bool
		var targetNode dst.Expr

		// Case 1: pointer match &Struct{}
		if unary, isUnary := node.(*dst.UnaryExpr); isUnary && unary.Op.String() == "&" {
			if cl, isCl := unary.X.(*dst.CompositeLit); isCl {
				compLit = cl
				pointer = true
				targetNode = unary
			}
		} else if cl, isCl := node.(*dst.CompositeLit); isCl {
			// Check if it's inside a pointer &
			if parentUnary, isParentUnary := cursor.Parent().(*dst.UnaryExpr); isParentUnary &&
				parentUnary.Op.String() == "&" &&
				parentUnary.X == cl {
				return true // Ignore, it is handled at the UnaryExpr level
			}
			// Case 2: value match Struct{}
			compLit = cl
			pointer = false
			targetNode = cl
		}

		if compLit == nil {
			return true
		}

		// check type
		if !matchesStructType(compLit.Type, importPath, structName, importAliases) {
			return true
		}

		// check match mode
		if r.Match == "value-only" && pointer {
			return true
		}
		if r.Match == "pointer-only" && !pointer {
			return true
		}

		// wrap
		wrappedExpr, tmplErr := tmpl.compileExpression(targetNode)
		if tmplErr != nil {
			ip.Warn("Failed to compile template for struct literal", "error", tmplErr)
			return true
		}

		// clone and replace
		cloned := dst.Clone(wrappedExpr)
		cursor.Replace(cloned)
		modified = true

		return true // continue visiting children of the wrapped expression if any (e.g. nested literals)
	}, nil)

	if modified {
		if addErr := ip.addRuleImports(ctx, root, r.Imports, r.Name); addErr != nil {
			return addErr
		}
		ip.Info("Apply struct_literal rule", "rule", r)
	}

	return nil
}

func matchesStructType(expr dst.Expr, expectedPath, expectedName string, aliases map[string]string) bool {
	// Case 1: Same-package struct (e.g., Config{} when struct_literal is "main.Config")
	if ident, ok := expr.(*dst.Ident); ok {
		return ident.Name == expectedName && ident.Path == ""
	}
	// Case 2: Imported struct (e.g., http.Server{} when struct_literal is "net/http.Server")
	sel, ok := expr.(*dst.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != expectedName {
		return false
	}
	ident, ok := sel.X.(*dst.Ident)
	if !ok {
		return false
	}

	pkgPath := ident.Path
	if pkgPath != "" {
		return pkgPath == expectedPath
	}

	resolvedPath, ok := aliases[ident.Name]
	return ok && resolvedPath == expectedPath
}
