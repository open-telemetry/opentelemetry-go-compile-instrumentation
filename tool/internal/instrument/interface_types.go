// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/pkgload"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const decimalBase = 10

type hookPackageTypeInfo struct {
	pkg       *types.Package
	decls     map[string]*dst.TypeSpec
	files     []*dst.File
	loadCause string
}

type hookDeclContext struct {
	rule     *rule.InstFuncRule
	file     *dst.File
	funcDecl *dst.FuncDecl
}

type astInterfaceInfo struct {
	empty    bool
	methods  []string
	resolved bool
}

type resolvedInterface struct {
	typesIface *types.Interface
	astInfo    astInterfaceInfo
	cause      string
	loadCause  string
}

type interfaceSlotPair struct {
	target *dst.InterfaceType
	hook   *dst.InterfaceType
	ok     bool
}

type methodSetComparison struct {
	known bool
	equal bool
	cause string
}

type interfaceCheckSlot struct {
	hook        hookDeclContext
	hookIdx     int
	targetIdx   int
	before      bool
	errParamIdx int
	trampBase   string
	hookBase    string
}

func (ip *instrumentPhase) resolveHookDeclAndFile(
	t *rule.InstFuncRule,
	before bool,
) (*dst.File, *dst.FuncDecl, error) {
	hookFilePath, hookFunc, err := ip.findHookFile(t, getHookFuncName(t, before))
	if err != nil {
		return nil, nil, err
	}
	hookFile, err := ip.parseHookFileCached(hookFilePath)
	if err != nil {
		return nil, nil, err
	}
	return hookFile, hookFunc, nil
}

// checkParamInterfaceMethodSets compares resolved method sets when a target
// parameter/result and hook parameter are both anonymous interfaces and at
// least one embeds another interface. Type matching strips package qualifiers
// from embedded interface names, so an empty or differently typed hook interface
// could otherwise match a method-bearing target interface and corrupt the ABI.
func (ip *instrumentPhase) checkParamInterfaceMethodSets(
	ctx context.Context,
	slot interfaceCheckSlot,
	trampExpr dst.Expr,
	targetParams *dst.FieldList,
	hookExpr dst.Expr,
) error {
	if isAnyOrInterface(slot.hookBase) {
		return nil
	}
	targetRaw := targetExprForCheck(trampExpr, targetParams, slot.targetIdx, slot.before)
	pair := unwrapInterfaceSlotPair(targetRaw, hookExpr)
	if !pair.ok || ast.IsEmptyInterfaceType(pair.hook) {
		return nil
	}
	if !hasEmbeddedInterface(pair.target) && !hasEmbeddedInterface(pair.hook) {
		return nil
	}

	targetResolved := ip.resolveTargetInterface(ctx, slot.targetIdx, slot.before, pair.target)
	hookResolved := ip.resolveHookInterface(ctx, slot.hook, slot.hookIdx, pair.hook)
	cmp := equalInterfaceMethodSets(targetResolved, hookResolved)
	if !cmp.known {
		return ex.Newf("cannot verify interface method sets for hook %s param %d: %s",
			hookDisplayName(slot.hook), slot.errParamIdx, cmp.cause)
	}
	if !cmp.equal {
		return ex.Newf("hook func param %d type mismatch, expected %s, got %s",
			slot.errParamIdx, slot.trampBase, slot.hookBase)
	}
	return nil
}

func hookDisplayName(hook hookDeclContext) string {
	name := ""
	if hook.funcDecl != nil && hook.funcDecl.Name != nil {
		name = hook.funcDecl.Name.Name
	}
	if hook.rule != nil && hook.rule.Path != "" {
		return hook.rule.Path + "." + name
	}
	return name
}

// isEmptyHookInterface reports whether a hook parameter expression resolves to
// an empty interface (no methods, represented as eface in Go's ABI), including
// anonymous interfaces embedding empty local or imported interfaces.
func (ip *instrumentPhase) isEmptyHookInterface(
	ctx context.Context,
	hook hookDeclContext,
	paramIdx int,
	hookExpr dst.Expr,
) bool {
	if ast.IsEmptyInterfaceType(hookExpr) {
		return true
	}
	hookIface, ok := unwrapInterfaceExpr(hookExpr)
	if !ok || hasExplicitInterfaceMethod(hookIface) {
		return false
	}
	res := ip.resolveHookInterface(ctx, hook, paramIdx, hookIface)
	if res.typesIface != nil {
		return res.typesIface.Empty()
	}
	return res.cause == "" && res.astInfo.resolved && res.astInfo.empty
}

// rsetHasEmbeddedInterfaceHook reports whether any matched func rule in rset
// uses a hook parameter with an anonymous embedded interface. Running
// ensureMethodCallInfo upfront keeps type-checking accurate before any file in
// the package is rewritten.
func (ip *instrumentPhase) rsetHasEmbeddedInterfaceHook(rset *rule.InstRuleSet) bool {
	for _, funcRule := range rset.AllFuncRules() {
		if funcRule.Before != "" && ip.hookHasEmbeddedInterface(funcRule, true) {
			return true
		}
		if funcRule.After != "" && ip.hookHasEmbeddedInterface(funcRule, false) {
			return true
		}
	}
	return false
}

func (ip *instrumentPhase) hookHasEmbeddedInterface(t *rule.InstFuncRule, before bool) bool {
	_, hookFunc, err := ip.findHookFile(t, getHookFuncName(t, before))
	if err != nil {
		return false
	}
	for _, field := range hookFunc.Type.Params.List {
		expr := field.Type
		if el, ok := expr.(*dst.Ellipsis); ok {
			expr = el.Elt
		}
		if iface, ok := unwrapInterfaceExpr(expr); ok && hasEmbeddedInterface(iface) {
			return true
		}
	}
	return false
}

func targetExprForCheck(
	trampExpr dst.Expr,
	targetParams *dst.FieldList,
	targetIdx int,
	before bool,
) dst.Expr {
	if before && targetParams != nil && targetIdx < len(targetParams.List) {
		return targetParams.List[targetIdx].Type
	}
	if star, ok := trampExpr.(*dst.StarExpr); ok {
		return star.X
	}
	return trampExpr
}

func unwrapInterfaceSlotPair(targetExpr, hookExpr dst.Expr) interfaceSlotPair {
	if hookEl, ok := hookExpr.(*dst.Ellipsis); ok {
		switch target := targetExpr.(type) {
		case *dst.Ellipsis:
			targetExpr = target.Elt
		case *dst.ArrayType:
			if target.Len != nil {
				return interfaceSlotPair{}
			}
			targetExpr = target.Elt
		default:
			return interfaceSlotPair{}
		}
		hookExpr = hookEl.Elt
	}
	targetIface, okT := unwrapInterfaceExpr(targetExpr)
	hookIface, okH := unwrapInterfaceExpr(hookExpr)
	return interfaceSlotPair{target: targetIface, hook: hookIface, ok: okT && okH}
}

func unwrapInterfaceExpr(expr dst.Expr) (*dst.InterfaceType, bool) {
	for {
		paren, ok := expr.(*dst.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}
	iface, ok := expr.(*dst.InterfaceType)
	return iface, ok
}

func hasEmbeddedInterface(iface *dst.InterfaceType) bool {
	if iface == nil || iface.Methods == nil {
		return false
	}
	for _, m := range iface.Methods.List {
		if len(m.Names) == 0 {
			return true
		}
	}
	return false
}

func hasExplicitInterfaceMethod(iface *dst.InterfaceType) bool {
	if iface == nil || iface.Methods == nil {
		return false
	}
	for _, m := range iface.Methods.List {
		if len(m.Names) > 0 {
			return true
		}
	}
	return false
}

func resolveASTInterface(
	expr dst.Expr,
	decls map[string]*dst.TypeSpec,
	visited map[string]bool,
) astInterfaceInfo {
	for {
		paren, ok := expr.(*dst.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}
	if ast.IsEmptyInterfaceType(expr) {
		return astInterfaceInfo{empty: true, resolved: true}
	}
	if ident, ok := expr.(*dst.Ident); ok {
		ts := decls[ident.Name]
		if ts == nil || visited[ident.Name] {
			return astInterfaceInfo{}
		}
		visited[ident.Name] = true
		return resolveASTInterface(ts.Type, decls, visited)
	}
	iface, ok := expr.(*dst.InterfaceType)
	if !ok {
		return astInterfaceInfo{}
	}
	return resolveASTInterfaceMembers(iface, decls, visited)
}

func resolveASTInterfaceMembers(
	iface *dst.InterfaceType,
	decls map[string]*dst.TypeSpec,
	visited map[string]bool,
) astInterfaceInfo {
	var methods []string
	for _, m := range iface.Methods.List {
		if len(m.Names) == 0 {
			sub := resolveASTInterface(m.Type, decls, visited)
			if !sub.resolved {
				return astInterfaceInfo{}
			}
			methods = append(methods, sub.methods...)
			continue
		}
		for _, name := range m.Names {
			sig := baseTypeName(m.Type)
			if ft, ok := m.Type.(*dst.FuncType); ok {
				sig = strings.TrimPrefix(funcTypeName(ft), "func")
			}
			methods = append(methods, name.Name+sig)
		}
	}
	slices.Sort(methods)
	methods = slices.Compact(methods)
	return astInterfaceInfo{
		empty:    len(methods) == 0,
		methods:  methods,
		resolved: true,
	}
}

func collectTypeDeclsFromFiles(files []*dst.File) map[string]*dst.TypeSpec {
	decls := make(map[string]*dst.TypeSpec)
	for _, file := range files {
		if file == nil {
			continue
		}
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*dst.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range genDecl.Specs {
				if ts, okTS := spec.(*dst.TypeSpec); okTS && ts.Name != nil {
					decls[ts.Name.Name] = ts
				}
			}
		}
	}
	return decls
}

func (ip *instrumentPhase) resolveTargetInterface(
	ctx context.Context,
	targetIdx int,
	before bool,
	targetIface *dst.InterfaceType,
) resolvedInterface {
	var res resolvedInterface
	var decls map[string]*dst.TypeSpec
	if ip.target != nil {
		decls = collectTypeDeclsFromFiles([]*dst.File{ip.target})
		res.astInfo = resolveASTInterface(targetIface, decls, map[string]bool{})
	}
	usesExternal := exprUsesExternalTypes(targetIface, decls, map[string]bool{})
	if len(ip.compileArgs) == 0 || ip.targetFunc == nil {
		if !res.astInfo.resolved || usesExternal {
			res.cause = "target package type info unavailable"
		}
		return res
	}
	info := ip.ensureMethodCallInfo(ctx)
	if info == nil || info.pkg == nil {
		res.cause = "target package type check failed"
		return res
	}
	if !embeddedSelectorsValid(targetIface, ip.target, info.pkg) {
		res.cause = "unresolved target embedded interface import or type"
		return res
	}
	sig := ip.fileTypes().signature(ip.targetFunc)
	slotType, ok := targetSlotType(sig, ip.targetFunc, targetIdx, before)
	if !ok {
		res.cause = "target parameter or result type unavailable in type info"
		return res
	}
	res.typesIface = extractTypesInterface(slotType)
	if res.typesIface == nil {
		res.cause = "target slot did not resolve to an interface type"
	}
	return res
}

func targetSlotType(
	sig *types.Signature,
	targetFunc *dst.FuncDecl,
	targetIdx int,
	before bool,
) (types.Type, bool) {
	if sig == nil || targetFunc == nil {
		return nil, false
	}
	if !before {
		resultIdx := targetIdx - 1
		if resultIdx < 0 || resultIdx >= sig.Results().Len() {
			return nil, false
		}
		return sig.Results().At(resultIdx).Type(), true
	}
	recvOffset := 0
	if targetFunc.Recv != nil && len(targetFunc.Recv.List) > 0 {
		if sig.Recv() == nil {
			return nil, false
		}
		recvOffset = 1
	}
	if targetIdx < recvOffset {
		return sig.Recv().Type(), true
	}
	paramIdx := targetIdx - recvOffset
	if paramIdx < 0 || paramIdx >= sig.Params().Len() {
		return nil, false
	}
	return sig.Params().At(paramIdx).Type(), true
}

func (ip *instrumentPhase) resolveHookInterface(
	ctx context.Context,
	hook hookDeclContext,
	hookParamIdx int,
	hookIface *dst.InterfaceType,
) resolvedInterface {
	var res resolvedInterface
	var pkgInfo *hookPackageTypeInfo
	if hook.rule != nil && hook.funcDecl != nil {
		pkgInfo = ip.getHookPackageGoTypes(ctx, hook.rule, hook.file)
	} else {
		pkgInfo = ip.buildHookPackageAST(hook.rule, hook.file)
	}
	res.astInfo = resolveASTInterface(hookIface, pkgInfo.decls, map[string]bool{})
	usesExternal := exprUsesExternalTypes(hookIface, pkgInfo.decls, map[string]bool{})
	if hook.rule == nil || hook.funcDecl == nil {
		if !res.astInfo.resolved || usesExternal {
			res.cause = "missing hook rule or function declaration"
		}
		return res
	}
	if pkgInfo.pkg == nil || pkgInfo.pkg.Scope() == nil {
		res.loadCause = pkgInfo.loadCause
		if !res.astInfo.resolved || usesExternal {
			res.cause = pkgInfo.loadCause
		}
		return res
	}
	if !embeddedSelectorsValid(hookIface, hook.file, pkgInfo.pkg) {
		res.cause = "unresolved hook embedded interface import or type"
		return res
	}
	fnObj, ok := pkgInfo.pkg.Scope().Lookup(hook.funcDecl.Name.Name).(*types.Func)
	if !ok {
		res.cause = "hook function " + hook.funcDecl.Name.Name + " not found in loaded package"
		return res
	}
	sig, ok := fnObj.Type().(*types.Signature)
	if !ok || hookParamIdx < 0 || hookParamIdx >= sig.Params().Len() {
		res.cause = "hook parameter unavailable in loaded package signature"
		return res
	}
	res.typesIface = extractTypesInterface(sig.Params().At(hookParamIdx).Type())
	if res.typesIface == nil {
		res.cause = "hook parameter did not resolve to an interface type"
	}
	return res
}

func exprUsesExternalTypes(expr dst.Node, decls map[string]*dst.TypeSpec, visited map[string]bool) bool {
	if expr == nil {
		return false
	}
	found := false
	dst.Inspect(expr, func(node dst.Node) bool {
		if found || node == nil {
			return false
		}
		switch n := node.(type) {
		case *dst.SelectorExpr, *dst.IndexExpr, *dst.IndexListExpr:
			found = true
			return false
		case *dst.Ident:
			if ts := decls[n.Name]; ts != nil && !visited[n.Name] {
				visited[n.Name] = true
				if exprUsesExternalTypes(ts.Type, decls, visited) {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

func embeddedSelectorsValid(iface *dst.InterfaceType, file *dst.File, pkg *types.Package) bool {
	if pkg == nil {
		return false
	}
	if iface == nil || iface.Methods == nil || file == nil {
		return true
	}
	for _, m := range iface.Methods.List {
		if len(m.Names) != 0 {
			continue
		}
		if !embeddedExprValid(m.Type, file, pkg) {
			return false
		}
	}
	return true
}

func embeddedExprValid(expr dst.Expr, file *dst.File, pkg *types.Package) bool {
	expr = unwrapEmbeddedTarget(expr)
	sel, ok := expr.(*dst.SelectorExpr)
	if !ok {
		return true
	}
	qual, ok := sel.X.(*dst.Ident)
	if !ok {
		return false
	}
	imported := findImportedPackage(pkg, file, qual.Name)
	if imported == nil {
		return false
	}
	obj := imported.Scope().Lookup(sel.Sel.Name)
	return obj != nil && isValidType(obj.Type())
}

func unwrapEmbeddedTarget(expr dst.Expr) dst.Expr {
	for {
		switch node := expr.(type) {
		case *dst.ParenExpr:
			expr = node.X
		case *dst.IndexExpr:
			expr = node.X
		case *dst.IndexListExpr:
			expr = node.X
		default:
			return expr
		}
	}
}

func findImportedPackage(pkg *types.Package, file *dst.File, qual string) *types.Package {
	byPath := make(map[string]*types.Package, len(pkg.Imports()))
	for _, imp := range pkg.Imports() {
		byPath[imp.Path()] = imp
	}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		if spec.Name != nil {
			if spec.Name.Name == qual {
				return byPath[path]
			}
			continue
		}
		if imp := byPath[path]; imp != nil && imp.Name() == qual {
			return imp
		}
	}
	return nil
}

func extractTypesInterface(t types.Type) *types.Interface {
	if t == nil {
		return nil
	}
	t = types.Unalias(t)
	if slice, ok := t.(*types.Slice); ok {
		t = types.Unalias(slice.Elem())
	}
	iface, _ := t.Underlying().(*types.Interface)
	return iface
}

func equalInterfaceMethodSets(target, hook resolvedInterface) methodSetComparison {
	if target.typesIface != nil && hook.typesIface != nil {
		return methodSetComparison{
			known: true,
			equal: equalTypesInterfaces(target.typesIface, hook.typesIface),
		}
	}
	if target.typesIface == nil && hook.typesIface == nil &&
		target.astInfo.resolved && hook.astInfo.resolved &&
		target.cause == "" && hook.cause == "" {
		return methodSetComparison{
			known: true,
			equal: slices.Equal(target.astInfo.methods, hook.astInfo.methods),
		}
	}
	if target.typesIface != nil && hook.astInfo.resolved && hook.cause == "" {
		return mixedInterfaceComparison(target.typesIface, hook.astInfo, hook.loadCause)
	}
	if hook.typesIface != nil && target.astInfo.resolved && target.cause == "" {
		return mixedInterfaceComparison(hook.typesIface, target.astInfo, target.cause)
	}
	return methodSetComparison{cause: firstCause(hook.cause, target.cause, hook.loadCause)}
}

func mixedInterfaceComparison(
	typesIface *types.Interface,
	astInfo astInterfaceInfo,
	fallbackCause string,
) methodSetComparison {
	if typesIface.Empty() != astInfo.empty || typesIface.NumMethods() != len(astInfo.methods) {
		return methodSetComparison{known: true, equal: false}
	}
	if typesIface.Empty() {
		return methodSetComparison{known: true, equal: true}
	}
	return methodSetComparison{
		cause: firstCause(fallbackCause, "non-empty interface method comparison requires type info on both sides"),
	}
}

func firstCause(causes ...string) string {
	for _, c := range causes {
		if c != "" {
			return c
		}
	}
	return "interface type resolution failed"
}

func equalTypesInterfaces(a, b *types.Interface) bool {
	if a.NumMethods() != b.NumMethods() || a.IsComparable() != b.IsComparable() {
		return false
	}
	for i := range a.NumMethods() {
		ma, mb := a.Method(i), b.Method(i)
		if ma.Id() != mb.Id() || canonicalTypeString(ma.Type()) != canonicalTypeString(mb.Type()) {
			return false
		}
	}
	return true
}

func canonicalTypeString(t types.Type) string {
	t = types.Unalias(t)
	switch typ := t.(type) {
	case *types.Basic:
		if typ.Kind() == types.Byte {
			return "uint8"
		}
		if typ.Kind() == types.Rune {
			return "int32"
		}
		return typ.Name()
	case *types.Pointer:
		return "*" + canonicalTypeString(typ.Elem())
	case *types.Slice:
		return "[]" + canonicalTypeString(typ.Elem())
	case *types.Array:
		return "[" + strconv.FormatInt(typ.Len(), decimalBase) + "]" + canonicalTypeString(typ.Elem())
	case *types.Map:
		return "map[" + canonicalTypeString(typ.Key()) + "]" + canonicalTypeString(typ.Elem())
	case *types.Chan:
		return canonicalChanString(typ)
	case *types.Named:
		return canonicalNamedString(typ)
	case *types.Signature:
		return canonicalSignatureString(typ)
	case *types.Interface:
		return canonicalInterfaceString(typ)
	default:
		return types.TypeString(typ, packagePathQualifier)
	}
}

func canonicalChanString(ch *types.Chan) string {
	switch ch.Dir() {
	case types.SendOnly:
		return "chan<- " + canonicalTypeString(ch.Elem())
	case types.RecvOnly:
		return "<-chan " + canonicalTypeString(ch.Elem())
	case types.SendRecv:
		return "chan " + canonicalTypeString(ch.Elem())
	default:
		return "chan " + canonicalTypeString(ch.Elem())
	}
}

func canonicalNamedString(t *types.Named) string {
	name := t.Obj().Name()
	if pkg := t.Obj().Pkg(); pkg != nil && pkg.Path() != "" {
		name = pkg.Path() + "." + name
	}
	targs := t.TypeArgs()
	if targs == nil || targs.Len() == 0 {
		return name
	}
	parts := make([]string, targs.Len())
	for i := range targs.Len() {
		parts[i] = canonicalTypeString(targs.At(i))
	}
	return name + "[" + strings.Join(parts, ", ") + "]"
}

func canonicalSignatureString(sig *types.Signature) string {
	params := make([]string, sig.Params().Len())
	for i := range sig.Params().Len() {
		pType := sig.Params().At(i).Type()
		if sig.Variadic() && i == sig.Params().Len()-1 {
			if slice, ok := types.Unalias(pType).(*types.Slice); ok {
				params[i] = "..." + canonicalTypeString(slice.Elem())
				continue
			}
		}
		params[i] = canonicalTypeString(pType)
	}
	results := make([]string, sig.Results().Len())
	for i := range sig.Results().Len() {
		results[i] = canonicalTypeString(sig.Results().At(i).Type())
	}
	return "func(" + strings.Join(params, ", ") + ") (" + strings.Join(results, ", ") + ")"
}

func canonicalInterfaceString(iface *types.Interface) string {
	methods := make([]string, 0, iface.NumMethods()+1)
	for m := range iface.Methods() {
		methods = append(methods, m.Id()+" "+canonicalTypeString(m.Type()))
	}
	if iface.IsComparable() {
		methods = append(methods, "comparable")
	}
	return "interface{" + strings.Join(methods, "; ") + "}"
}

func packagePathQualifier(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	return pkg.Path()
}

func (ip *instrumentPhase) buildHookPackageAST(t *rule.InstFuncRule, hookFile *dst.File) *hookPackageTypeInfo {
	files := ip.collectHookFiles(t, hookFile)
	return &hookPackageTypeInfo{
		decls: collectTypeDeclsFromFiles(files),
		files: files,
	}
}

func (ip *instrumentPhase) collectHookFiles(t *rule.InstFuncRule, hookFile *dst.File) []*dst.File {
	if t == nil || t.ResolvedPath == "" {
		if hookFile == nil {
			return nil
		}
		return []*dst.File{hookFile}
	}
	paths, err := hookPackageFiles(t)
	if err != nil {
		if hookFile == nil {
			return nil
		}
		return []*dst.File{hookFile}
	}
	files := make([]*dst.File, 0, len(paths))
	for _, path := range paths {
		parsed, parseErr := ip.parseHookFileCached(path)
		if parseErr != nil {
			continue
		}
		files = append(files, parsed)
	}
	return files
}

func (ip *instrumentPhase) getHookPackageGoTypes(
	ctx context.Context,
	t *rule.InstFuncRule,
	hookFile *dst.File,
) *hookPackageTypeInfo {
	key := hookCacheKey(t)
	if cached, ok := ip.hookPkgTypes[key]; ok {
		return cached
	}
	if ip.hookPkgTypes == nil {
		ip.hookPkgTypes = make(map[string]*hookPackageTypeInfo)
	}

	info := ip.buildHookPackageAST(t, hookFile)
	info.pkg, info.loadCause = ip.loadHookGoTypes(ctx, t, info.files)
	ip.hookPkgTypes[key] = info
	return info
}

func hookCacheKey(t *rule.InstFuncRule) string {
	if t == nil {
		return ""
	}
	return t.ResolvedPath + "|" + t.Path
}

func (ip *instrumentPhase) loadHookGoTypes(
	ctx context.Context,
	t *rule.InstFuncRule,
	files []*dst.File,
) (*types.Package, string) {
	pkg, exportErr := ip.tryCheckHookFromExportConfig(ctx, t, files)
	if pkg != nil {
		return pkg, ""
	}
	mode := packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps
	var causes []string
	if exportErr != nil {
		causes = append(causes, exportErr.Error())
	}
	if t.ResolvedPath != "" {
		dirPkg, dirErr := loadSinglePackageTypes(ctx, mode, t.ResolvedPath, hookDirBuildFlags(), ".")
		if dirPkg != nil {
			return dirPkg, ""
		}
		causes = append(causes, dirErr.Error())
	}
	if t.Path != "" {
		pkgs, loadErr := pkgload.LoadPackages(ctx, mode, util.GetBuildFlags(), false, t.Path)
		if loadErr != nil {
			causes = append(causes, loadErr.Error())
		} else {
			pathPkg, pkgErr := extractLoadedPackage(pkgs, t.Path)
			if pathPkg != nil {
				return pathPkg, ""
			}
			causes = append(causes, pkgErr.Error())
		}
	}
	return nil, strings.Join(causes, "; ")
}

func (ip *instrumentPhase) tryCheckHookFromExportConfig(
	ctx context.Context,
	t *rule.InstFuncRule,
	files []*dst.File,
) (*types.Package, error) {
	if t.ResolvedPath == "" || len(ip.importConfig.PackageFile) == 0 {
		return nil, ex.New("target import config unavailable")
	}
	exportFiles, err := ip.exportDataFiles(ctx)
	if err != nil {
		return nil, err
	}
	if missing := missingHookImport(files, exportFiles); missing != "" {
		return nil, ex.Newf("hook import %s missing from target import config", missing)
	}
	filePaths, err := hookPackageFiles(t)
	if err != nil || len(filePaths) == 0 {
		return nil, ex.New("no hook package files found")
	}
	pkgPath := t.Path
	if pkgPath == "" {
		pkgPath = "hookpkg"
	}
	pkgInfo, checkErr := checkPackageForMethodCalls(pkgPath, filePaths, imports.ImportConfig{
		PackageFile: exportFiles,
		ImportMap:   ip.importConfig.ImportMap,
	})
	if checkErr != nil {
		return nil, checkErr
	}
	if pkgInfo.firstTypeError != nil {
		return nil, pkgInfo.firstTypeError
	}
	return pkgInfo.pkg, nil
}

func missingHookImport(files []*dst.File, exportFiles map[string]string) string {
	for _, file := range files {
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, `"`)
			if importPath == unsafePackageName || importPath == "C" {
				continue
			}
			if _, ok := exportFiles[importPath]; !ok {
				return importPath
			}
		}
	}
	return ""
}

func hookDirBuildFlags() []string {
	tags := util.ActiveBuildTags()
	if len(tags) == 0 {
		return nil
	}
	return []string{"-tags=" + strings.Join(tags, ",")}
}

func loadSinglePackageTypes(
	ctx context.Context,
	mode packages.LoadMode,
	dir string,
	buildFlags []string,
	pattern string,
) (*types.Package, error) {
	cfg := &packages.Config{
		Mode:       mode,
		Context:    ctx,
		Dir:        dir,
		BuildFlags: buildFlags,
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, err
	}
	return extractLoadedPackage(pkgs, pattern)
}

func extractLoadedPackage(pkgs []*packages.Package, pattern string) (*types.Package, error) {
	if len(pkgs) == 0 {
		return nil, ex.Newf("no packages loaded for %s", pattern)
	}
	if len(pkgs[0].Errors) > 0 {
		return nil, ex.New(pkgs[0].Errors[0].Error())
	}
	return pkgs[0].Types, nil
}
