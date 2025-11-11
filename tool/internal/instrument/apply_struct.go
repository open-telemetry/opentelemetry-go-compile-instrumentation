// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

func (ip *InstrumentPhase) applyStructRule(rule *rule.InstStructRule, root *dst.File) error {
	structDecl := ast.FindStructDecl(root, rule.Struct)
	if structDecl == nil {
		return ex.Newf("can not find struct %s", rule.Struct)
	}
	for _, field := range rule.NewField {
		ast.AddStructField(structDecl, field.Name, field.Type)
	}
	ip.Info("Apply struct rule", "rule", rule)
	return nil
}
