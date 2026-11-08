// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func (ip *instrumentPhase) applyStructRule(ctx context.Context, rule *rule.InstStructRule, root *dst.File) error {
	structType := ast.FindStructType(root, rule.Struct)
	if structType == nil {
		return ex.Newf("can not find struct %q (missing, or not a struct type)", rule.Struct)
	}

	_, aliasOverrides := ip.resolveImportOverrides(root, rule.Imports)

	existingFields := make(map[string]bool, len(structType.Fields.List))
	for _, f := range structType.Fields.List {
		if len(f.Names) == 0 {
			// Embedded field: its effective name is the type name itself.
			existingFields[embeddedFieldName(f.Type)] = true
			continue
		}
		for _, name := range f.Names {
			existingFields[name.Name] = true
		}
	}

	for _, field := range rule.NewField {
		if existingFields[field.Name] {
			return ex.Newf("struct %q already has a field named %q", rule.Struct, field.Name)
		}
		typeExpr, err := parseGoTypeExpression(field.Type)
		if err != nil {
			return ex.Wrapf(err, "failed to parse type %q for field %q", field.Type, field.Name)
		}
		replaceQualifierAliases(typeExpr, aliasOverrides)
		structType.Fields.List = append(structType.Fields.List, ast.Field(field.Name, typeExpr))
		existingFields[field.Name] = true
	}

	// Handle imports if specified in the rule
	if err := ip.addRuleImports(ctx, root, usedRuleImports(root, rule.Imports, aliasOverrides), rule.Name); err != nil {
		return err
	}

	ip.Info("Apply struct rule", "rule", rule)
	return nil
}

// embeddedFieldName returns the field name Go derives for an embedded field,
// which is the type name with any pointer, package qualifier and type
// arguments removed (e.g. *pkg.Inner[T] embeds as "Inner").
func embeddedFieldName(typeExpr dst.Expr) string {
	if star, ok := typeExpr.(*dst.StarExpr); ok {
		typeExpr = star.X
	}
	switch e := typeExpr.(type) {
	case *dst.IndexExpr:
		typeExpr = e.X
	case *dst.IndexListExpr:
		typeExpr = e.X
	}
	switch e := typeExpr.(type) {
	case *dst.SelectorExpr:
		return e.Sel.Name
	case *dst.Ident:
		return e.Name
	default:
		return ""
	}
}
