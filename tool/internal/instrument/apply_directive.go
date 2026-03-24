// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"

	"github.com/dave/dst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

// applyDirectiveRule finds all functions annotated with the directive, renders
// the template for each, and prepends the resulting Go statements into the
// function body.
func (ip *InstrumentPhase) applyDirectiveRule(ctx context.Context, r *rule.InstDirectiveRule, root *dst.File) error {
	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}
	funcs := ast.FindFuncsByDirective(root, r.Directive)
	for _, funcDecl := range funcs {
		snippet, err := r.Render(rule.DirectiveTemplateData{FuncName: funcDecl.Name.Name})
		if err != nil {
			return ex.Wrapf(err, "rendering template for func %s", funcDecl.Name.Name)
		}
		p := ast.NewAstParser()
		stmts, err := p.ParseSnippet(snippet)
		if err != nil {
			return ex.Wrapf(err, "parsing rendered template for func %s", funcDecl.Name.Name)
		}
		renameReturnValues(funcDecl)
		funcDecl.Body.List = append(stmts, funcDecl.Body.List...)
		ip.Info("Apply directive rule", "rule", r, "func", funcDecl.Name.Name)
	}
	return nil
}
