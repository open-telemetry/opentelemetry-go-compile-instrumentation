// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// hookUsesContext deliberately counts every matching identifier, including
// shadowed names, so uncertain uses retain the existing context path.
func hookUsesContext(hook *dst.FuncDecl) bool {
	if hook.Body == nil {
		return true
	}
	if hook.Type == nil || hook.Type.Params == nil || len(hook.Type.Params.List) == 0 {
		return true
	}
	names := hook.Type.Params.List[0].Names
	if len(names) == 0 || names[0].Name == "_" {
		return false
	}
	used := false
	dst.Inspect(hook.Body, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok && id.Name == names[0].Name {
			used = true
		}
		return !used
	})
	return used
}

func (ip *instrumentPhase) ruleUsesContext(r *rule.InstFuncRule) (bool, error) {
	for _, before := range []bool{true, false} {
		if getHookFuncName(r, before) == "" {
			continue
		}
		hook, err := ip.getHookFunc(r, before)
		if err != nil {
			return true, err
		}
		if hookUsesContext(hook) {
			return true, nil
		}
	}
	return false, nil
}

// omitTrampolineContext keeps the trampoline signature and panic guard while
// removing context construction and passing nil across the linkname boundary.
func omitTrampolineContext(fn *dst.FuncDecl, before bool) {
	stmts := fn.Body.List[:0]
	for _, stmt := range fn.Body.List {
		if assign, ok := stmt.(*dst.AssignStmt); ok {
			contextAssignment := false
			for _, lhs := range assign.Lhs {
				dst.Inspect(lhs, func(n dst.Node) bool {
					if id, ok := n.(*dst.Ident); ok && id.Name == trampolineHookContextName {
						contextAssignment = true
					}
					return true
				})
			}
			if contextAssignment {
				continue
			}
		}
		if ret, ok := stmt.(*dst.ReturnStmt); ok && before {
			ret.Results = []dst.Expr{ast.Nil(), ast.Ident("false")}
		}
		dst.Inspect(stmt, func(n dst.Node) bool {
			if call, ok := n.(*dst.CallExpr); ok && len(call.Args) > 0 {
				if id, ok := call.Args[0].(*dst.Ident); ok && id.Name == trampolineHookContextName {
					call.Args[0] = ast.Nil()
				}
			}
			return true
		})
		stmts = append(stmts, stmt)
	}
	fn.Body.List = stmts
}
