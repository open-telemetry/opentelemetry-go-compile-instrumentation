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

// applyDirectiveRule finds all functions and function literals annotated with
// the directive, renders the template for each, and prepends the resulting Go
// statements into the function body. It reports whether any function was
// instrumented. It logs a warning for each directive comment that annotates no
// function.
func (ip *instrumentPhase) applyDirectiveRule(
	ctx context.Context,
	r *rule.InstDirectiveRule,
	root *dst.File,
) (bool, error) {
	// Match before mutating. The directive comment may appear anywhere in the
	// file without annotating a function, and the file is rewritten even
	// when no code is injected, so adding imports up front would leave the
	// rewritten file with an unused import.
	matches, err := ast.FindFuncsByDirective(root, r.Directive)
	if err != nil {
		return false, ex.Wrap(err)
	}
	ip.warnStrayDirectives(r, root)
	if len(matches) == 0 {
		return false, nil
	}
	tmpl, err := rule.ParseFuncTemplate(r.Template)
	if err != nil {
		return false, ex.Wrap(err)
	}

	imports, aliasOverrides := ip.resolveImportOverrides(root, r.Imports)

	for _, match := range matches {
		funcDecl, name := ip.directiveTarget(match)
		util.Assert(funcDecl.Body != nil, "function must have a body")
		if err = checkAliasOverrideShadowing(aliasOverrides, funcDecl); err != nil {
			return false, ex.Wrapf(err, "checking import aliases for func %s", name)
		}
		// A literal's body can also be shadowed by the names in scope around it.
		if match.Lit != nil {
			switch enclosing := match.Enclosing.(type) {
			case *dst.FuncDecl:
				err = checkAliasOverrideShadowingAt(aliasOverrides, enclosing, match.Lit)
			case *dst.GenDecl:
				err = checkAliasOverrideShadowingInDecl(aliasOverrides, enclosing, match.Lit)
			}
			if err != nil {
				return false, ex.Wrapf(err, "checking import aliases for func %s", name)
			}
		}
		var (
			snippet string
			stmts   []dst.Stmt //nolint:prealloc // Slice allocated by `p.ParseSnippet`
		)
		snippet, err = renderDirective(tmpl, newFuncTemplateData(funcDecl, match.Args, imports, r.Identity()))
		if err != nil {
			return false, ex.Wrapf(err, "rendering template for func %s", name)
		}
		p := ast.NewAstParser()
		stmts, err = p.ParseSnippet(snippet)
		if err != nil {
			return false, ex.Wrapf(err, "parsing rendered template for func %s", name)
		}
		for _, stmt := range stmts {
			replaceQualifierAliases(stmt, aliasOverrides)
			stripDynamicIdents(stmt)
		}
		funcDecl.Body.List = append(stmts, funcDecl.Body.List...)
		ip.Info("Apply directive rule", "rule", r, "func", name)
	}

	// Runs after injection so that an import the rewrite above eliminated (by
	// reusing the file's existing alias) is not added back as a duplicate.
	if importErr := ip.addRuleImports(
		ctx,
		root,
		usedRuleImports(root, r.Imports, aliasOverrides),
		r.Name,
	); importErr != nil {
		return false, importErr
	}

	return true, nil
}

// directiveTarget returns the function that match annotates, and its name
// for logs and errors. A function literal is returned as the view that
// funcLitDecl builds, and its name is "literal" with its position.
func (ip *instrumentPhase) directiveTarget(match ast.FuncDirectiveMatch) (*dst.FuncDecl, string) {
	if match.Lit == nil {
		return match.Func, match.Func.Name.Name
	}
	name := "literal"
	if pos := ip.position(match.Lit); pos != "" {
		name += " at " + pos
	}
	return funcLitDecl(match.Lit), name
}

// warnStrayDirectives logs a warning for each directive comment of r in root
// that annotates no function.
func (ip *instrumentPhase) warnStrayDirectives(r *rule.InstDirectiveRule, root *dst.File) {
	for _, node := range ast.FindStrayDirectives(root, r.Directive) {
		args := []any{"rule", r.Name, "directive", r.Directive}
		if pos := ip.position(node); pos != "" {
			args = append(args, "pos", pos)
		}
		ip.Warn("Ignoring directive comment that annotates no function", args...)
	}
}

// position returns the source position of node, or "" if it is not known.
func (ip *instrumentPhase) position(node dst.Node) string {
	if ip.parser == nil {
		return ""
	}
	pos := ip.parser.FindPosition(node)
	if !pos.IsValid() {
		return ""
	}
	return pos.String()
}

// renderDirective executes the template with the given data and returns the
// resulting Go source snippet.
func renderDirective(tmpl *rule.FuncTemplate, data *funcTemplateData) (string, error) {
	return tmpl.Execute(renderingFuncTemplateData{data})
}
