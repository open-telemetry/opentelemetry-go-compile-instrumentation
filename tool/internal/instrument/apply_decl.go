// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"slices"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
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
	if len(valueSpec.Values) != 1 {
		return nil, ex.Newf("invalid value expression %q: expected 1 value, got %d", exprSource, len(valueSpec.Values))
	}
	return valueSpec.Values[0], nil
}

// applyDeclRule applies a declaration rule to the target file, modifying the
// matched named declaration (e.g., assigning a new value to a var or const).
func (ip *instrumentPhase) applyDeclRule(ctx context.Context, r *rule.InstDeclRule, root *dst.File) error {
	util.Assert(r.Replace != "" || r.Wrap != "", "decl rule must set replace or wrap")

	node := ast.FindNamedDecl(root, r.Identifier, r.Kind)
	if node == nil {
		return ex.Newf("can not find declaration %q (kind: %q)", r.Identifier, r.Kind)
	}

	spec, ok := node.(*dst.ValueSpec)
	if !ok {
		return ex.Newf("declaration %q (kind: %q) is not a var or const declaration", r.Identifier, r.Kind)
	}

	// One ValueSpec can declare several names (var a, b = 1, 2), and only the
	// name the rule targets may be rewritten.
	//
	// nameIdx is guaranteed non-negative: FindNamedDecl already selected this
	// spec via the identical name.Name == r.Identifier comparison, so the
	// targeted identifier is present in spec.Names by construction.
	nameIdx := slices.IndexFunc(spec.Names, func(name *dst.Ident) bool { return name.Name == r.Identifier })
	util.Assert(nameIdx >= 0, "matched spec must declare the targeted identifier")

	// Validate and apply the rewrite before touching imports: a rule that
	// fails here (unparsable wrap/replace expression, tuple-valued
	// initializer) must not leave an import spec in root.Decls behind it.
	if r.Wrap != "" {
		if err := wrapDeclValue(spec, r.Wrap, nameIdx); err != nil {
			return err
		}
	} else {
		if err := replaceDeclValue(spec, r.Replace, nameIdx); err != nil {
			return err
		}
	}

	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}

	ip.Info("Apply decl rule", "rule", r)
	return nil
}

// replaceDeclValue assigns the parsed replacement expression to the initializer
// of the name at nameIdx, leaving every sibling name in the spec untouched.
func replaceDeclValue(spec *dst.ValueSpec, replace string, nameIdx int) error {
	expr, err := parseValueExpr(replace)
	if err != nil {
		return err
	}

	switch {
	case len(spec.Values) == len(spec.Names):
		spec.Values[nameIdx] = util.AssertType[dst.Expr](dst.Clone(expr))
	case len(spec.Names) == 1 && len(spec.Values) == 0:
		// A single name with no initializer gains one: var X T -> var X T = expr.
		spec.Values = []dst.Expr{util.AssertType[dst.Expr](dst.Clone(expr))}
	default:
		return declArityError(spec, nameIdx)
	}

	return nil
}

// wrapDeclValue wraps the initializer of the name at nameIdx using the given
// template, leaving every sibling name in the spec untouched. Returns an error
// if spec has no initializers, since wrap requires an existing value to
// substitute into {{ . }}.
func wrapDeclValue(spec *dst.ValueSpec, templateStr string, nameIdx int) error {
	if len(spec.Values) == 0 {
		return ex.Newf(
			"wrap requires an existing initializer but the declaration has none",
		)
	}
	if len(spec.Values) != len(spec.Names) {
		return declArityError(spec, nameIdx)
	}

	tmpl, err := newCallTemplate(templateStr)
	if err != nil {
		return ex.Wrapf(err, "failed to compile wrap template")
	}

	// Package-level var/const initializers have no enclosing function.
	wrapped, err := tmpl.compileExpression(spec.Values[nameIdx], nil)
	if err != nil {
		return ex.Wrapf(err, "failed to wrap expression at index %d", nameIdx)
	}
	spec.Values[nameIdx] = util.AssertType[dst.Expr](dst.Clone(wrapped))

	return nil
}

// declArityError reports a declaration whose initializers cannot be attributed
// to individual names, such as a tuple-valued `var a, b = f()`. Rewriting one
// name there would change the shape of the declaration, so it is refused
// instead of guessed at.
func declArityError(spec *dst.ValueSpec, nameIdx int) error {
	return ex.Newf(
		"declaration %q declares %d names but has %d initializer(s); "+
			"rewriting a single name requires one initializer per name "+
			"(tuple-valued declarations such as `var a, b = f()` are not supported)",
		spec.Names[nameIdx].Name, len(spec.Names), len(spec.Values),
	)
}
