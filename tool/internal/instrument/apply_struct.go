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

	for _, field := range rule.NewField {
		typeExpr, err := parseGoTypeExpression(field.Type)
		if err != nil {
			return ex.Wrapf(err, "failed to parse type %q for field %q", field.Type, field.Name)
		}
		replaceQualifierAliases(typeExpr, aliasOverrides)
		structType.Fields.List = append(structType.Fields.List, ast.Field(field.Name, typeExpr))
	}

	// Handle imports if specified in the rule
	if err := ip.addRuleImports(ctx, root, usedRuleImports(root, rule.Imports), rule.Name); err != nil {
		return err
	}

	ip.Info("Apply struct rule", "rule", rule)
	return nil
}
