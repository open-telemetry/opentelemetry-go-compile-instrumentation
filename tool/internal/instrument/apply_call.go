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
// instrumentation code according to the provided replacement template.
func (ip *instrumentPhase) applyCallRule(ctx context.Context, r *rule.InstCallRule, root *dst.File) error {
	importAliases := ast.ImportAliasMap(root)

	appendModified := ip.applyCallAppendArgs(r, root, importAliases)

	replaceModified := false
	if r.Replace != "" {
		var err error
		replaceModified, err = ip.applyCallReplace(r, root, importAliases)
		if err != nil {
			return err
		}
	}

	if !appendModified && !replaceModified {
		return nil
	}

	if err := ip.addRuleImports(ctx, root, usedRuleImports(root, r.Imports), r.Name); err != nil {
		return err
	}
	ip.Info("Apply call rule", "rule", r)

	return nil
}

// usedRuleImports returns the subset of ruleImports whose alias is actually
// referenced somewhere in root. It must be called after the rule's append_args/replace
// modifications have already been applied to root.
//
// Blank ("_") and dot (".") aliases are always kept.
func usedRuleImports(root *dst.File, ruleImports map[string]string) map[string]string {
	if len(ruleImports) == 0 {
		return nil
	}

	used := make(map[string]string, len(ruleImports))
	for alias, path := range ruleImports {
		if alias == "_" || alias == "." {
			used[alias] = path
		}
	}

	dst.Inspect(root, func(node dst.Node) bool {
		sel, ok := node.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, identOk := sel.X.(*dst.Ident)
		if !identOk {
			return true
		}
		if path, importOk := ruleImports[ident.Name]; importOk {
			used[ident.Name] = path
		}
		return true
	})

	return used
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
func (*instrumentPhase) applyCallReplace(
	r *rule.InstCallRule,
	root *dst.File,
	importAliases map[string]string,
) (bool, error) {
	tmpl, err := newCallTemplate(r.Replace)
	if err != nil {
		return false, ex.Wrapf(err, "rule has no compiled replacement template")
	}

	// Pass 1: collect matching calls and pre-compute replacements to avoid
	// re-matching the original call pointer inside its own wrapper.
	replacements := make(map[*dst.CallExpr]dst.Expr)
	var wrapError error
	walkCallsWithEnclosingFunc(root, func(call *dst.CallExpr, enclosing *dst.FuncDecl) bool {
		if !matchesCallRule(call, r, importAliases) {
			return true
		}
		wrapped, wrapErr := tmpl.compileExpression(call, enclosing)
		if wrapErr != nil {
			wrapError = wrapErr
			return false
		}
		replacements[call] = util.AssertType[dst.Expr](dst.Clone(wrapped))
		return true
	})

	if wrapError != nil {
		return false, ex.Wrapf(wrapError, "failed to wrap matched call")
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

func (ip *instrumentPhase) applyCallAppendArgs(
	r *rule.InstCallRule,
	root *dst.File,
	importAliases map[string]string,
) bool {
	if len(r.AppendArgs) == 0 {
		return false
	}

	var matchingCalls []*dst.CallExpr
	dst.Inspect(root, func(node dst.Node) bool {
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}
		if matchesCallRule(call, r, importAliases) {
			matchingCalls = append(matchingCalls, call)
		}
		return true
	})
	for _, call := range matchingCalls {
		if _, err := appendCallArgs(call, r); err != nil {
			ip.Warn("Failed to append args to call", "error", err)
		}
	}

	return len(matchingCalls) > 0
}

// appendCallArgs appends the expressions from r.AppendArgs to the call's argument list.
// For ellipsis calls, an IIFE wrapper is generated using r.VariadicType.
// Returns (true, nil) if the call was modified, (false, nil) if AppendArgs is empty.
func appendCallArgs(call *dst.CallExpr, r *rule.InstCallRule) (bool, error) {
	if len(r.AppendArgs) == 0 {
		return false, nil
	}

	// Parse all new argument expressions
	newArgs := make([]dst.Expr, 0, len(r.AppendArgs))
	for _, argStr := range r.AppendArgs {
		argExpr, err := parseGoExpression(argStr)
		if err != nil {
			return false, ex.Wrapf(err, "failed to parse append_args entry %q", argStr)
		}
		newArgs = append(newArgs, argExpr)
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

	// Replace the spread arg with an IIFE that appends the new args before spreading.
	// call.Ellipsis remains true — the outer call is still a spread call.
	lastArg := call.Args[len(call.Args)-1]
	call.Args[len(call.Args)-1] = buildEllipsisIIFE(lastArg, varTypeExpr, newArgs)
	return true, nil
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
