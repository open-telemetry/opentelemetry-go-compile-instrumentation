// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"io"

	"github.com/dave/dst"
	"github.com/valyala/fasttemplate"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

type directiveTemplateData struct {
	FuncName string // Name of the annotated function
}

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
	funcs := ast.FindFuncsByDirective(root, r.Directive)
	if len(funcs) == 0 {
		return false, nil
	}
	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return false, err
	}
	tmpl, err := fasttemplate.NewTemplate(r.Template, "{{", "}}")
	if err != nil {
		return false, ex.Wrap(err)
	}
	for _, funcDecl := range funcs {
		var (
			snippet string
			stmts   []dst.Stmt //nolint:prealloc // Slice allocated by `p.ParseSnippet`
		)
		snippet, err = renderDirective(tmpl, directiveTemplateData{FuncName: funcDecl.Name.Name})
		if err != nil {
			return false, ex.Wrapf(err, "rendering template for func %s", funcDecl.Name.Name)
		}
		p := ast.NewAstParser()
		stmts, err = p.ParseSnippet(snippet)
		if err != nil {
			return false, ex.Wrapf(err, "parsing rendered template for func %s", funcDecl.Name.Name)
		}
		renameReturnValues(funcDecl)
		funcDecl.Body.List = append(stmts, funcDecl.Body.List...)
		ip.Info("Apply directive rule", "rule", r, "func", funcDecl.Name.Name)
	}
	return true, nil
}

// renderDirective executes the template with the given data and returns the
// resulting Go source snippet.
func renderDirective(tmpl *fasttemplate.Template, data directiveTemplateData) (string, error) {
	return tmpl.ExecuteFuncStringWithErr(func(w io.Writer, tag string) (int, error) {
		switch tag {
		case "FuncName":
			return io.WriteString(w, data.FuncName)
		default:
			return 0, ex.Newf("unknown template tag %q", tag)
		}
	})
}
