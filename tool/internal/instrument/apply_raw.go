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

func insertRawAtPos(
	decl *dst.FuncDecl,
	restorer *decorator.Restorer,
	pattern *regexp.Regexp,
	stmts []dst.Stmt,
) bool {
	inserted := false

	dstutil.Apply(decl.Body, func(cursor *dstutil.Cursor) bool {
		if inserted {
			return false
		}

		stmt, isStmt := cursor.Node().(dst.Stmt)
		if !isStmt {
			return true
		}

		astNode, nodeFound := restorer.Ast.Nodes[stmt]
		if !nodeFound {
			return true
		}

		var buf strings.Builder
		_ = format.Node(&buf, restorer.Fset, astNode)

		if pattern.MatchString(buf.String()) {
			if _, ok := cursor.Parent().(*dst.BlockStmt); !ok {
				return true
			}

			for _, s := range stmts {
				cursor.InsertBefore(s)
			}

			inserted = true
			return false
		}

		return true
	}, nil)

	return inserted
}

func insertRaw(r *rule.InstRawRule, decl *dst.FuncDecl, root *dst.File) error {
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
	if r.Pos != "" {
		restorer := decorator.NewRestorer()
		if _, restoreErr := restorer.RestoreFile(root); restoreErr != nil {
			return ex.Wrapf(restoreErr, "failed to restore the AST")
		}

		pattern := regexp.MustCompile(r.Pos)
		inserted := insertRawAtPos(decl, restorer, pattern, stmts)
		if !inserted {
			return ex.Newf("failed to find the position to insert raw code with pattern: %s", r.Pos)
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
	funcDecl := ast.FindFuncDecl(root, rule.Func, rule.Recv)
	if funcDecl == nil {
		return ex.Newf("can not find function %s", rule.Func)
	}

	// Handle imports if specified in the rule
	if err := ip.addRuleImports(ctx, root, rule.Imports, rule.Name); err != nil {
		return err
	}

	// Insert the raw code into the target function
	err := insertRaw(rule, funcDecl, root)
	if err != nil {
		return err
	}
	ip.Info("Apply raw rule", "rule", rule)
	return nil
}
