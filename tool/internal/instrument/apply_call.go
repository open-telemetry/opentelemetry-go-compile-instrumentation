// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	goast "go/ast"
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
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// applyCallRule transforms function calls at call sites by wrapping them with
// instrumentation code according to the provided replacement template. It
// reports whether it changed root.
func (ip *instrumentPhase) applyCallRule(ctx context.Context, r *rule.InstCallRule, root *dst.File) (bool, error) {
	importAliases, aliasOverrides := ip.resolveImportOverrides(root, r.Imports)

	appendModified, err := ip.applyCallAppendArgs(r, root, importAliases, aliasOverrides)
	if err != nil {
		return false, err
	}

	replaceModified := false
	if r.Replace != "" {
		replaceModified, err = ip.applyCallReplace(r, root, importAliases, aliasOverrides)
		if err != nil {
			return false, err
		}
	}

	if !appendModified && !replaceModified {
		return false, nil
	}

	if err = ip.addRuleImports(ctx, root, usedRuleImports(root, r.Imports, aliasOverrides), r.Name); err != nil {
		return false, err
	}
	ip.Info("Apply call rule", "rule", r)

	return true, nil
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
//
// A //otelc:ignore comment above a matching call's enclosing statement opts
// that one call site out and leaves every other matching call site wrapped.
func (ip *instrumentPhase) applyCallReplace(
	r *rule.InstCallRule,
	root *dst.File,
	importAliases map[string]string,
	aliasOverrides map[string]string,
) (bool, error) {
	tmpl, err := newCallTemplate(r.Replace)
	if err != nil {
		return false, err
	}

	stmts := ast.CallEnclosingStmts(root)

	// Pass 1: collect matching calls and pre-compute replacements to avoid
	// re-matching the original call pointer inside its own wrapper.
	replacements := make(map[*dst.CallExpr]dst.Expr)
	var wrapError error
	walkCallsWithEnclosingFunc(root, func(call *dst.CallExpr, enclosing *dst.FuncDecl) bool {
		if !ip.matchesRule(call, r, importAliases) {
			return true
		}
		if ast.HasLeadingDirective(stmts[call], util.DirectiveIgnore) {
			ip.Debug("Skip call site due to //otelc:ignore", "rule", r.Name)
			return true
		}
		if shadowErr := checkAliasOverrideShadowing(aliasOverrides, enclosing); shadowErr != nil {
			wrapError = shadowErr
			return false
		}
		wrapped, wrapErr := tmpl.compileExpression(call, enclosing, importAliases, aliasOverrides)
		if wrapErr != nil {
			wrapError = wrapErr
			return false
		}
		cloned := util.AssertType[dst.Expr](dst.Clone(wrapped))
		// dst.Clone gives the wrapped call's nodes fresh pointers absent from the
		// parser's position map
		if ip.parser != nil {
			ip.parser.PropagatePositions(wrapped, cloned)
		}
		replacements[call] = cloned
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

// applyCallAppendArgs appends r's extra arguments to every call site that
// matches r. A //otelc:ignore comment above a matching call's enclosing
// statement opts that one call site out and leaves every other matching call
// site appended.
func (ip *instrumentPhase) applyCallAppendArgs(
	r *rule.InstCallRule,
	root *dst.File,
	importAliases map[string]string,
	aliasOverrides map[string]string,
) (bool, error) {
	if len(r.AppendArgs) == 0 {
		return false, nil
	}

	newArgs, err := parseAppendArgs(r.AppendArgs, aliasOverrides)
	if err != nil {
		return false, err
	}

	stmts := ast.CallEnclosingStmts(root)
	var matchingCalls []*dst.CallExpr
	dst.Inspect(root, func(node dst.Node) bool {
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}
		if !ip.matchesRule(call, r, importAliases) {
			return true
		}
		if ast.HasLeadingDirective(stmts[call], util.DirectiveIgnore) {
			ip.Debug("Skip call site due to //otelc:ignore", "rule", r.Name)
			return true
		}
		matchingCalls = append(matchingCalls, call)
		return true
	})
	modified := false
	for _, call := range matchingCalls {
		callArgs := make([]dst.Expr, len(newArgs))
		for i, arg := range newArgs {
			callArgs[i] = util.AssertType[dst.Expr](dst.Clone(arg))
		}
		ok, appendErr := appendParsedCallArgs(call, r, callArgs, aliasOverrides)
		if appendErr != nil {
			return false, appendErr
		}
		modified = modified || ok
	}

	return modified, nil
}

// appendCallArgs appends the expressions from r.AppendArgs to the call's argument list.
// For ellipsis calls, an IIFE wrapper is generated using r.VariadicType.
// Returns (true, nil) if the call was modified, (false, nil) if AppendArgs is empty.
func appendCallArgs(call *dst.CallExpr, r *rule.InstCallRule) (bool, error) {
	newArgs, err := parseAppendArgs(r.AppendArgs, nil)
	if err != nil {
		return false, err
	}
	return appendParsedCallArgs(call, r, newArgs, nil)
}

func parseAppendArgs(args []string, aliasOverrides map[string]string) ([]dst.Expr, error) {
	newArgs := make([]dst.Expr, 0, len(args))
	for _, argStr := range args {
		argExpr, err := parseGoExpression(argStr)
		if err != nil {
			return nil, ex.Wrapf(err, "failed to parse append_args entry %q", argStr)
		}
		replaceQualifierAliases(argExpr, aliasOverrides)
		newArgs = append(newArgs, argExpr)
	}
	return newArgs, nil
}

func appendParsedCallArgs(
	call *dst.CallExpr,
	r *rule.InstCallRule,
	newArgs []dst.Expr,
	aliasOverrides map[string]string,
) (bool, error) {
	if len(newArgs) == 0 {
		return false, nil
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
		ip.Debug("method_call: call has no source position, skipping",
			"rule", r.Name, "method", r.FuncName)
		return false
	}

	importPath, recvType, ok := info.methodReceiver(pos.Filename, pos.Line, pos.Column)
	if !ok {
		ip.logUnresolvedMethodCall(r, pos, info)
		return false
	}
	return importPath == r.ImportPath && recvType == r.RecvType
}

// logUnresolvedMethodCall logs why a call to a method with the rule's name
// could not be resolved to a receiver type.
func (ip *instrumentPhase) logUnresolvedMethodCall(
	r *rule.InstCallRule,
	pos token.Position,
	info *methodCallPackageInfo,
) {
	args := []any{
		"rule", r.Name, "method", r.FuncName,
		"file", pos.Filename, "line", pos.Line, "column", pos.Column,
	}
	if info.firstTypeError != nil {
		args = append(args, "first_type_error", info.firstTypeError)
	}
	ip.Debug("method_call: call could not be resolved to a receiver type, rule will not match", args...)
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
	selByPos       map[methodCallPosition]*types.Selection
	firstTypeError error
}

// checkPackageForMethodCalls type-checks files in pkgPath.
func checkPackageForMethodCalls(
	pkgPath string,
	files []string,
	cfg imports.ImportConfig,
) (*methodCallPackageInfo, error) {
	fset := token.NewFileSet()
	astFiles := make([]*goast.File, 0, len(files))
	for _, path := range files {
		astFile, err := parseForTypeCheck(fset, path)
		if err != nil {
			return nil, err
		}
		astFiles = append(astFiles, astFile)
	}

	info := &types.Info{
		Selections: make(map[*goast.SelectorExpr]*types.Selection),
	}
	var firstTypeError error
	// Sizes is nil, so go/types uses SizesFor("gc", "amd64") — unsafe sizing
	// is checked with amd64 sizes on every other architecture. Mismatches only
	// surface as type errors, which degrade method_call matching, never break
	// the build. Set Sizes explicitly if the setup phase learns the target
	// GOARCH.
	tcfg := &types.Config{
		Importer: newExportImporter(fset, cfg.PackageFile, cfg.ImportMap),
		Error: func(err error) {
			if firstTypeError == nil {
				firstTypeError = err
			}
		},
	}
	_, _ = tcfg.Check(pkgPath, fset, astFiles, info)

	pi := &methodCallPackageInfo{
		selByPos:       make(map[methodCallPosition]*types.Selection, len(info.Selections)),
		firstTypeError: firstTypeError,
	}
	for sel, selection := range info.Selections {
		pos := fset.Position(sel.Sel.Pos())
		pi.selByPos[methodCallPosition{file: pos.Filename, line: pos.Line, col: pos.Column}] = selection
	}
	return pi, nil
}

// parseForTypeCheck records position filenames as filepath.Base(path)
func parseForTypeCheck(fset *token.FileSet, path string) (*goast.File, error) {
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

	if _, isInterface := named.Underlying().(*types.Interface); isInterface {
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
	fset      *token.FileSet
	archives  map[string]string // import path -> .a file, from -importcfg
	importMap map[string]string // source import path -> its resolved/vendored path, from -importcfg
	packages  map[string]*types.Package
}

func newExportImporter(fset *token.FileSet, archives, importMap map[string]string) *exportImporter {
	return &exportImporter{
		fset:      fset,
		archives:  archives,
		importMap: importMap,
		packages:  make(map[string]*types.Package),
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

	resolvedPath := path
	archive, ok := imp.archives[path]
	if !ok {
		if mapped, mappedOk := imp.importMap[path]; mappedOk {
			archive, ok = imp.archives[mapped]
			resolvedPath = mapped
		}
	}
	if !ok {
		return nil, ex.Newf("no archive for import path %q in -importcfg", path)
	}

	f, err := os.Open(archive)
	if err != nil {
		return nil, ex.Wrapf(err, "opening archive for %q", path)
	}
	defer f.Close()

	r, err := gcexportdata.NewReader(f) //nolint:staticcheck // no replacement exists yet ahead of Go 1.29
	if err != nil {
		return nil, ex.Wrapf(err, "reading export data section for %q from %s", path, archive)
	}

	pkg, err := gcexportdata.Read(r, imp.fset, imp.packages, resolvedPath)
	if err != nil {
		return nil, ex.Wrapf(err, "decoding export data for %q from %s", path, archive)
	}
	imp.packages[path] = pkg
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

// applyIgnoredCallSites brackets every call whose enclosing statement
// carries //otelc:ignore. It reports whether it changed root.
func (ip *instrumentPhase) applyIgnoredCallSites(ctx context.Context, root *dst.File) (bool, error) {
	marked := markedIgnoredStmts(root)
	if len(marked) == 0 {
		return false, nil
	}

	stmtLists := findStmtLists(root)

	selfPackage := ip.isSuppressHooksPackage()

	bracketed := 0
	nextFlag := 0
	for _, list := range stmtLists {
		n, err := bracketMarkedStmts(list, marked, selfPackage, &nextFlag)
		if err != nil {
			return false, err
		}
		bracketed += n
	}
	if bracketed == 0 {
		return false, nil
	}

	if selfPackage {
		return true, nil
	}
	suppressImport := map[string]string{suppressHooksPackage: suppressHooksPackage}
	if err := ip.addRuleImports(ctx, root, suppressImport, "otelc:ignore"); err != nil {
		return false, err
	}
	return true, nil
}

// markedIgnoredStmts returns statements in root carrying //otelc:ignore
// above a call.
func markedIgnoredStmts(root *dst.File) map[dst.Stmt]bool {
	marked := make(map[dst.Stmt]bool)
	for _, stmt := range ast.CallEnclosingStmts(root) {
		if stmt == nil {
			continue
		}
		if ast.HasLeadingDirective(stmt, util.DirectiveIgnore) {
			marked[stmt] = true
		}
	}
	return marked
}

// findStmtLists returns a pointer to every statement list in root: the body
// of each block, plus the body of each switch case and select case. A
// //otelc:ignore comment can sit above a call in any of these lists.
func findStmtLists(root *dst.File) []*[]dst.Stmt {
	var lists []*[]dst.Stmt
	dst.Inspect(root, func(n dst.Node) bool {
		switch node := n.(type) {
		case *dst.BlockStmt:
			lists = append(lists, &node.List)
		case *dst.CaseClause:
			lists = append(lists, &node.Body)
		case *dst.CommClause:
			lists = append(lists, &node.Body)
		}
		return true
	})
	return lists
}

// bracketMarkedStmts brackets every statement in *list that appears in
// marked and reports how many it bracketed. selfPackage decides whether the
// inserted calls need a "runtime" qualifier. nextFlag numbers each inserted
// guard variable so that two bracketed statements in the same function never
// collide; callers share one counter across every list in the file.
func bracketMarkedStmts(
	list *[]dst.Stmt,
	marked map[dst.Stmt]bool,
	selfPackage bool,
	nextFlag *int,
) (int, error) {
	stmts := *list
	bracketed := 0
	for i := 0; i < len(stmts); i++ {
		stmt := stmts[i]
		if !marked[stmt] {
			continue
		}
		if hasEscapingControlFlow(stmt) {
			*list = stmts
			return 0, ex.Newf(
				"the statement above //otelc:ignore returns, breaks, continues, or jumps out of " +
					"its enclosing block; the suppression placed after it would not always run. " +
					"Assign the call's result to a variable in its own statement, then use the " +
					"variable in the control-flow statement on its own, unannotated line")
		}
		if hasMultipleCalls(stmt) {
			*list = stmts
			return 0, ex.Newf(
				"the statement above //otelc:ignore holds more than one call. The bracket " +
					"suppresses hooks for the whole statement, so it would also suppress hooks " +
					"for the other calls. Give each other call its own statement, then use " +
					"its result here")
		}
		before, after := suppressHooksStmts(selfPackage, *nextFlag)
		*nextFlag++
		stmts = append(stmts[:i], append(before, stmts[i:]...)...)
		i += len(before)
		stmts = append(stmts[:i+1], append(after, stmts[i+1:]...)...)
		i += len(after)
		bracketed++
	}
	*list = stmts
	return bracketed, nil
}

// suppressHooksStmts returns the statements to put before and after an
// ignored call to suppress any hooks around it.
//
// A deferred statement also turns suppression off if the call panics.

//nolint:revive // nonamedreturns conflicts with confusing-results
func suppressHooksStmts(selfPackage bool, id int) ([]dst.Stmt, []dst.Stmt) {
	flag := fmt.Sprintf("otelcIgnoreDone%d", id)

	fallback := &dst.IfStmt{
		Cond: &dst.UnaryExpr{Op: token.NOT, X: ast.Ident(flag)},
		Body: ast.Block(ast.ExprStmt(suppressHooksCall(unsuppressHooksFuncName, selfPackage))),
	}
	deferFallback := ast.DeferStmt(&dst.CallExpr{
		Fun: &dst.FuncLit{
			Type: &dst.FuncType{Params: &dst.FieldList{}},
			Body: ast.BlockStmts(fallback),
		},
	})

	before := []dst.Stmt{
		ast.ExprStmt(suppressHooksCall(suppressHooksFuncName, selfPackage)),
		ast.DefineStmts([]dst.Expr{ast.Ident(flag)}, []dst.Expr{ast.BoolFalse()}),
		deferFallback,
	}
	after := []dst.Stmt{
		ast.AssignStmt(ast.Ident(flag), ast.BoolTrue()),
		ast.ExprStmt(suppressHooksCall(unsuppressHooksFuncName, selfPackage)),
	}
	return before, after
}

// hasEscapingControlFlow reports whether stmt's subtree holds a return,
// break, continue, or goto outside a nested function literal.
func hasEscapingControlFlow(stmt dst.Stmt) bool {
	found := false
	dst.Inspect(stmt, func(n dst.Node) bool {
		if found {
			return false
		}
		switch n.(type) {
		case *dst.FuncLit:
			return false
		case *dst.ReturnStmt, *dst.BranchStmt:
			found = true
			return false
		}
		return true
	})
	return found
}

// hasMultipleCalls reports whether stmt's subtree holds more than one call,
// outside a nested function literal.
func hasMultipleCalls(stmt dst.Stmt) bool {
	calls := 0
	dst.Inspect(stmt, func(n dst.Node) bool {
		switch n.(type) {
		case *dst.FuncLit:
			return false
		case *dst.CallExpr:
			calls++
		}
		return true
	})
	return calls > 1
}
