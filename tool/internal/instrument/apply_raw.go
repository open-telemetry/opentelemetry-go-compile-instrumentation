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

type rawCodeInserter struct {
	stmts    []dst.Stmt
	restorer *decorator.Restorer
	pattern  *regexp.Regexp

	block *dst.BlockStmt
	idx   int

	inserted bool
}

func (r *rawCodeInserter) Visit(node dst.Node) dst.Visitor {
	if node == nil || r.inserted {
		return nil
	}

	stmt, isStmt := node.(dst.Stmt)
	if !isStmt {
		return r
	}

	block, isBlock := stmt.(*dst.BlockStmt)
	if isBlock {
		r.block = block
		r.idx = 0

		return r
	}

	astNode, nodeFound := r.restorer.Ast.Nodes[stmt]
	if !nodeFound {
		return r
	}

	var buf strings.Builder
	_ = format.Node(&buf, r.restorer.Fset, astNode)

	if r.pattern.MatchString(buf.String()) {
		r.block.List = append(
			r.block.List[:r.idx],
			append(r.stmts, r.block.List[r.idx:]...)...,
		)

		r.inserted = true
		return nil
	}

	r.idx++
	return r
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
		inserter := rawCodeInserter{
			stmts:    stmts,
			restorer: restorer,
			pattern:  pattern,
		}
		dst.Walk(&inserter, decl.Body)
		if !inserter.inserted {
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
