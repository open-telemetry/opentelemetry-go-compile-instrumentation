// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

// applyCallRule transforms function calls at call sites by wrapping them with
// instrumentation code according to the provided template.
func (ip *InstrumentPhase) applyCallRule(r *rule.InstCallRule, root *dst.File) error {
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

	// Now wrap each matching call
	for _, call := range matchingCalls {
		if err := wrapCall(call, r); err != nil {
			// Log but continue processing other calls
			ip.Warn("Failed to wrap call", "error", err)
			continue
		}
		modified = true
	}

	if modified {
		ip.Info("Apply call rule", "rule", r)
	}

	return nil
}

// wrapCall applies the template transformation to wrap the original call.
func wrapCall(call *dst.CallExpr, r *rule.InstCallRule) error {
	// Get the compiled template from the rule
	tmpl := r.CompiledTemplate
	if tmpl == nil {
		return ex.Newf("rule has no compiled template")
	}

	// Use the template to compile the wrapped expression
	wrappedExpr, err := tmpl.CompileExpression(call)
	if err != nil {
		return ex.Wrapf(err, "failed to compile template")
	}

	// Verify we got a call expression back
	wrappedCall, ok := wrappedExpr.(*dst.CallExpr)
	if !ok {
		return ex.Newf("template did not produce a call expression, got %T", wrappedExpr)
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
