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

func (ip *InstrumentPhase) applyStructRule(ctx context.Context, rule *rule.InstStructRule, root *dst.File) error {
	structDecl := ast.FindStructDecl(root, rule.Struct)
	if structDecl == nil {
		return ex.Newf("can not find struct %s", rule.Struct)
	}

	// Handle imports if specified in the rule
	if err := ip.addRuleImports(ctx, root, rule.Imports, rule.Name); err != nil {
		return err
	}

	for _, field := range rule.NewField {
		ast.AddStructField(structDecl, field.Name, field.Type)
	}
	ip.Info("Apply struct rule", "rule", rule)
	return nil
}
