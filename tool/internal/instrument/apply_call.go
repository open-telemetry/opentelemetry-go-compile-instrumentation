// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// applyCallRule transforms function calls at call sites by wrapping them with
// instrumentation code according to the provided replacement template.
func (ip *InstrumentPhase) applyCallRule(ctx context.Context, r *rule.InstCallRule, root *dst.File) error {
	importAliases := collectImportAliases(root)

	appendModified := ip.applyCallAppendArgs(r, root, importAliases)

	replaceModified := false
	if r.Replace != "" {
		var err error
		replaceModified, err = ip.applyCallReplace(r, root, importAliases)
		if err != nil {
			return err
		}
	}

	util.Assert(appendModified || replaceModified, "call rule did not match any call")

	if err := ip.applyCallRuleHelpers(ctx, r, root); err != nil {
		return err
	}

	if err := ip.addRuleImports(ctx, root, r.Imports, r.Name); err != nil {
		return err
	}
	ip.Info("Apply call rule", "rule", r)

	return nil
}

// applyCallReplace applies replacement wrapping to all matching calls in root using a
// two-pass approach to avoid re-matching wrapped nodes.
// Returns true if any replacement was made.
func (*InstrumentPhase) applyCallReplace(
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
	dst.Inspect(root, func(node dst.Node) bool {
		if wrapError != nil {
			return false
		}
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}
		if !matchesCallRule(call, r, importAliases) {
			return true
		}
		wrapped, wrapErr := tmpl.compileExpression(call)
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

func (ip *InstrumentPhase) applyCallRuleHelpers(
	ctx context.Context,
	r *rule.InstCallRule,
	root *dst.File,
) error {
	if strings.TrimSpace(r.Path) == "" {
		return nil
	}

	helperNames, err := callRuleHelperNames(r)
	if err != nil {
		return err
	}
	removeLocalFuncNames(helperNames, root)
	if len(helperNames) == 0 {
		return nil
	}

	files, err := callRuleHelperFiles(r.Path, helperNames)
	if err != nil {
		return ex.Wrapf(err, "finding helper files for call rule %s at path %s", r.Name, r.Path)
	}
	for _, file := range files {
		if err = ip.addCallRuleHelperFile(ctx, r, file, root.Name.Name); err != nil {
			return err
		}
	}
	return nil
}

func callRuleHelperNames(r *rule.InstCallRule) (map[string]bool, error) {
	names := make(map[string]bool)
	if strings.TrimSpace(r.Replace) != "" {
		tmpl, err := newCallTemplate(r.Replace)
		if err != nil {
			return nil, err
		}
		expr, err := tmpl.compileExpression(&dst.CallExpr{
			Fun: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "pkg", Path: r.ImportPath},
				Sel: &dst.Ident{Name: r.FuncName},
			},
		})
		if err != nil {
			return nil, err
		}
		collectUnqualifiedCallNames(expr, names)
	}
	for _, arg := range r.AppendArgs {
		expr, err := parseGoExpression(arg)
		if err != nil {
			return nil, err
		}
		collectUnqualifiedCallNames(expr, names)
	}
	return names, nil
}

func collectUnqualifiedCallNames(node dst.Node, names map[string]bool) {
	dst.Inspect(node, func(n dst.Node) bool {
		call, ok := n.(*dst.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*dst.Ident)
		if !ok || isBuiltinCall(ident.Name) {
			return true
		}
		names[ident.Name] = true
		return true
	})
}

func isBuiltinCall(name string) bool {
	switch name {
	case "append", "cap", "clear", "close", "complex", "copy", "delete", "imag",
		"len", "make", "max", "min", "new", "panic", "print", "println", "real", "recover":
		return true
	default:
		return false
	}
}

func removeLocalFuncNames(names map[string]bool, root *dst.File) {
	for _, decl := range root.Decls {
		funcDecl, ok := decl.(*dst.FuncDecl)
		if !ok || funcDecl.Recv != nil {
			continue
		}
		delete(names, funcDecl.Name.Name)
	}
}

func callRuleHelperFiles(path string, names map[string]bool) ([]string, error) {
	files, err := listRuleFiles(path)
	if err != nil {
		return nil, err
	}

	remaining := make(map[string]bool, len(names))
	for name := range names {
		remaining[name] = true
	}

	var matched []string
	for _, file := range files {
		if !util.IsGoFile(file) {
			continue
		}
		root, err := ast.ParseFileFast(file)
		if err != nil {
			return nil, err
		}
		var found bool
		for _, decl := range root.Decls {
			funcDecl, ok := decl.(*dst.FuncDecl)
			if !ok || funcDecl.Recv != nil || !remaining[funcDecl.Name.Name] {
				continue
			}
			delete(remaining, funcDecl.Name.Name)
			found = true
		}
		if found {
			matched = append(matched, file)
		}
	}

	if len(remaining) != 0 {
		missing := mapsKeys(remaining)
		sort.Strings(missing)
		return nil, ex.Newf("helper function(s) %v not found", missing)
	}
	return matched, nil
}

func mapsKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

var nonIdentifierChars = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func callRuleHelperOutputName(ruleName, file string) string {
	base := filepath.Base(file)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	rulePart := nonIdentifierChars.ReplaceAllString(ruleName, "_")
	filePart := nonIdentifierChars.ReplaceAllString(name, "_")
	return fmt.Sprintf("otelc.%s.%s.go", rulePart, filePart)
}

func (ip *InstrumentPhase) addCallRuleHelperFile(
	ctx context.Context,
	r *rule.InstCallRule,
	file string,
	pkgName string,
) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return ex.Wrapf(err, "reading call rule helper file %s", file)
	}
	root, err := ast.NewAstParser().ParseSource(stripBuildIgnoreTag(string(data)))
	if err != nil {
		return ex.Wrapf(err, "parsing call rule helper file %s", file)
	}
	root.Name.Name = pkgName
	if err = ip.updateImportConfigForFile(ctx, root, r.Name); err != nil {
		return err
	}

	newFile := filepath.Join(ip.workDir, callRuleHelperOutputName(r.Name, file))
	if !util.PathExists(newFile) {
		if err = ast.WriteFile(newFile, root); err != nil {
			return ex.Wrapf(err, "writing call rule helper file %s", newFile)
		}
		ip.keepForDebug(newFile)
	}
	if !ip.hasCompileArg(newFile) {
		ip.addCompileArg(newFile)
	}
	ip.Info("Apply call rule helper file", "rule", r, "helper", file, "new", newFile)
	return nil
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
