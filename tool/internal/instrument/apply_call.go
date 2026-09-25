// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"
	"golang.org/x/tools/go/gcexportdata"

	"go.opentelemetry.io/otelc/tool/ex"
	toolast "go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// applyCallRule transforms function calls at call sites by wrapping them with
// instrumentation code according to the provided replacement template.
func (ip *instrumentPhase) applyCallRule(ctx context.Context, r *rule.InstCallRule, root *dst.File) error {
	importAliases := toolast.ImportAliasMap(root)

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
func (ip *instrumentPhase) applyCallReplace(
	r *rule.InstCallRule,
	root *dst.File,
	importAliases map[string]string,
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
		if !ip.matchesRule(call, r, importAliases) {
			return true
		}
		wrapped, wrapErr := tmpl.compileExpression(call, enclosing, importAliases)
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
		if ip.matchesRule(call, r, importAliases) {
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

// matchesRule dispatches by selector: function_call or method_call.
func (ip *instrumentPhase) matchesRule(call *dst.CallExpr, r *rule.InstCallRule, importAliases map[string]string) bool {
	if r.MethodCall != "" {
		return ip.matchesMethodCallRule(call, r)
	}
	return matchesCallRule(call, r, importAliases)
}

// matchesMethodCallRule ensures that files that never mention the method
// name skip type-checking entirely.
func (ip *instrumentPhase) matchesMethodCallRule(call *dst.CallExpr, r *rule.InstCallRule) bool {
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok || sel.Sel.Name != r.FuncName {
		return false
	}

	info := ip.ensureMethodCallInfo()
	if info == nil {
		return false
	}

	pos := ip.parser.FindPosition(sel.Sel)
	if pos.Filename == "" {
		return false
	}

	importPath, recvType, ok := info.methodReceiver(pos.Filename, pos.Line, pos.Column)
	return ok && importPath == r.ImportPath && recvType == r.RecvType
}

// ensureMethodCallInfo type-checks the package at most once.
func (ip *instrumentPhase) ensureMethodCallInfo() *methodCallPackageInfo {
	if ip.methodCallInfoLoaded {
		return ip.methodCallInfo
	}
	ip.methodCallInfoLoaded = true

	pkgPath := util.FindFlagValue(ip.compileArgs, "-p")
	files := packageSourceFiles(ip.compileArgs)
	info, err := checkPackageForMethodCalls(pkgPath, files, ip.importConfig)
	if err != nil {
		ip.Warn("method_call type-checking failed; method_call rules will not match in this package",
			"package", pkgPath, "error", err)
		return nil
	}

	ip.methodCallInfo = info
	return ip.methodCallInfo
}

// packageSourceFiles returns the compile command's source files
func packageSourceFiles(compileArgs []string) []string {
	var files []string
	for _, arg := range compileArgs {
		if strings.HasPrefix(arg, "-") || !util.IsGoFile(arg) {
			continue
		}
		abs, err := filepath.Abs(arg)
		if err != nil {
			continue
		}
		files = append(files, abs)
	}
	return files
}

// methodCallPosition keys the dst/ast bridge by file base name, line, and
// column, since both parses read the same source bytes.
type methodCallPosition struct {
	file string
	line int
	col  int
}

// methodCallPackageInfo is the result of type-checking one package for
// method_call matching.
type methodCallPackageInfo struct {
	selByPos map[methodCallPosition]*types.Selection
}

// checkPackageForMethodCalls type-checks files in pkgPath.
func checkPackageForMethodCalls(
	pkgPath string,
	files []string,
	cfg imports.ImportConfig,
) (*methodCallPackageInfo, error) {
	fset := token.NewFileSet()
	astFiles := make([]*ast.File, 0, len(files))
	for _, path := range files {
		astFile, err := parseForTypeCheck(fset, path)
		if err != nil {
			return nil, err
		}
		astFiles = append(astFiles, astFile)
	}

	info := &types.Info{
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	tcfg := &types.Config{
		Importer: newExportImporter(fset, cfg.PackageFile),
		Error:    func(error) {}, // Type errors are expected and ignored here.
	}
	_, _ = tcfg.Check(pkgPath, fset, astFiles, info)

	pi := &methodCallPackageInfo{selByPos: make(map[methodCallPosition]*types.Selection, len(info.Selections))}
	for sel, selection := range info.Selections {
		pos := fset.Position(sel.Sel.Pos())
		pi.selByPos[methodCallPosition{file: pos.Filename, line: pos.Line, col: pos.Column}] = selection
	}
	return pi, nil
}

// parseForTypeCheck records position filenames as filepath.Base(path)
func parseForTypeCheck(fset *token.FileSet, path string) (*ast.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ex.Wrapf(err, "opening %s for type-checking", path)
	}
	defer f.Close()

	astFile, err := parser.ParseFile(fset, filepath.Base(path), f, parser.SkipObjectResolution)
	if err != nil {
		return nil, ex.Wrapf(err, "parsing %s for type-checking", path)
	}
	return astFile, nil
}

// methodReceiver resolves the method at the selector position, the file,
// line, and column of its method-name identifier. It returns the receiver's
// import path, its type name, and whether a method was resolved. A leading
// "*" on the type name marks a pointer receiver.
//
//nolint:revive // confusing-results conflicts with nonamedreturns
func (pi *methodCallPackageInfo) methodReceiver(file string, line, col int) (string, string, bool) {
	selection, found := pi.selByPos[methodCallPosition{file: file, line: line, col: col}]
	if !found {
		return "", "", false
	}

	// Only bound method calls (recv.Method(...)) should match
	// Exclude method expressions (Type.Method(recv, ...))
	if selection.Kind() != types.MethodVal {
		return "", "", false
	}

	fn, isFunc := selection.Obj().(*types.Func)
	if !isFunc {
		return "", "", false
	}
	sig, isSig := fn.Type().(*types.Signature)
	if !isSig || sig.Recv() == nil {
		return "", "", false
	}

	recvT := sig.Recv().Type()
	pointer := false
	if ptr, isPtr := recvT.(*types.Pointer); isPtr {
		pointer = true
		recvT = ptr.Elem()
	}

	named, isNamed := recvT.(*types.Named)
	if !isNamed || named.Obj() == nil || named.Obj().Pkg() == nil {
		return "", "", false
	}

	recvType := named.Obj().Name()
	if pointer {
		recvType = "*" + recvType
	}
	return named.Obj().Pkg().Path(), recvType, true
}

// exportImporter reads a dependency's own compiled .a file.
type exportImporter struct {
	fset     *token.FileSet
	archives map[string]string // import path -> .a file, from -importcfg
	packages map[string]*types.Package
}

func newExportImporter(fset *token.FileSet, archives map[string]string) *exportImporter {
	return &exportImporter{
		fset:     fset,
		archives: archives,
		packages: make(map[string]*types.Package),
	}
}

// Import implements types.Importer.
func (imp *exportImporter) Import(path string) (*types.Package, error) {
	return imp.ImportFrom(path, "", 0)
}

// ImportFrom implements types.ImporterFrom.
func (imp *exportImporter) ImportFrom(path, _ string, _ types.ImportMode) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if pkg, ok := imp.packages[path]; ok && pkg.Complete() {
		return pkg, nil
	}

	archive, ok := imp.archives[path]
	if !ok {
		return nil, ex.Newf("no archive for import path %q in -importcfg", path)
	}

	f, err := os.Open(archive)
	if err != nil {
		return nil, ex.Wrapf(err, "opening archive for %q", path)
	}
	defer f.Close()

	r, err := gcexportdata.NewReader(f)
	if err != nil {
		return nil, ex.Wrapf(err, "reading export data section for %q from %s", path, archive)
	}

	pkg, err := gcexportdata.Read(r, imp.fset, imp.packages, path)
	if err != nil {
		return nil, ex.Wrapf(err, "decoding export data for %q from %s", path, archive)
	}
	return pkg, nil
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
