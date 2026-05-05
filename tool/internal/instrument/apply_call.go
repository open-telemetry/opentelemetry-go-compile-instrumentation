// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// applyCallRule transforms function calls at call sites by wrapping them with
// instrumentation code according to the provided template.
func (ip *InstrumentPhase) applyCallRule(ctx context.Context, r *rule.InstCallRule, root *dst.File) error {
	importAliases := collectImportAliases(root)

	appendModified := ip.applyCallAppendArgs(r, root, importAliases)

	templateModified := false
	if r.Template != "" {
		var err error
		templateModified, err = ip.applyCallTemplate(r, root, importAliases)
		if err != nil {
			return err
		}
	}

	util.Assert(appendModified || templateModified, "call rule did not match any call")

	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}
	ip.Info("Apply call rule", "rule", r)

	return nil
}

// applyCallTemplate applies template wrapping to all matching calls in root using a
// two-pass approach to avoid re-matching wrapped nodes.
// Returns true if any replacement was made.
func (ip *InstrumentPhase) applyCallTemplate(
	r *rule.InstCallRule,
	root *dst.File,
	importAliases map[string]string,
) (bool, error) {
	tmpl, err := newCallTemplate(r.Template)
	if err != nil {
		return false, ex.Wrapf(err, "rule has no compiled template")
	}

	// Pass 1: collect matching calls and pre-compute replacements to avoid
	// re-matching the original call pointer inside its own wrapper.
	replacements := make(map[*dst.CallExpr]dst.Expr)
	dst.Inspect(root, func(node dst.Node) bool {
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}
		if !matchesCallRule(call, r, importAliases) {
			return true
		}
		wrapped, wrapErr := tmpl.compileExpression(call)
		if wrapErr != nil {
			ip.Warn("Failed to wrap call", "error", wrapErr)
			return true
		}
		replacements[call] = util.AssertType[dst.Expr](dst.Clone(wrapped))
		return true
	})

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

func (ip *InstrumentPhase) applyCallAppendArgs(
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

	return true
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
