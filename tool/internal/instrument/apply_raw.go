// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"go/format"
	"regexp"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	unnamedRetValName = "_unnamedRetVal"
	ignoredParam      = "_ignoredParam"
	// Blank named returns get their own prefix; sharing ignoredParam with a blank
	// param or receiver would collide, since both live in the same scope.
	ignoredRetValName = "_ignoredRetVal"
)

// renameReturnValues assigns referenceable names to funcDecl's unnamed return
// values. Raw and directive rules reference these by the resulting bare
// "_unnamedRetValN" name in their injected code (e.g. the runtime
// instrumentation's goroutine_propagate rule), so the naming here is a
// stable, positional convention and intentionally not salted with a rule
// hash the way collectReturnValues/collectArguments are for func rules.
func renameReturnValues(funcDecl *dst.FuncDecl) {
	if retList := funcDecl.Type.Results; retList != nil {
		idx := 0
		for _, field := range retList.List {
			if field.Names == nil {
				name := fmt.Sprintf("%s%d", unnamedRetValName, idx)
				field.Names = []*dst.Ident{ast.Ident(name)}
				idx++
			}
		}
	}
}

// renderRawCode renders the shared function template variables (FuncName,
// FuncArgument N, FuncReturn N, ...) in raw code injected by a raw rule. Raw
// code that does not contain "{{" is returned unchanged. hash salts synthetic
// argument/return names the same way InstRawRule.Identity salts other rules'
// trampoline/template names.
func renderRawCode(raw string, decl *dst.FuncDecl, hash string) (string, error) {
	if !strings.Contains(raw, "{{") {
		return raw, nil
	}
	tmpl, err := rule.ParseFuncTemplate(raw)
	if err != nil {
		return "", ex.Wrap(err)
	}
	return tmpl.Execute(newFuncTemplateData(decl, nil, nil, hash))
}

type insertPos struct {
	pattern   *regexp.Regexp
	placement string
}

func insertRawAtPattern(
	ctx context.Context,
	decl *dst.FuncDecl,
	restorer *decorator.Restorer,
	pos insertPos,
	stmts []dst.Stmt,
) bool {
	inserted := false
	logger := util.LoggerFromContext(ctx)

	dstutil.Apply(decl.Body, func(cursor *dstutil.Cursor) bool {
		if inserted {
			return false
		}

		stmt, isStmt := cursor.Node().(dst.Stmt)
		if !isStmt {
			return true
		}

		if _, ok := cursor.Parent().(*dst.BlockStmt); !ok {
			return true
		}

		astNode, nodeFound := restorer.Ast.Nodes[stmt]
		if !nodeFound {
			return true
		}

		var buf strings.Builder
		if err := format.Node(&buf, restorer.Fset, astNode); err != nil {
			logger.Warn("Failed to restore AST node to source code", "error", err)
			return true
		}

		logger.Debug("Matching statement with pattern", "stmt", buf.String(), "pattern", pos.pattern.String())
		if !pos.pattern.MatchString(buf.String()) {
			return true
		}

		switch pos.placement {
		default: // default to "before"
			for _, s := range stmts {
				cursor.InsertBefore(s)
			}
		case "after":
			for i := len(stmts) - 1; i >= 0; i-- {
				cursor.InsertAfter(stmts[i])
			}
		}

		inserted = true
		return false
	}, nil)

	return inserted
}

func insertRaw(ctx context.Context, r *rule.InstRawRule, decl *dst.FuncDecl, root *dst.File) error {
	util.Assert(decl.Name.Name == r.Func, "sanity check")
	util.Assert(decl.Body != nil, "function must have a body")

	// Rename the unnamed return values so that the raw code can reference them
	renameReturnValues(decl)

	raw, err := renderRawCode(r.Raw, decl, r.Identity())
	if err != nil {
		return ex.Wrapf(err, "rendering template for func %s", decl.Name.Name)
	}

	// Parse the raw code into AST statements
	p := ast.NewAstParser()
	stmts, err := p.ParseSnippet(raw)
	if err != nil {
		return err
	}

	// if specified, insert raw code at the position matched by the regex
	if r.Pattern != "" {
		restorer := decorator.NewRestorer()
		if _, restoreErr := restorer.RestoreFile(root); restoreErr != nil {
			return ex.Wrapf(restoreErr, "failed to restore the AST")
		}

		pattern, compileErr := regexp.Compile(r.Pattern)
		if compileErr != nil {
			return ex.Wrapf(compileErr, "invalid raw rule pattern %q", r.Pattern)
		}
		pos := insertPos{
			pattern:   pattern,
			placement: r.Placement,
		}

		inserted := insertRawAtPattern(ctx, decl, restorer, pos, stmts)
		if !inserted {
			return ex.Newf("no statement matches the pattern %s", r.Pattern)
		}

		return nil
	}

	// Insert the raw code into target function body
	decl.Body.List = append(stmts, decl.Body.List...)
	return nil
}

// applyRawRule injects the raw code into the target function at the beginning
// of the function.
func (ip *InstrumentPhase) applyRawRule(ctx context.Context, rule *rule.InstRawRule, root *dst.File) error {
	// Find the target function to be instrumented
	funcDecl, ok, err := ast.FindFuncDecl(root, rule)
	if err != nil {
		return err
	}
	if !ok {
		return ex.Newf("can not find function %s", rule.Func)
	}

	// Handle imports if specified in the rule
	if err = ip.addRuleImports(ctx, root, rule.Imports, rule.Name); err != nil {
		return err
	}

	// Insert the raw code into the target function
	err = insertRaw(ctx, rule, funcDecl, root)
	if err != nil {
		return err
	}
	ip.Info("Apply raw rule", "rule", rule)
	return nil
}
