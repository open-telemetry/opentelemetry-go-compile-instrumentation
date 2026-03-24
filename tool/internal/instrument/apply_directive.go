// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"io"

	"github.com/dave/dst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/valyala/fasttemplate"
)

type directiveTemplateData struct {
	FuncName string // Name of the annotated function
}

// applyDirectiveRule finds all functions annotated with the directive, renders
// the template for each, and prepends the resulting Go statements into the
// function body.
func (ip *InstrumentPhase) applyDirectiveRule(ctx context.Context, r *rule.InstDirectiveRule, root *dst.File) error {
	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}
	tmpl, err := fasttemplate.NewTemplate(r.Template, "{{", "}}")
	if err != nil {
		return ex.Wrap(err)
	}
	funcs := ast.FindFuncsByDirective(root, r.Directive)
	for _, funcDecl := range funcs {
		snippet, err := renderDirective(tmpl, directiveTemplateData{FuncName: funcDecl.Name.Name})
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
