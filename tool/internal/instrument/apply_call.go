// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"strings"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

// findNewImports determines which imports from the rule are not already present in the file.
func findNewImports(root *dst.File, ruleImports map[string]string) map[string]string {
	if len(ruleImports) == 0 {
		return nil
	}

	// Get existing imports
	existingImports := make(map[string]bool)
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		for _, spec := range genDecl.Specs {
			importSpec, isImport := spec.(*dst.ImportSpec)
			if !isImport || importSpec.Path == nil {
				continue
			}
			path := strings.Trim(importSpec.Path.Value, `"`)
			existingImports[path] = true
		}
	}

	// Find new imports
	newImports := make(map[string]string)
	for alias, importPath := range ruleImports {
		if !existingImports[importPath] {
			newImports[alias] = importPath
		}
	}

	return newImports
}

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
		/*// Track which imports are new
		newImports := findNewImports(root, r.Imports)

		// Add imports to AST
		addImportsFromRule(root, r)

		// Update importcfg for new imports
		if len(newImports) > 0 {
			if err := ip.updateImportConfig(newImports); err != nil {
				return err
			}
		}*/

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
