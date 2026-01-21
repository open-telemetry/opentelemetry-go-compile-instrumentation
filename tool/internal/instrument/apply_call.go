// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"strings"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
)

// applyCallRule transforms function calls at call sites by wrapping them with
// instrumentation code according to the provided template.
func (ip *InstrumentPhase) applyCallRule(r *rule.InstCallRule, root *dst.File) error {
	modified := false

	// Collect all matching calls first to avoid infinite recursion when wrapping
	var matchingCalls []*dst.CallExpr
	dst.Inspect(root, func(node dst.Node) bool {
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}

		// Check if this call matches our rule
		if matchesCallRule(call, r, root) {
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
		// Add any required imports
		addImportsFromRule(root, r)
		ip.Info("Apply call rule", "rule", r)
	}

	return nil
}

// matchesCallRule checks if a call expression matches the rule's criteria.
//
// Only qualified calls are supported: pkg.Function()
// The function-call rule must specify the full import path: "package/path.FunctionName"
//
// Examples in source code:
//   - http.Get() after "import 'net/http'" matches "net/http.Get"
//   - redis.Get() after "import redis 'github.com/redis/go-redis/v9'" matches "github.com/redis/go-redis/v9.Get"
//   - sql.Open() after "import 'database/sql'" matches "database/sql.Open"
//
// What does NOT match:
//   - Get() without package qualifier (unqualified calls not supported)
//   - other.Get() where other is from a different package
func matchesCallRule(call *dst.CallExpr, r *rule.InstCallRule, file *dst.File) bool {
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

	// Check that the package's import path matches the rule's import path
	// If Path is empty, try to resolve from imports
	pkgPath := ident.Path
	if pkgPath == "" {
		pkgPath = resolveImportPath(ident.Name, file)
	}

	return pkgPath == importPath
}

// resolveImportPath resolves the import path for a given package name by looking
// at the file's import declarations.
func resolveImportPath(pkgName string, file *dst.File) string {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		for _, spec := range genDecl.Specs {
			importSpec, isImport := spec.(*dst.ImportSpec)
			if !isImport {
				continue
			}
			path := strings.Trim(importSpec.Path.Value, `"`)
			// Determine the alias for this import
			alias := path[strings.LastIndex(path, "/")+1:] // Default: last part of path
			if importSpec.Name != nil {
				alias = importSpec.Name.Name
			}
			if alias == pkgName {
				return path
			}
		}
	}
	return ""
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

// addImportsFromRule adds any imports specified in the rule to the file.
func addImportsFromRule(root *dst.File, r *rule.InstCallRule) {
	if len(r.Imports) == 0 {
		return
	}

	// Check existing imports to avoid duplicates
	existingImports := make(map[string]string) // path -> alias
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		for _, spec := range genDecl.Specs {
			importSpec, isImport := spec.(*dst.ImportSpec)
			if !isImport {
				continue
			}
			path := strings.Trim(importSpec.Path.Value, `"`)
			alias := ""
			if importSpec.Name != nil {
				alias = importSpec.Name.Name
			}
			existingImports[path] = alias
		}
	}

	// Add new imports that don't already exist
	for alias, path := range r.Imports {
		if existingAlias, exists := existingImports[path]; exists {
			// Import already exists, check if alias matches
			if existingAlias != alias && alias != "" {
				// Different alias - this might cause issues but we'll let it be
				continue
			}
			continue
		}

		// Add the import
		importDecl := ast.ImportDecl(alias, path)
		root.Decls = append([]dst.Decl{importDecl}, root.Decls...)
	}
}
