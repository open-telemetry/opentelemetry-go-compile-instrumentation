// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// loadCallRuleAuxFiles loads auxiliary Go files from call rule paths into the
// compilation. This ensures that wrapper functions referenced in replace
// templates (e.g. "Wrapper({{ . }})") are available in the target package. Only
// files with the //go:build ignore tag are loaded, following the same convention
// used by hook files in this project.
func (ip *InstrumentPhase) loadCallRuleAuxFiles(ctx context.Context, rset *rule.InstRuleSet) error {
	loaded := make(map[string]bool)
	for _, callRules := range rset.CallRules {
		for _, r := range callRules {
			if r.Path == "" || loaded[r.Path] {
				continue
			}
			loaded[r.Path] = true
			if err := ip.loadAuxFilesFromPath(ctx, r.Path, r.Name, rset.PackageName); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadAuxFilesFromPath finds Go files at the given path that have the
// //go:build ignore tag, strips the tag, renames the package, and adds
// them to the compilation. This follows the same pattern as applyFileRule.
func (ip *InstrumentPhase) loadAuxFilesFromPath(
	ctx context.Context,
	rulePath, ruleName, pkgName string,
) error {
	files, err := listRuleFiles(rulePath)
	if err != nil {
		return ex.Wrapf(err, "listing aux files for call rule %s at path %s", ruleName, rulePath)
	}

	for _, file := range files {
		if !util.IsGoFile(file) {
			continue
		}
		if err1 := ip.loadOneAuxFile(ctx, file, ruleName, pkgName); err1 != nil {
			return err1
		}
	}
	return nil
}

// loadOneAuxFile loads a single auxiliary Go file into the compilation if it
// carries the //go:build ignore tag.
func (ip *InstrumentPhase) loadOneAuxFile(
	ctx context.Context,
	file, ruleName, pkgName string,
) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return ex.Wrapf(err, "reading call rule aux file %s", file)
	}
	content := string(data)
	// Only load files marked with //go:build ignore to avoid pulling in
	// test source files or other non-hook code at the same path.
	if !strings.Contains(content, "//go:build ignore") {
		return nil
	}
	root, err := ast.NewAstParser().ParseSource(stripBuildIgnoreTag(content))
	if err != nil {
		return ex.Wrapf(err, "parsing call rule aux file %s", file)
	}
	root.Name.Name = pkgName

	if err = ip.updateImportConfigForFile(ctx, root, ruleName); err != nil {
		return err
	}

	base := filepath.Base(file)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	// Include a stable suffix to prevent collisions when multiple call rules
	// at different paths both contain a file with the same basename.
	suffix := util.CRC32(ruleName + ":" + file)
	newFile := filepath.Join(ip.workDir, fmt.Sprintf("otelc.%s.%s.go", suffix, stem))
	if err = ast.WriteFile(newFile, root); err != nil {
		return ex.Wrapf(err, "writing call rule aux file %s", newFile)
	}
	ip.addCompileArg(newFile)
	ip.keepForDebug(newFile)
	ip.Info("Load call rule aux file", "rule", ruleName, "file", newFile)
	return nil
}

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
