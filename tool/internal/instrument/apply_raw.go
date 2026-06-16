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

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	unnamedRetValName = "_unnamedRetVal"
	ignoredParam      = "_ignoredParam"
)

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

	// Rename the unnamed return values so that the raw code can reference them
	renameReturnValues(decl)
	// Parse the raw code into AST statements
	p := ast.NewAstParser()
	stmts, err := p.ParseSnippet(r.Raw)
	if err != nil {
		return err
	}

	// if specified, insert raw code at the position matched by the regex
	if r.Pattern != "" {
		restorer := decorator.NewRestorer()
		if _, restoreErr := restorer.RestoreFile(root); restoreErr != nil {
			return ex.Wrapf(restoreErr, "failed to restore the AST")
		}

		pattern := regexp.MustCompile(r.Pattern)
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
	if err := ip.addRuleImports(ctx, root, rule.Imports, rule.Name); err != nil {
		return err
	}

	// Insert the raw code into the target function
	err := insertRaw(ctx, rule, funcDecl, root)
	if err != nil {
		return err
	}
	ip.Info("Apply raw rule", "rule", rule)
	return nil
}
