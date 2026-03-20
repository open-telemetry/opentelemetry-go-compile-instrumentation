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
		return nil, err
	}
	genDecl := util.AssertType[*dst.GenDecl](file.Decls[0])
	valueSpec := util.AssertType[*dst.ValueSpec](genDecl.Specs[0])
	util.Assert(len(valueSpec.Values) == 1, "expected exactly one value in parsed expression")
	return valueSpec.Values[0], nil
}

// applyDeclRule applies a declaration rule to the target file, modifying the
// matched named declaration (e.g., assigning a new value to a var or const).
func (ip *InstrumentPhase) applyDeclRule(ctx context.Context, r *rule.InstDeclRule, root *dst.File) error {
	if r.Value == "" {
		return nil
	}

	node := ast.FindNamedDecl(root, r.Declaration, r.Kind)
	if node == nil {
		return ex.Newf("cannot find declaration %q (kind: %q)", r.Declaration, r.Kind)
	}

	// Handle imports if specified in the rule
	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}

	spec := util.AssertType[*dst.ValueSpec](node)
	expr, err := parseValueExpr(r.Value)
	if err != nil {
		return err
	}
	// Assign the expression to all names in the spec.
	spec.Values = make([]dst.Expr, len(spec.Names))
	for i := range spec.Values {
		spec.Values[i] = util.AssertType[dst.Expr](dst.Clone(expr))
	}

	ip.Info("Apply decl rule", "rule", r)
	return nil
}
