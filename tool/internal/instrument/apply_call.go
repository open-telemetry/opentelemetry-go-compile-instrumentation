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
	importAliases := ast.ImportAliasMap(root, ip.importNames)

	// The target file can already import a rule path under an alias the
	// rule does not expect. aliasOverrides holds the file's alias for
	// each such path, derived from importAliases.
	existingAliases := make(map[string]string, len(importAliases))
	for alias, path := range importAliases {
		existingAliases[path] = alias
	}
	aliasOverrides := resolveAliasOverrides(r.Imports, existingAliases)

	appendModified := ip.applyCallAppendArgs(r, root, importAliases, aliasOverrides)

	replaceModified := false
	if r.Replace != "" {
		var err error
		replaceModified, err = ip.applyCallReplace(r, root, importAliases, aliasOverrides)
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
	aliasOverrides map[string]string,
) (bool, error) {
	tmpl, err := newCallTemplate(r.Replace)
	if err != nil {
		return false, err
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
		replaceQualifierAliases(wrapped, aliasOverrides)
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

func (ip *instrumentPhase) applyCallAppendArgs(
	r *rule.InstCallRule,
	root *dst.File,
	importAliases map[string]string,
	aliasOverrides map[string]string,
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
		if _, err := appendCallArgs(call, r, aliasOverrides); err != nil {
			ip.Warn("Failed to append args to call", "error", err)
		}
	}

	return len(matchingCalls) > 0
}

// appendCallArgs appends the expressions from r.AppendArgs to the call's argument list.
// For ellipsis calls, an IIFE wrapper is generated using r.VariadicType.
// Returns (true, nil) if the call was modified, (false, nil) if AppendArgs is empty.
func appendCallArgs(call *dst.CallExpr, r *rule.InstCallRule, aliasOverrides map[string]string) (bool, error) {
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
		replaceQualifierAliases(argExpr, aliasOverrides)
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

// resolveAliasOverrides reports the alias to substitute for each rule
// import already present in the target file under a different alias.
// Substituting the file's alias into generated code, instead of the
// rule's alias, avoids a build failure.
//
// existingAliases must resolve an unaliased import to its real name,
// not a guess. A guessed name can be illegal as a Go identifier.
//
// The dot alias and the blank alias are exempt from substitution.
func resolveAliasOverrides(ruleImports, existingAliases map[string]string) map[string]string {
	var overrides map[string]string
	for ruleAlias, importPath := range ruleImports {
		if ruleAlias == "." || ruleAlias == "_" {
			continue
		}
		existingAlias, ok := existingAliases[importPath]
		if !ok || existingAlias == ruleAlias || existingAlias == "." || existingAlias == "_" {
			continue
		}
		if overrides == nil {
			overrides = make(map[string]string, len(ruleImports))
		}
		overrides[ruleAlias] = existingAlias
	}
	return overrides
}

// replaceQualifierAliases rewrites a freshly generated expression to
// use the file's alias for an import, instead of the rule's alias.
// overrides maps each rule alias to the file's alias.
//
// replaceQualifierAliases only touches the node passed in. The rewrite
// cannot reach unrelated code that shares an identifier name.
func replaceQualifierAliases(node dst.Node, overrides map[string]string) {
	if len(overrides) == 0 {
		return
	}
	dst.Inspect(node, func(n dst.Node) bool {
		sel, ok := n.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if existingAlias, override := overrides[ident.Name]; override {
			ident.Name = existingAlias
		}
		return true
	})
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
