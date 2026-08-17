// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// applyDirectiveRule finds all functions annotated with the directive, renders
// the template for each, and prepends the resulting Go statements into the
// function body.
func (ip *InstrumentPhase) applyDirectiveRule(ctx context.Context, r *rule.InstDirectiveRule, root *dst.File) error {
	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}
	tmpl, err := rule.ParseDirectiveTemplate(r.Template)
	if err != nil {
		return ex.Wrap(err)
	}
	matches, err := ast.FindFuncsByDirective(root, r.Directive)
	if err != nil {
		return ex.Wrap(err)
	}
	imports := ast.ImportAliasMap(root)
	for _, match := range matches {
		funcDecl := match.Func
		util.Assert(funcDecl.Body != nil, "function must have a body")
		var (
			snippet string
			stmts   []dst.Stmt //nolint:prealloc // Slice allocated by `p.ParseSnippet`
		)
		snippet, err = renderDirective(tmpl, newFuncTemplateData(funcDecl, match.Args, imports, r.Identity()))
		if err != nil {
			return ex.Wrapf(err, "rendering template for func %s", funcDecl.Name.Name)
		}
		p := ast.NewAstParser()
		stmts, err = p.ParseSnippet(snippet)
		if err != nil {
			return ex.Wrapf(err, "parsing rendered template for func %s", funcDecl.Name.Name)
		}
		funcDecl.Body.List = append(stmts, funcDecl.Body.List...)
		ip.Info("Apply directive rule", "rule", r, "func", funcDecl.Name.Name)
	}
	return nil
}

// renderDirective executes the template with the given data and returns the
// resulting Go source snippet.
func renderDirective(tmpl *rule.DirectiveTemplate, data *funcTemplateData) (string, error) {
	return tmpl.Execute(data)
}
