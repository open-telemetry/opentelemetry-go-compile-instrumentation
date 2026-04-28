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
	util.Assert(r.Value != "" || r.Wrap != "", "decl rule must set value or wrap")

	node := ast.FindNamedDecl(root, r.Identifier, r.Kind)
	if node == nil {
		return ex.Newf("can not find declaration %q (kind: %q)", r.Identifier, r.Kind)
	}

	// Handle imports if specified in the rule
	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}

	spec := util.AssertType[*dst.ValueSpec](node)

	if r.Wrap != "" {
		if err := wrapDeclValues(spec, r.Wrap); err != nil {
			return err
		}
		ip.Info("Apply decl rule", "rule", r)
		return nil
	}

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

// wrapDeclValues wraps each initializer in spec using the given template.
// Returns an error if spec has no initializers, since wrap requires
// an existing value to substitute into {{ . }}.
func wrapDeclValues(spec *dst.ValueSpec, templateStr string) error {
	if len(spec.Values) == 0 {
		return ex.Newf(
			"wrap requires an existing initializer but the declaration has none",
		)
	}

	tmpl, err := newCallTemplate(templateStr)
	if err != nil {
		return ex.Wrapf(err, "failed to compile wrap template")
	}

	var wrapped dst.Expr
	for i, val := range spec.Values {
		wrapped, err = tmpl.compileExpression(val)
		if err != nil {
			return ex.Wrapf(err, "failed to wrap expression at index %d", i)
		}
		spec.Values[i] = util.AssertType[dst.Expr](dst.Clone(wrapped))
	}

	return nil
}
