// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// parseValueExpr parses a Go expression string by wrapping it as a var
// declaration and extracting the resulting expression node.
func parseValueExpr(exprSource string) (dst.Expr, error) {
	p := ast.NewAstParser()
	// Wrap as a valid package-level declaration so it can be parsed.
	snippet := "package main\nvar _ = " + exprSource
	file, err := p.ParseSource(snippet)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to parse assign_value expression: %s", exprSource)
	}
	genDecl := util.AssertType[*dst.GenDecl](file.Decls[0])
	valueSpec := util.AssertType[*dst.ValueSpec](genDecl.Specs[0])
	util.Assert(len(valueSpec.Values) == 1, "expected exactly one value in parsed expression")
	return valueSpec.Values[0], nil
}

// applyDeclRule applies a declaration rule to the target file, modifying the
// matched named declaration (e.g., assigning a new value to a var or const).
func (ip *InstrumentPhase) applyDeclRule(ctx context.Context, r *rule.InstDeclRule, root *dst.File) error {
	node := ast.FindNamedDecl(root, r.DeclarationOf, r.DeclKind)
	if node == nil {
		return ex.Newf("can not find declaration %q (kind: %q)", r.DeclarationOf, r.DeclKind)
	}

	// Handle imports if specified in the rule
	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}

	if r.AssignValue != "" {
		spec, ok := node.(*dst.ValueSpec)
		if !ok {
			return ex.Newf(
				"assign_value requires a var or const declaration, but %q matched a %T",
				r.DeclarationOf,
				node,
			)
		}
		expr, err := parseValueExpr(r.AssignValue)
		if err != nil {
			return err
		}
		// Assign the expression to all names in the spec (following Orchestrion's
		// assign-value pattern: clone the expression for each declared name).
		spec.Values = make([]dst.Expr, len(spec.Names))
		for i := range spec.Values {
			spec.Values[i] = util.AssertType[dst.Expr](dst.Clone(expr))
		}
	}

	ip.Info("Apply decl rule", "rule", r)
	return nil
}
