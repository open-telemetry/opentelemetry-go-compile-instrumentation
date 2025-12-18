// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"

	"github.com/dave/dst"

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

func insertRaw(r *rule.InstRawRule, decl *dst.FuncDecl) error {
	util.Assert(decl.Name.Name == r.Func, "sanity check")

	// Rename the unnamed return values so that the raw code can reference them
	renameReturnValues(decl)
	// Parse the raw code into AST statements
	p := ast.NewAstParser()
	stmts, err := p.ParseSnippet(r.Raw)
	if err != nil {
		return err
	}
	// Insert the raw code into target function body
	decl.Body.List = append(stmts, decl.Body.List...)
	return nil
}

// applyRawRule injects the raw code into the target function at the beginning
// of the function.
func (ip *InstrumentPhase) applyRawRule(rule *rule.InstRawRule, root *dst.File) error {
	// Find the target function to be instrumented
	funcDecl := ast.FindFuncDecl(root, rule.Func, rule.Recv)
	if funcDecl == nil {
		return ex.Newf("can not find function %s", rule.Func)
	}
	// Insert the raw code into the target function
	err := insertRaw(rule, funcDecl)
	if err != nil {
		return err
	}
	ip.Info("Apply raw rule", "rule", rule)
	return nil
}
