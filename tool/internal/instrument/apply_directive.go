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
// function body. It reports whether any function was instrumented.
func (ip *InstrumentPhase) applyDirectiveRule(
	ctx context.Context,
	r *rule.InstDirectiveRule,
	root *dst.File,
) (bool, error) {
	// Match before mutating. The directive comment may appear anywhere in the
	// file without annotating a top-level func, and the file is rewritten even
	// when no code is injected, so adding imports up front would leave the
	// rewritten file with an unused import.
	matches, err := ast.FindFuncsByDirective(root, r.Directive)
	if err != nil {
		return false, ex.Wrap(err)
	}
	if len(matches) == 0 {
		return false, nil
	}
	tmpl, err := rule.ParseFuncTemplate(r.Template)
	if err != nil {
		return false, ex.Wrap(err)
	}
	if importErr := ip.addRuleImports(ctx, root, r.Imports, r.Name); importErr != nil {
		return false, importErr
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
			return false, ex.Wrapf(err, "rendering template for func %s", funcDecl.Name.Name)
		}
		p := ast.NewAstParser()
		stmts, err = p.ParseSnippet(snippet)
		if err != nil {
			return false, ex.Wrapf(err, "parsing rendered template for func %s", funcDecl.Name.Name)
		}
		funcDecl.Body.List = append(stmts, funcDecl.Body.List...)
		ip.Info("Apply directive rule", "rule", r, "func", funcDecl.Name.Name)
	}
	return true, nil
}

// renderDirective executes the template with the given data and returns the
// resulting Go source snippet.
func renderDirective(tmpl *rule.FuncTemplate, data *funcTemplateData) (string, error) {
	return tmpl.Execute(data)
}
