// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// matchesType reports whether the given DST expression represents the type
// described by importPath, typeName, and pointer.
//
//   - pointer=true: expression must be a *dst.StarExpr wrapping the inner type
//   - importPath!="": expression must be a selector (pkg.Type), resolved via imports map
//   - importPath=="": expression must be a plain identifier matching typeName
func matchesType(expr dst.Expr, importPath, typeName string, pointer bool, imports map[string]string) bool {
	if pointer {
		star, ok := expr.(*dst.StarExpr)
		if !ok {
			return false
		}
		return matchesType(star.X, importPath, typeName, false, imports)
	}
	// Rule is non-pointer; reject pointer expressions.
	if _, ok := expr.(*dst.StarExpr); ok {
		return false
	}

	if importPath != "" {
		return matchesQualifiedSelector(expr, importPath, typeName, imports)
	}

	// Built-in or unqualified type.
	ident, ok := expr.(*dst.Ident)
	if !ok {
		return false
	}
	return ident.Name == typeName
}

// applyValueDeclRule applies a value-declaration rule to the target file,
// replacing the values of all matching explicitly-typed var/const declarations.
// Files with no matching declarations are a silent no-op.
func (ip *InstrumentPhase) applyValueDeclRule(ctx context.Context, r *rule.InstValueDeclRule, root *dst.File) error {
	var imports map[string]string
	if r.TypeImportPath != "" {
		imports = collectImportAliases(root)
	}

	var matched []*dst.ValueSpec
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || (genDecl.Tok != token.VAR && genDecl.Tok != token.CONST) {
			continue
		}
		for _, spec := range genDecl.Specs {
			vs, isValueSpec := spec.(*dst.ValueSpec)
			if !isValueSpec {
				continue
			}
			// Skip untyped declarations (design decision: only match explicit types).
			if vs.Type == nil {
				continue
			}
			if matchesType(vs.Type, r.TypeImportPath, r.TypeIdent, r.TypePointer, imports) {
				matched = append(matched, vs)
			}
		}
	}

	if len(matched) == 0 {
		return nil // silent no-op
	}

	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}

	expr, err := parseValueExpr(r.AssignValue)
	if err != nil {
		return err
	}

	for _, spec := range matched {
		spec.Values = make([]dst.Expr, len(spec.Names))
		for i := range spec.Values {
			spec.Values[i] = util.AssertType[dst.Expr](dst.Clone(expr))
		}
	}

	ip.Info("Apply value decl rule", "rule", r)
	return nil
}
