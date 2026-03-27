// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// applyCallRule transforms function calls at call sites by wrapping them with
// instrumentation code according to the provided template.
func (ip *InstrumentPhase) applyCallRule(ctx context.Context, r *rule.InstCallRule, root *dst.File) error {
	modified := false
	importAliases := collectImportAliases(root)

	// Collect all matching calls first to avoid infinite recursion when wrapping
	var matchingCalls []*dst.CallExpr
	dst.Inspect(root, func(node dst.Node) bool {
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}

		// Check if this call matches our rule
		if matchesCallRule(call, r, importAliases) {
			matchingCalls = append(matchingCalls, call)
		}
		return true
	})

	// Now apply each matching call
	for _, call := range matchingCalls {
		appended, err := appendCallArgs(call, r)
		if err != nil {
			ip.Warn("Failed to append args to call", "error", err)
			continue
		}

		if r.Template != "" {
			if err = wrapCall(call, r); err != nil {
				ip.Warn("Failed to wrap call", "error", err)
				continue
			}
		}

		if appended || r.Template != "" {
			modified = true
		}
	}

	if modified {
		if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
			return err
		}
		ip.Info("Apply call rule", "rule", r)
	}

	return nil
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
		return false, ex.Newf("append_args on ellipsis call requires variadic_type; see docs/rules.md#append_args")
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

// wrapCall applies the template transformation to wrap the original call.
func wrapCall(call *dst.CallExpr, r *rule.InstCallRule) error {
	tmpl, err := newCallTemplate(r.Template)
	if err != nil {
		return ex.Wrapf(err, "rule has no compiled template")
	}

	// Use the template to compile the wrapped expression
	wrappedExpr, err := tmpl.compileExpression(call)
	if err != nil {
		return ex.Wrapf(err, "failed to compile template")
	}

	// Verify we got a call expression back
	wrappedCall, ok := wrappedExpr.(*dst.CallExpr)
	if !ok {
		return ex.Newf(
			"template output must be a call expression (e.g. \"wrapper({{ . }})\") but got %T; see docs/rules.md for supported template patterns",
			wrappedExpr,
		)
	}

	// Clone the wrapped expression to avoid decoration conflicts
	cloned := dst.Clone(wrappedCall)
	clonedCall, ok := cloned.(*dst.CallExpr)
	if !ok {
		return ex.Newf("clone result is not a CallExpr: got %T", cloned)
	}

	// Replace the original call with the wrapped version
	// Copy fields to preserve the original call's position in the AST
	call.Fun = clonedCall.Fun
	call.Args = clonedCall.Args
	call.Ellipsis = clonedCall.Ellipsis
	call.Decs = clonedCall.Decs

	return nil
}
