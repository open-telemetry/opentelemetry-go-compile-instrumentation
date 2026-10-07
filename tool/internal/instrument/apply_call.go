// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// applyCallRule transforms function calls at call sites by wrapping them with
// instrumentation code according to the provided replacement template. It
// reports whether it changed root.
func (ip *instrumentPhase) applyCallRule(ctx context.Context, r *rule.InstCallRule, root *dst.File) (bool, error) {
	importAliases, aliasOverrides := ip.resolveImportOverrides(root, r.Imports)

	appendModified, err := ip.applyCallAppendArgs(r, root, importAliases, aliasOverrides)
	if err != nil {
		return false, err
	}

	replaceModified := false
	if r.Replace != "" {
		replaceModified, err = ip.applyCallReplace(r, root, importAliases, aliasOverrides)
		if err != nil {
			return false, err
		}
	}

	if !appendModified && !replaceModified {
		return false, nil
	}

	if err = ip.addRuleImports(ctx, root, usedRuleImports(root, r.Imports, aliasOverrides), r.Name); err != nil {
		return false, err
	}
	ip.Info("Apply call rule", "rule", r)

	return true, nil
}

// walkCallsWithEnclosingFunc visits every *dst.CallExpr in root and invokes fn
// with the call and the top-level *dst.FuncDecl that contains it. Returns nil for
// calls outside any function body, e.g. a package-level variable
// initializer.
func walkCallsWithEnclosingFunc(root *dst.File, fn func(call *dst.CallExpr, enclosing *dst.FuncDecl) bool) {
	stopped := false
	for _, decl := range root.Decls {
		if stopped {
			return
		}
		enclosing, _ := decl.(*dst.FuncDecl)
		dst.Inspect(decl, func(node dst.Node) bool {
			if stopped {
				return false
			}
			call, ok := node.(*dst.CallExpr)
			if ok && !fn(call, enclosing) {
				stopped = true
				return false
			}
			return true
		})
	}
}

// applyCallReplace applies replacement wrapping to all matching calls in root using a
// two-pass approach to avoid re-matching wrapped nodes.
// Returns true if any replacement was made.
//
// A //otelc:ignore comment above a matching call's enclosing statement opts
// that one call site out and leaves every other matching call site wrapped.
func (ip *instrumentPhase) applyCallReplace(
	r *rule.InstCallRule,
	root *dst.File,
	importAliases map[string]string,
	aliasOverrides map[string]string,
) (bool, error) {
	tmpl, err := newCallTemplate(r.Replace)
	if err != nil {
		return false, err
	}

	stmts := ast.CallEnclosingStmts(root)

	// Pass 1: collect matching calls and pre-compute replacements to avoid
	// re-matching the original call pointer inside its own wrapper.
	replacements := make(map[*dst.CallExpr]dst.Expr)
	var wrapError error
	walkCallsWithEnclosingFunc(root, func(call *dst.CallExpr, enclosing *dst.FuncDecl) bool {
		if !matchesCallRule(call, r, importAliases) {
			return true
		}
		if ast.HasLeadingDirective(stmts[call], util.DirectiveIgnore) {
			ip.Debug("Skip call site due to //otelc:ignore", "rule", r.Name)
			ip.markIgnoreConsumed(stmts[call])
			return true
		}
		if shadowErr := checkAliasOverrideShadowing(aliasOverrides, enclosing); shadowErr != nil {
			wrapError = shadowErr
			return false
		}
		wrapped, wrapErr := tmpl.compileExpression(call, enclosing, importAliases, aliasOverrides)
		if wrapErr != nil {
			wrapError = wrapErr
			return false
		}
		replacements[call] = util.AssertType[dst.Expr](dst.Clone(wrapped))
		return true
	})

	if wrapError != nil {
		return false, wrapError
	}

	if len(replacements) == 0 {
		return false, nil
	}

	// Pass 2: replace each matched call with its pre-computed expression.
	dstutil.Apply(root, func(cursor *dstutil.Cursor) bool {
		call, ok := cursor.Node().(*dst.CallExpr)
		if !ok {
			return true
		}
		replacement, found := replacements[call]
		if !found {
			return true
		}
		cursor.Replace(replacement)
		return true
	}, nil)

	return true, nil
}

// applyCallAppendArgs appends r's extra arguments to every call site that
// matches r. A //otelc:ignore comment above a matching call's enclosing
// statement opts that one call site out and leaves every other matching call
// site appended.
func (ip *instrumentPhase) applyCallAppendArgs(
	r *rule.InstCallRule,
	root *dst.File,
	importAliases map[string]string,
	aliasOverrides map[string]string,
) (bool, error) {
	if len(r.AppendArgs) == 0 {
		return false, nil
	}

	newArgs, err := parseAppendArgs(r.AppendArgs, aliasOverrides)
	if err != nil {
		return false, err
	}

	stmts := ast.CallEnclosingStmts(root)
	var matchingCalls []*dst.CallExpr
	dst.Inspect(root, func(node dst.Node) bool {
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}
		if !matchesCallRule(call, r, importAliases) {
			return true
		}
		if ast.HasLeadingDirective(stmts[call], util.DirectiveIgnore) {
			ip.Debug("Skip call site due to //otelc:ignore", "rule", r.Name)
			ip.markIgnoreConsumed(stmts[call])
			return true
		}
		matchingCalls = append(matchingCalls, call)
		return true
	})
	modified := false
	for _, call := range matchingCalls {
		callArgs := make([]dst.Expr, len(newArgs))
		for i, arg := range newArgs {
			callArgs[i] = util.AssertType[dst.Expr](dst.Clone(arg))
		}
		ok, appendErr := appendParsedCallArgs(call, r, callArgs, aliasOverrides)
		if appendErr != nil {
			return false, appendErr
		}
		modified = modified || ok
	}

	return modified, nil
}

// appendCallArgs appends the expressions from r.AppendArgs to the call's argument list.
// For ellipsis calls, an IIFE wrapper is generated using r.VariadicType.
// Returns (true, nil) if the call was modified, (false, nil) if AppendArgs is empty.
func appendCallArgs(call *dst.CallExpr, r *rule.InstCallRule) (bool, error) {
	newArgs, err := parseAppendArgs(r.AppendArgs, nil)
	if err != nil {
		return false, err
	}
	return appendParsedCallArgs(call, r, newArgs, nil)
}

func parseAppendArgs(args []string, aliasOverrides map[string]string) ([]dst.Expr, error) {
	newArgs := make([]dst.Expr, 0, len(args))
	for _, argStr := range args {
		argExpr, err := parseGoExpression(argStr)
		if err != nil {
			return nil, ex.Wrapf(err, "failed to parse append_args entry %q", argStr)
		}
		replaceQualifierAliases(argExpr, aliasOverrides)
		newArgs = append(newArgs, argExpr)
	}
	return newArgs, nil
}

func appendParsedCallArgs(
	call *dst.CallExpr,
	r *rule.InstCallRule,
	newArgs []dst.Expr,
	aliasOverrides map[string]string,
) (bool, error) {
	if len(newArgs) == 0 {
		return false, nil
	}

	if !call.Ellipsis {
		call.Args = append(call.Args, newArgs...)
		return true, nil
	}

	// Ellipsis call: requires variadic_type
	if r.VariadicType == "" {
		return false, ex.Newf(
			"append_args on ellipsis call requires variadic_type to be set",
		)
	}

	if len(call.Args) == 0 {
		return false, ex.Newf("append_args on ellipsis call with no arguments")
	}

	varTypeExpr, err := parseGoTypeExpression(r.VariadicType)
	if err != nil {
		return false, ex.Wrapf(err, "failed to parse variadic_type %q", r.VariadicType)
	}
	replaceQualifierAliases(varTypeExpr, aliasOverrides)

	// Replace the spread arg with an IIFE that appends the new args before spreading.
	// call.Ellipsis remains true — the outer call is still a spread call.
	lastArg := call.Args[len(call.Args)-1]
	call.Args[len(call.Args)-1] = buildEllipsisIIFE(lastArg, varTypeExpr, newArgs)
	return true, nil
}

// matchesCallRule checks if a call expression matches the rule's criteria.
//
// Only qualified calls are supported: pkg.Function()
// The function_call rule must specify the full import path: "package/path.FunctionName"
//
// Examples in source code:
//   - http.Get() after "import 'net/http'" matches "net/http.Get"
//   - redis.Get() after "import redis 'github.com/redis/go-redis/v9'" matches "github.com/redis/go-redis/v9.Get"
//   - sql.Open() after "import 'database/sql'" matches "database/sql.Open"
//
// What does NOT match:
//   - Get() without package qualifier (unqualified calls not supported)
//   - other.Get() where other is from a different package
func matchesCallRule(call *dst.CallExpr, r *rule.InstCallRule, importAliases map[string]string) bool {
	// Use pre-parsed fields - no parsing needed!
	importPath := r.ImportPath
	funcName := r.FuncName

	// Only match qualified calls: pkg.Function()
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return false
	}

	// Check function name matches
	if sel.Sel.Name != funcName {
		return false
	}

	// Check that the package identifier is a simple identifier (not a chained selector)
	ident, ok := sel.X.(*dst.Ident)
	if !ok {
		return false
	}

	// Check that the package's import path matches the rule's import path.
	pkgPath := ident.Path
	if pkgPath != "" {
		return pkgPath == importPath
	}

	resolvedPath, ok := importAliases[ident.Name]
	return ok && resolvedPath == importPath
}

// buildEllipsisIIFE constructs the IIFE that appends new args to a spread argument:
//
//	func(v ...VariadicType) []VariadicType { return append(v, newArgs...) }(spreadArg...)
func buildEllipsisIIFE(spreadArg, varType dst.Expr, newArgs []dst.Expr) *dst.CallExpr {
	param := &dst.Field{
		Names: []*dst.Ident{{Name: "v"}},
		Type:  &dst.Ellipsis{Elt: util.AssertType[dst.Expr](dst.Clone(varType))},
	}

	returnType := &dst.ArrayType{Elt: util.AssertType[dst.Expr](dst.Clone(varType))}

	appendArgs := make([]dst.Expr, 0, 1+len(newArgs))
	appendArgs = append(appendArgs, &dst.Ident{Name: "v"})
	appendArgs = append(appendArgs, newArgs...)

	appendCall := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "append"},
		Args: appendArgs,
	}

	funcLit := &dst.FuncLit{
		Type: &dst.FuncType{
			Params:  &dst.FieldList{List: []*dst.Field{param}},
			Results: &dst.FieldList{List: []*dst.Field{{Type: returnType}}},
		},
		Body: &dst.BlockStmt{
			List: []dst.Stmt{&dst.ReturnStmt{Results: []dst.Expr{appendCall}}},
		},
	}

	return &dst.CallExpr{
		Fun:      funcLit,
		Args:     []dst.Expr{spreadArg},
		Ellipsis: true,
	}
}

// applyIgnoredCallSites brackets every call whose enclosing statement
// carries //otelc:ignore, skipping a statement a wrap_call rule already
// consumed in this file. It reports whether it changed root.
func (ip *instrumentPhase) applyIgnoredCallSites(ctx context.Context, root *dst.File) (bool, error) {
	marked := ip.markedIgnoredStmts(root)
	if len(marked) == 0 {
		return false, nil
	}

	var blocks []*dst.BlockStmt
	dst.Inspect(root, func(n dst.Node) bool {
		if block, ok := n.(*dst.BlockStmt); ok {
			blocks = append(blocks, block)
		}
		return true
	})

	selfPackage := ip.isSuppressHooksPackage()

	bracketed := 0
	for _, block := range blocks {
		n, err := bracketMarkedStmts(block, marked, selfPackage)
		if err != nil {
			return false, err
		}
		bracketed += n
	}
	if bracketed == 0 {
		return false, nil
	}

	if selfPackage {
		return true, nil
	}
	suppressImport := map[string]string{suppressHooksPackage: suppressHooksPackage}
	if err := ip.addRuleImports(ctx, root, suppressImport, "otelc:ignore"); err != nil {
		return false, err
	}
	return true, nil
}

// markedIgnoredStmts returns statements in root carrying //otelc:ignore
// above a call, excluding ones already consumed by a wrap_call rule.
func (ip *instrumentPhase) markedIgnoredStmts(root *dst.File) map[dst.Stmt]bool {
	marked := make(map[dst.Stmt]bool)
	for _, stmt := range ast.CallEnclosingStmts(root) {
		if stmt == nil || ip.consumedIgnoreStmts[stmt] {
			continue
		}
		if ast.HasLeadingDirective(stmt, util.DirectiveIgnore) {
			marked[stmt] = true
		}
	}
	return marked
}

// markIgnoreConsumed marks stmt's //otelc:ignore comment as already handled
// by a wrap_call rule.
func (ip *instrumentPhase) markIgnoreConsumed(stmt dst.Stmt) {
	if stmt == nil {
		return
	}
	if ip.consumedIgnoreStmts == nil {
		ip.consumedIgnoreStmts = make(map[dst.Stmt]bool)
	}
	ip.consumedIgnoreStmts[stmt] = true
}

// bracketMarkedStmts brackets every statement in block.List that appears in
// marked and reports how many it bracketed. selfPackage decides whether the
// inserted calls need a "runtime" qualifier.
func bracketMarkedStmts(block *dst.BlockStmt, marked map[dst.Stmt]bool, selfPackage bool) (int, error) {
	bracketed := 0
	for i := 0; i < len(block.List); i++ {
		stmt := block.List[i]
		if !marked[stmt] {
			continue
		}
		if hasEscapingControlFlow(stmt) {
			return 0, ex.Newf(
				"the statement above //otelc:ignore returns, breaks, continues, or jumps out of " +
					"its enclosing block; the suppression placed after it would not always run. " +
					"Assign the call's result to a variable in its own statement, then use the " +
					"variable in the control-flow statement on its own, unannotated line")
		}
		inc := ast.ExprStmt(suppressHooksCall(suppressHooksFuncName, selfPackage))
		dec := ast.ExprStmt(suppressHooksCall(unsuppressHooksFuncName, selfPackage))
		block.List = append(block.List[:i], append([]dst.Stmt{inc}, block.List[i:]...)...)
		i++
		block.List = append(block.List[:i+1], append([]dst.Stmt{dec}, block.List[i+1:]...)...)
		i++
		bracketed++
	}
	return bracketed, nil
}

// hasEscapingControlFlow reports whether stmt's subtree holds a return,
// break, continue, or goto outside a nested function literal.
func hasEscapingControlFlow(stmt dst.Stmt) bool {
	found := false
	dst.Inspect(stmt, func(n dst.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *dst.FuncLit:
			return false
		case *dst.ReturnStmt, *dst.BranchStmt:
			found = true
			return false
		}
		return true
	})
	return found
}
