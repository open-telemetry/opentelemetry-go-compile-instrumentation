// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	_ "embed"
	"fmt"
	"go/token"
	"strconv"
	"strings"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// -----------------------------------------------------------------------------
// Trampoline Jump
//
// We distinguish between three types of functions: RawFunc, TrampolineFunc, and
// HookFunc. RawFunc is the original function that needs to be instrumented.
// TrampolineFunc is the function that is generated to call the Before and
// After hooks, it serves as a trampoline to the original function. HookFunc is
// the function that is called at entrypoint and exitpoint of the RawFunc. The
// so-called "Trampoline Jump" snippet is inserted at start of raw func, it is
// guaranteed to be generated within one line to avoid confusing debugging, as
// its name suggests, it jumps to the trampoline function from raw function.

const (
	trampolineBeforeName            = "OtelBeforeTrampoline"
	trampolineAfterName             = "OtelAfterTrampoline"
	trampolineHookContextName       = "hookContext"
	trampolineHookContextType       = "HookContext"
	trampolineInterfaceType         = "interface{}"
	trampolineEmptyStructType       = "struct{}"
	trampolineSkipName              = "skip"
	trampolineSetParamName          = "SetParam"
	trampolineGetParamName          = "GetParam"
	trampolineSetReturnValName      = "SetReturnVal"
	trampolineGetReturnValName      = "GetReturnVal"
	trampolineSetSkipCallName       = "SetSkipCall"
	trampolineValIdentifier         = "val"
	trampolineCtxIdentifier         = "c"
	trampolineParamsIdentifier      = "params"
	trampolineFuncNameIdentifier    = "funcName"
	trampolinePackageNameIdentifier = "packageName"
	trampolineReturnValsIdentifier  = "returnVals"
	trampolineHookContextImplType   = "HookContextImpl"
	trampolineBeforeNamePlaceholder = `"OtelBeforeNamePlaceholder"`
	trampolineAfterNamePlaceholder  = `"OtelAfterNamePlaceholder"`
	trampolineBefore                = true
	trampolineAfter                 = false
	unsafePackageName               = "unsafe"
)

// @@ Modification on this trampoline template should be cautious, as it imposes
// many implicit constraints on generated code, known constraints are as follows:
// - It's performance critical, so it should be as simple as possible
// - It should not import any package because there is no guarantee that package
//   is existed in import config during the compilation, one practical approach
//   is to use function variables and setup these variables in preprocess stage
// - It should not panic as this affects user application
// - Function and variable names are coupled with the framework, any modification
//   on them should be synced with the framework

//go:embed impl.tmpl
var templateImpl string

func (ip *instrumentPhase) addDecl(decl dst.Decl) {
	util.Assert(ip.target != nil, "sanity check")
	ip.target.Decls = append(ip.target.Decls, decl)
}

// ensureUnsafeImport ensures that the unsafe package is imported in the target file.
// This is required when using //go:linkname directives.
func (ip *instrumentPhase) ensureUnsafeImport() {
	for _, decl := range ip.target.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		for _, spec := range genDecl.Specs {
			if importSpec, ok2 := spec.(*dst.ImportSpec); ok2 &&
				importSpec.Path.Value == strconv.Quote(unsafePackageName) {
				return
			}
		}
	}
	unsafeImport := ast.ImportDecl(ast.IdentIgnore, unsafePackageName)
	ip.target.Decls = append([]dst.Decl{unsafeImport}, ip.target.Decls...)
}

func (ip *instrumentPhase) materializeTemplate() error {
	// Read trampoline template and materialize before and after function
	// declarations based on that
	p := ast.NewAstParser()
	astRoot, err := p.ParseSource(templateImpl)
	if err != nil {
		return err
	}

	ip.varDecls = make([]dst.Decl, 0)
	ip.hookCtxMethods = make([]*dst.FuncDecl, 0)
	for _, node := range astRoot.Decls {
		// Materialize function declarations
		if decl, ok := node.(*dst.FuncDecl); ok {
			switch decl.Name.Name {
			case trampolineBeforeName:
				ip.beforeTrampFunc = decl
				ip.addDecl(decl)
			case trampolineAfterName:
				ip.afterTrampFunc = decl
				ip.addDecl(decl)
			default:
				if ast.HasReceiver(decl) {
					// We know exactly this is HookContextImpl method
					t := util.AssertType[*dst.StarExpr](decl.Recv.List[0].Type)
					t2 := util.AssertType[*dst.Ident](t.X)
					util.Assert(t2.Name == trampolineHookContextImplType, "sanity check")
					ip.hookCtxMethods = append(ip.hookCtxMethods, decl)
					ip.addDecl(decl)
				}
			}
		}
		// Materialize variable declarations
		if decl, ok := node.(*dst.GenDecl); ok {
			// No further processing for variable declarations, just append them
			//nolint:exhaustive // We have too many cases to cover, but we can't exhaustively cover them all
			switch decl.Tok {
			case token.VAR:
				ip.varDecls = append(ip.varDecls, decl)
			case token.TYPE:
				ip.hookCtxDecl = decl
				ip.addDecl(decl)
			default:
				util.ShouldNotReachHere()
			}
		}
	}
	util.Assert(ip.hookCtxDecl != nil &&
		ip.beforeTrampFunc != nil &&
		ip.afterTrampFunc != nil, "sanity check")
	util.Assert(len(ip.varDecls) > 0, "sanity check")
	return nil
}

func getNames(list *dst.FieldList) []string {
	var names []string
	for _, field := range list.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func getHookFuncName(t *rule.InstFuncRule, before bool) string {
	if before {
		return t.Before
	}
	return t.After
}

func isHookDefined(root *dst.File, rule *rule.InstFuncRule) bool {
	if rule.Before != "" {
		decl := ast.FindFuncDeclWithoutRecv(root, rule.Before)
		if decl == nil {
			return false
		}
	}
	if rule.After != "" {
		decl := ast.FindFuncDeclWithoutRecv(root, rule.After)
		if decl == nil {
			return false
		}
	}
	return true
}

func findHookFile(rule *rule.InstFuncRule) (string, error) {
	files, err0 := util.ListFiles(rule.ResolvedPath)
	if err0 != nil {
		return "", err0
	}
	for _, file := range files {
		if !util.IsGoFile(file) {
			continue
		}
		root, err := ast.ParseFileFast(file)
		if err != nil {
			return "", err
		}
		if isHookDefined(root, rule) {
			return file, nil
		}
	}
	return "", ex.Newf("no hook {%s,%s} found for %s from %v",
		rule.Before, rule.After, rule.Func, files)
}

func getHookFunc(t *rule.InstFuncRule, before bool) (*dst.FuncDecl, error) {
	file, err := findHookFile(t)
	if err != nil {
		return nil, err
	}
	root, err := ast.ParseFile(file) // Complete parse
	if err != nil {
		return nil, err
	}
	var target *dst.FuncDecl
	if before {
		target = ast.FindFuncDeclWithoutRecv(root, t.Before)
	} else {
		target = ast.FindFuncDeclWithoutRecv(root, t.After)
	}
	if target == nil {
		return nil, ex.Newf("hook %s or %s not found from %s",
			t.Before, t.After, file)
	}
	return target, nil
}

func arrayTypeName(t *dst.ArrayType) string {
	lenStr := ""
	if t.Len != nil {
		switch l := t.Len.(type) {
		case *dst.BasicLit:
			lenStr = l.Value
		case *dst.Ident:
			lenStr = l.Name
		default:
			lenStr = ""
		}
	}
	return "[" + lenStr + "]" + baseTypeName(t.Elt)
}

func chanTypeName(t *dst.ChanType) string {
	switch t.Dir {
	case dst.SEND:
		return "chan<- " + baseTypeName(t.Value)
	case dst.RECV:
		return "<-chan " + baseTypeName(t.Value)
	default:
		return "chan " + baseTypeName(t.Value)
	}
}

func fieldListTypes(fl *dst.FieldList) []string {
	if fl == nil {
		return nil
	}
	var res []string
	for _, f := range fl.List {
		fType := baseTypeName(f.Type)
		if len(f.Names) > 0 {
			for range f.Names {
				res = append(res, fType)
			}
		} else {
			res = append(res, fType)
		}
	}
	return res
}

func funcTypeName(t *dst.FuncType) string {
	params := fieldListTypes(t.Params)
	results := fieldListTypes(t.Results)
	resStr := ""
	if len(results) == 1 {
		resStr = " " + results[0]
	} else if len(results) > 1 {
		resStr = " (" + strings.Join(results, ", ") + ")"
	}
	return "func(" + strings.Join(params, ", ") + ")" + resStr
}

func structTypeName(t *dst.StructType) string {
	if t.Fields == nil || len(t.Fields.List) == 0 {
		return trampolineEmptyStructType
	}
	parts := make([]string, 0, len(t.Fields.List))
	for _, f := range t.Fields.List {
		fType := baseTypeName(f.Type)
		if len(f.Names) == 0 {
			// Embedded field.
			parts = append(parts, fType)
			continue
		}
		for _, n := range f.Names {
			parts = append(parts, n.Name+" "+fType)
		}
	}
	return "struct{" + strings.Join(parts, "; ") + "}"
}

func interfaceTypeName(t *dst.InterfaceType) string {
	if t.Methods == nil || len(t.Methods.List) == 0 {
		return trampolineInterfaceType
	}
	parts := make([]string, 0, len(t.Methods.List))
	for _, m := range t.Methods.List {
		if len(m.Names) == 0 {
			// Embedded interface.
			parts = append(parts, baseTypeName(m.Type))
			continue
		}
		for _, n := range m.Names {
			sig := baseTypeName(m.Type)
			if ft, ok := m.Type.(*dst.FuncType); ok {
				sig = strings.TrimPrefix(funcTypeName(ft), "func")
			}
			parts = append(parts, n.Name+sig)
		}
	}
	return "interface{" + strings.Join(parts, "; ") + "}"
}

// baseTypeName normalizes a type expression for signature matching by stripping
// package qualifiers and the outermost pointer while preserving composite type
// constructors, so trampoline and hook parameter types can be compared directly.
// Examples: *pkg.Type → Type, []int → []int, ...string → ...string
func baseTypeName(expr dst.Expr) string {
	switch t := expr.(type) {
	case *dst.Ident:
		if t.Name == "any" {
			return trampolineInterfaceType
		}
		return t.Name
	case *dst.StarExpr:
		return baseTypeName(t.X)
	case *dst.SelectorExpr:
		return t.Sel.Name
	case *dst.ArrayType:
		return arrayTypeName(t)
	case *dst.Ellipsis:
		return "..." + baseTypeName(t.Elt)
	case *dst.MapType:
		return "map[" + baseTypeName(t.Key) + "]" + baseTypeName(t.Value)
	case *dst.ChanType:
		return chanTypeName(t)
	case *dst.InterfaceType:
		return interfaceTypeName(t)
	case *dst.StructType:
		return structTypeName(t)
	case *dst.FuncType:
		return funcTypeName(t)
	case *dst.IndexExpr:
		return indexListType(t.X, []dst.Expr{t.Index})
	case *dst.IndexListExpr:
		return indexListType(t.X, t.Indices)
	default:
		return ""
	}
}

// indexListType builds a normalized name for a generic type instantiation
// (X[Index] or X[Index1, Index2, ...]) from its base type and index expressions.
func indexListType(x dst.Expr, indices []dst.Expr) string {
	parts := make([]string, 0, len(indices))
	for _, idx := range indices {
		parts = append(parts, baseTypeName(idx))
	}
	return baseTypeName(x) + "[" + strings.Join(parts, ", ") + "]"
}

// isAnyOrInterface reports whether the type string represents an empty interface (any / interface{}).
func isAnyOrInterface(s string) bool {
	return s == "any" || s == trampolineInterfaceType
}

// sliceOrEllipsisElt returns the element type and true if s is a slice or ellipsis type.
func sliceOrEllipsisElt(s string) (string, bool) {
	if strings.HasPrefix(s, "[]") {
		return s[2:], true
	}
	if strings.HasPrefix(s, "...") {
		return s[3:], true
	}
	return "", false
}

// matchBaseType reports whether hookBase matches the expected trampBase type.
func matchBaseType(trampBase, hookBase string) bool {
	// The any-typed parameter can accept any trampoline parameter.
	if isAnyOrInterface(hookBase) {
		return true
	}
	if trampBase != "" && trampBase == hookBase {
		return true
	}
	if tElt, tOk := sliceOrEllipsisElt(trampBase); tOk {
		if hElt, hOk := sliceOrEllipsisElt(hookBase); hOk {
			// isAnyOrInterface(hElt) alone already covers the case where both
			// tElt and hElt are any/interface{}.
			return tElt == hElt || isAnyOrInterface(hElt)
		}
	}
	return false
}

// checkHookDecl checks if the hook function declaration is correct, i.e. if they
// have correct signature (parameter count and types)
func (ip *instrumentPhase) checkHookDecl(hookFunc *dst.FuncDecl, before bool) error {
	// TargetFunc:  func A(a int, b string) (ret string)
	// BeforeTramp: func B(a *int, b *string) (ctx *HookContext, skip bool)
	// BeforeHook:  func C(ctx *HookContext, a int, b string)
	if before {
		beforeTrampParams := ast.SplitMultiNameFields(ip.beforeTrampFunc.Type.Params)
		beforeHookParams := ast.SplitMultiNameFields(hookFunc.Type.Params)

		if len(beforeHookParams.List) != len(beforeTrampParams.List)+1 {
			return ex.Newf("hook func signature mismatch, expected %d params, got %d",
				len(beforeTrampParams.List)+1, len(beforeHookParams.List))
		}

		// First param must be HookContext
		if baseTypeName(beforeHookParams.List[0].Type) != trampolineHookContextType {
			return ex.Newf("hook func first param must be %s, got %s",
				trampolineHookContextType, baseTypeName(beforeHookParams.List[0].Type))
		}

		// Remaining params must match dereferenced trampoline params
		// (interface{} or any in hook accepts any type, used for generics)
		for i, trampField := range beforeTrampParams.List {
			trampBase := baseTypeName(trampField.Type)
			hookBase := baseTypeName(beforeHookParams.List[i+1].Type)
			if !matchBaseType(trampBase, hookBase) {
				return ex.Newf("hook func param %d type mismatch, expected %s, got %s",
					i+1, trampBase, hookBase)
			}
		}
		return nil
	}

	// TargetFunc:  func A(a int, b string) (ret string)
	// AfterTramp:  func B(ctx *HookContext, ret *string)
	// AfterHook:   func C(ctx *HookContext, ret string)
	afterTrampParams := ast.SplitMultiNameFields(ip.afterTrampFunc.Type.Params)
	afterHookParams := ast.SplitMultiNameFields(hookFunc.Type.Params)

	if len(afterHookParams.List) != len(afterTrampParams.List) {
		return ex.Newf("hook func signature mismatch, expected %d params, got %d",
			len(afterTrampParams.List), len(afterHookParams.List))
	}

	// All params must match (first is HookContext, rest are dereferenced)
	// (interface{} or any in hook accepts any type, used for generics)
	for i, trampField := range afterTrampParams.List {
		trampBase := baseTypeName(trampField.Type)
		hookBase := baseTypeName(afterHookParams.List[i].Type)
		if !matchBaseType(trampBase, hookBase) {
			return ex.Newf("hook func param %d type mismatch, expected %s, got %s",
				i, trampBase, hookBase)
		}
	}
	return nil
}

func (ip *instrumentPhase) callBeforeHook(t *rule.InstFuncRule) {
	// Query whether the parameter is a variadic parameter in the target function
	targetParams := findTargetParamType(ip.targetFunc)
	isEllipsis := func(i int) bool { return ast.IsEllipsis(targetParams.List[i].Type) }

	args := []dst.Expr{ast.Ident(trampolineHookContextName)}
	for i, field := range ip.beforeTrampFunc.Type.Params.List {
		for _, name := range field.Names {
			// If the parameter is a variadic parameter, pass "*param..." to the
			// hook function, otherwise pass "*param"
			if isEllipsis(i) {
				args = append(args, ast.DereferenceOf(ast.Ident(name.Name+"...")))
			} else {
				args = append(args, ast.DereferenceOf(ast.Ident(name.Name)))
			}
		}
	}
	fnName := getHookFuncName(t, trampolineBefore)
	call := ast.ExprStmt(ast.CallTo(fnName, nil, args))
	iff := ast.IfNotNilStmt(
		ast.Ident(fnName),
		ast.Block(call),
		nil,
	)
	insertAt(ip.beforeTrampFunc, iff, len(ip.beforeTrampFunc.Body.List)-1)
}

func (ip *instrumentPhase) callAfterHook(t *rule.InstFuncRule) {
	var args []dst.Expr
	for i, field := range ip.afterTrampFunc.Type.Params.List {
		// If it's the first HookContext parameter, pass it directly
		if i == 0 {
			args = append(args, ast.Ident(trampolineHookContextName))
			continue
		}
		for _, name := range field.Names {
			// Pass "*param" to the hook function directly, we don't need to care
			// about the variadic parameter here since variadic parameters can
			// not appear in the result list.
			args = append(args, ast.DereferenceOf(ast.Ident(name.Name)))
		}
	}
	fnName := getHookFuncName(t, trampolineAfter)
	call := ast.ExprStmt(ast.CallTo(fnName, nil, args))
	iff := ast.IfNotNilStmt(
		ast.Ident(fnName),
		ast.Block(call),
		nil,
	)
	insertAtEnd(ip.afterTrampFunc, iff)
}

func (ip *instrumentPhase) addHookDecl(t *rule.InstFuncRule, paramTypes *dst.FieldList, before bool) error {
	fnName := getHookFuncName(t, before)
	funcDecl := &dst.FuncDecl{
		Name: ast.Ident(fnName),
		Type: &dst.FuncType{
			Func:   false,
			Params: paramTypes,
		},
		Decs: dst.FuncDeclDecorations{
			NodeDecs: ast.LineComments(
				fmt.Sprintf("//go:linkname %s %s.%s", fnName, t.Path, fnName)),
		},
	}

	exist := ast.FindFuncDeclWithoutRecv(ip.target, fnName)
	if exist == nil {
		ip.addDecl(funcDecl)
	}
	return nil
}

func insertAt(funcDecl *dst.FuncDecl, stmt dst.Stmt, index int) {
	stmts := funcDecl.Body.List
	newStmts := make([]dst.Stmt, 0, len(stmts)+1)
	newStmts = append(newStmts, stmts[:index]...)
	newStmts = append(newStmts, stmt)
	newStmts = append(newStmts, stmts[index:]...)
	funcDecl.Body.List = newStmts
}

func insertAtEnd(funcDecl *dst.FuncDecl, stmt dst.Stmt) {
	insertAt(funcDecl, stmt, len(funcDecl.Body.List))
}

func (ip *instrumentPhase) renameTrampFunc(t *rule.InstFuncRule) {
	// Randomize trampoline function names
	ip.beforeTrampFunc.Name.Name = makeName(t, ip.targetFunc, trampolineBefore)
	dst.Inspect(ip.beforeTrampFunc, func(node dst.Node) bool {
		if basicLit, ok := node.(*dst.BasicLit); ok {
			// Replace OtelBeforeTrampolinePlaceHolder to real hook func name
			if basicLit.Value == trampolineBeforeNamePlaceholder {
				basicLit.Value = strconv.Quote(t.Before)
			}
		}
		return true
	})
	ip.afterTrampFunc.Name.Name = makeName(t, ip.targetFunc, trampolineAfter)
	dst.Inspect(ip.afterTrampFunc, func(node dst.Node) bool {
		if basicLit, ok := node.(*dst.BasicLit); ok {
			if basicLit.Value == trampolineAfterNamePlaceholder {
				basicLit.Value = strconv.Quote(t.After)
			}
		}
		return true
	})
}

func addHookContext(list *dst.FieldList) {
	hookCtx := ast.Field(
		trampolineHookContextName,
		ast.Ident(trampolineHookContextType),
	)
	list.List = append([]*dst.Field{hookCtx}, list.List...)
}

// findTargetParamType finds the parameter list of the target function
//
// func (recv *Type1) Target(arg1 Type2, arg2 Type3, ...) (ret1 Type4, ret2 Type5, ...)
// ->
// [recv *Type1, arg1 Type2, arg2 Type3, ...]
func findTargetParamType(targetFunc *dst.FuncDecl) *dst.FieldList {
	paramTypes := &dst.FieldList{}
	idx := 0
	if ast.HasReceiver(targetFunc) {
		splitRecv := ast.SplitMultiNameFields(targetFunc.Recv)
		recvField := util.AssertType[*dst.Field](dst.Clone(splitRecv.List[0]))
		for _, names := range recvField.Names {
			names.Name = fmt.Sprintf("%s%d", "recv", idx)
			idx++
		}
		paramTypes.List = append(paramTypes.List, recvField)
	}
	idx = 0
	splitParams := ast.SplitMultiNameFields(targetFunc.Type.Params)
	for _, field := range splitParams.List {
		paramField := util.AssertType[*dst.Field](dst.Clone(field))
		for _, names := range paramField.Names {
			names.Name = fmt.Sprintf("%s%d", "param", idx)
			idx++
		}
		paramTypes.List = append(paramTypes.List, paramField)
	}
	return paramTypes
}

// findTargetResultType finds the result list of the target function
//
// func (recv *Type1) Target(arg1 Type2, arg2 Type3, ...) (ret1 Type4, ret2 Type5, ...)
// ->
// [ret1 Type4, ret2 Type5, ...]
func findTargetResultType(targetFunc *dst.FuncDecl) *dst.FieldList {
	paramTypes := &dst.FieldList{}
	if targetFunc.Type.Results != nil {
		splitResults := ast.SplitMultiNameFields(targetFunc.Type.Results)
		idx := 0
		for _, field := range splitResults.List {
			retField := util.AssertType[*dst.Field](dst.Clone(field))
			for _, names := range retField.Names {
				names.Name = fmt.Sprintf("%s%d", "arg", idx)
				idx++
			}
			paramTypes.List = append(paramTypes.List, retField)
		}
	}
	return paramTypes
}

// findTargetGenericType finds the type parameter list of the target function
//
// func (c *Type1[K]) Target[V any]() V
// ->
// [K, V]
func findTargetGenericType(file *dst.File, targetFunc *dst.FuncDecl) *dst.FieldList {
	var trampolineTypeParams *dst.FieldList
	if ast.HasReceiver(targetFunc) {
		receiverTypeParams := extractReceiverTypeParams(file, targetFunc.Recv.List[0].Type)
		if receiverTypeParams != nil {
			trampolineTypeParams = receiverTypeParams
		}
	}
	if targetFunc.Type.TypeParams != nil {
		if trampolineTypeParams == nil {
			trampolineTypeParams = targetFunc.Type.TypeParams
		} else {
			combined := &dst.FieldList{List: make([]*dst.Field, 0)}
			combined.List = append(combined.List, trampolineTypeParams.List...)
			combined.List = append(combined.List, targetFunc.Type.TypeParams.List...)
			trampolineTypeParams = combined
		}
	}

	if trampolineTypeParams != nil {
		clone := dst.Clone(trampolineTypeParams)
		trampolineTypeParams = util.AssertType[*dst.FieldList](clone)
	}
	return trampolineTypeParams
}

func (ip *instrumentPhase) buildTrampSignature(before bool) {
	var fields *dst.FieldList
	if before {
		beforeTramp := ip.beforeTrampFunc
		beforeTramp.Type.Params = findTargetParamType(ip.targetFunc)
		beforeTramp.Type.TypeParams = findTargetGenericType(ip.target, ip.targetFunc)
		fields = beforeTramp.Type.Params
	} else {
		afterTramp := ip.afterTrampFunc
		afterTramp.Type.Params = findTargetResultType(ip.targetFunc)
		afterTramp.Type.TypeParams = findTargetGenericType(ip.target, ip.targetFunc)
		fields = afterTramp.Type.Params
	}
	// All types should be replaced with dereferenced types, so that the trampoline
	// function can modify the target function's parameters.
	for i := range len(fields.List) {
		paramField := fields.List[i]
		paramFieldType := desugarType(paramField)
		paramField.Type = ast.DereferenceOf(paramFieldType)
	}

	// If it's after trampoline, add hook context as the first parameter
	if !before {
		addHookContext(fields)
	}
}

func (ip *instrumentPhase) buildHookSignature(t *rule.InstFuncRule, before bool) (*dst.FieldList, error) {
	// TargetFunc: func A(a int, b string) (ret string)
	// BeforeHook: func B(ctx *HookContext, a int, b string)
	// AfterHook:  func C(ctx *HookContext, ret string)
	var paramTypes, genericTypes *dst.FieldList
	if before {
		paramTypes = findTargetParamType(ip.targetFunc)
	} else {
		paramTypes = findTargetResultType(ip.targetFunc)
	}
	addHookContext(paramTypes)

	// Replace type parameters with interface{}
	genericTypes = findTargetGenericType(ip.target, ip.targetFunc)
	for _, field := range paramTypes.List {
		field.Type = replaceTypeParamsWithAny(field.Type, genericTypes)
	}
	// Get the hook function declaration
	hookFunc, err := getHookFunc(t, before)
	if err != nil {
		return nil, err
	}
	// Check if the hook function declaration is correct
	err = ip.checkHookDecl(hookFunc, before)
	if err != nil {
		return nil, err
	}
	// If a hook function's signature includes a parameter of type interface{},
	// we must treat it as the definitive type, overriding the preliminary type
	// inferred from the trampoline function. This is because the trampoline
	// inherits its type from the target function, which may contain unexported
	// types. The hook function uses interface{} to accommodate these otherwise
	// inaccessible types.
	hookParamTypes := ast.SplitMultiNameFields(hookFunc.Type.Params)
	util.Assert(len(hookParamTypes.List) == len(paramTypes.List), "sanity check")
	for i, field := range hookParamTypes.List {
		if ast.IsInterfaceType(field.Type) {
			paramTypes.List[i].Type = ast.InterfaceType()
		}
	}
	return paramTypes, nil
}

func assignString(assignStmt *dst.AssignStmt, val string) bool {
	rhs := assignStmt.Rhs
	if len(rhs) == 1 {
		rhsExpr := rhs[0]
		if basicLit, ok := rhsExpr.(*dst.BasicLit); ok {
			if basicLit.Kind == token.STRING {
				basicLit.Value = strconv.Quote(val)
				return true
			}
		}
	}
	return false
}

func assignSliceLiteral(assignStmt *dst.AssignStmt, vals []dst.Expr) bool {
	rhs := assignStmt.Rhs
	if len(rhs) == 1 {
		rhsExpr := rhs[0]
		if compositeLit, ok := rhsExpr.(*dst.CompositeLit); ok {
			elems := compositeLit.Elts
			elems = append(elems, vals...)
			compositeLit.Elts = elems
			return true
		}
	}
	return false
}

// populateHookContext populates the hook context before hook invocation
func (ip *instrumentPhase) populateHookContext(before bool) bool {
	funcDecl := ip.beforeTrampFunc
	if !before {
		funcDecl = ip.afterTrampFunc
	}
	for _, stmt := range funcDecl.Body.List {
		if assignStmt, ok := stmt.(*dst.AssignStmt); ok {
			lhs := assignStmt.Lhs
			if sel, ok1 := lhs[0].(*dst.SelectorExpr); ok1 {
				switch sel.Sel.Name {
				case trampolineFuncNameIdentifier:
					util.Assert(before, "sanity check")
					// hookContext.FuncName = "..."
					assigned := assignString(assignStmt, ip.targetFunc.Name.Name)
					util.Assert(assigned, "sanity check")
				case trampolinePackageNameIdentifier:
					util.Assert(before, "sanity check")
					// hookContext.PackageName = "..."
					assigned := assignString(assignStmt, ip.target.Name.Name)
					util.Assert(assigned, "sanity check")
				default:
					// hookContext.Params = []interface{}{...} or
					// hookContext.(*HookContextImpl).Params[0] = &int
					names := getNames(funcDecl.Type.Params)
					vals := make([]dst.Expr, 0, len(names))
					for i, name := range names {
						if i == 0 && !before {
							// SKip first hookContext parameter for after
							continue
						}
						vals = append(vals, ast.Ident(name))
					}
					assigned := assignSliceLiteral(assignStmt, vals)
					util.Assert(assigned, "sanity check")
				}
			}
		}
	}
	return true
}

// -----------------------------------------------------------------------------
// Dynamic HookContext API Generation
//
// This is somewhat challenging, as we need to generate type-aware HookContext
// APIs, which means we need to generate a bunch of switch statements to handle
// different types of parameters. Different RawFuncs in the same package may have
// different types of parameters, all of them should have their own HookContext
// implementation, thus we need to generate a bunch of HookContextImpl{suffix}
// types and methods to handle them. The suffix is generated based on the rule
// suffix, so that we can distinguish them from each other.

// implementHookContext effectively "implements" the HookContext interface by
// renaming occurrences of HookContextImpl to HookContextImpl{suffix} in the
// trampoline template
func (ip *instrumentPhase) implementHookContext(t *rule.InstFuncRule) {
	suffix := t.Identity()
	structType := util.AssertType[*dst.TypeSpec](ip.hookCtxDecl.Specs[0])
	util.Assert(structType.Name.Name == trampolineHookContextImplType,
		"sanity check")
	structType.Name.Name += suffix             // type declaration
	for _, method := range ip.hookCtxMethods { // method declaration
		t1 := util.AssertType[*dst.StarExpr](method.Recv.List[0].Type)
		t2 := util.AssertType[*dst.Ident](t1.X)
		t2.Name += suffix
	}
	for _, node := range []dst.Node{ip.beforeTrampFunc, ip.afterTrampFunc} {
		dst.Inspect(node, func(node dst.Node) bool {
			if ident, ok1 := node.(*dst.Ident); ok1 {
				if ident.Name == trampolineHookContextImplType {
					ident.Name += suffix
					return false
				}
			}
			return true
		})
	}
}

func setValue(field string, idx int, t dst.Expr) *dst.CaseClause {
	// interface{}/any is a passthrough slot with no pointer indirection.
	se := ast.SelectorExpr(ast.Ident(trampolineCtxIdentifier), field)
	if ast.IsInterfaceType(t) {
		ie := ast.IndexExpr(se, ast.IntLit(idx))
		assign := ast.AssignStmt(ie, ast.Ident(trampolineValIdentifier))
		return ast.SwitchCase(ast.Exprs(ast.IntLit(idx)), ast.Stmts(assign))
	}

	// Non-interface slots are written through the stored pointer. A nil val
	// can't be asserted to a non-nilable type, so write its zero value instead.
	target := func() dst.Expr {
		ie := ast.IndexExpr(se, ast.IntLit(idx))
		te := ast.TypeAssertExpr(ie, ast.DereferenceOf(t))
		return ast.DereferenceOf(ast.ParenExpr(te))
	}
	nonNilAssign := ast.AssignStmt(target(), ast.TypeAssertExpr(ast.Ident(trampolineValIdentifier), t))
	tClone := util.AssertType[dst.Expr](dst.Clone(t))
	nilAssign := ast.AssignStmt(target(), ast.DereferenceOf(ast.CallTo("new", nil, ast.Exprs(tClone))))

	ifStmt := &dst.IfStmt{
		Cond: &dst.BinaryExpr{X: ast.Ident(trampolineValIdentifier), Op: token.EQL, Y: ast.Nil()},
		Body: ast.Block(nilAssign),
		Else: ast.Block(nonNilAssign),
	}
	return ast.SwitchCase(ast.Exprs(ast.IntLit(idx)), ast.Stmts(ifStmt))
}

func getValue(field string, idx int, t dst.Expr) *dst.CaseClause {
	// return *(c.Params[idx].(*int))
	// return c.Params[idx] iff type is interface{}
	se := ast.SelectorExpr(ast.Ident(trampolineCtxIdentifier), field)
	ie := ast.IndexExpr(se, ast.IntLit(idx))
	te := ast.TypeAssertExpr(ie, ast.DereferenceOf(t))
	pe := ast.ParenExpr(te)
	de := ast.DereferenceOf(pe)
	ret := ast.ReturnStmt(ast.Exprs(de))
	if ast.IsInterfaceType(t) {
		ret = ast.ReturnStmt(ast.Exprs(ie))
	}
	caseClause := ast.SwitchCase(
		ast.Exprs(ast.IntLit(idx)),
		ast.Stmts(ret),
	)
	return caseClause
}

func getParamClause(idx int, t dst.Expr) *dst.CaseClause {
	return getValue(trampolineParamsIdentifier, idx, t)
}

func setParamClause(idx int, t dst.Expr) *dst.CaseClause {
	return setValue(trampolineParamsIdentifier, idx, t)
}

func getReturnValClause(idx int, t dst.Expr) *dst.CaseClause {
	return getValue(trampolineReturnValsIdentifier, idx, t)
}

func setReturnValClause(idx int, t dst.Expr) *dst.CaseClause {
	return setValue(trampolineReturnValsIdentifier, idx, t)
}

// extractReceiverTypeParams extracts type parameters from a receiver type expression
// For example: *GenStruct[T] or GenStruct[T, U] -> FieldList with T and U as type parameters
//
// file is the source file the receiver was declared in. When it contains the
// matching generic type declaration (e.g. type GenStruct[T comparable] struct{...}),
// each parameter's real constraint is recovered from it instead of being widened to
// any. Constraints are matched positionally against that declaration's own type
// parameters, since a method receiver is free to use different names for them
// (type GenStruct[T comparable] struct{} but func (s GenStruct[U]) M(v U) is valid
// Go; U is still constrained by comparable). If no matching declaration is found in
// file, the constraint falls back to any, as before.
func extractReceiverTypeParams(file *dst.File, recvType dst.Expr) *dst.FieldList {
	switch t := recvType.(type) {
	case *dst.StarExpr:
		// *GenStruct[T] - recurse into X
		return extractReceiverTypeParams(file, t.X)
	case *dst.IndexExpr:
		// GenStruct[T] - single type parameter
		if ident, ok := t.Index.(*dst.Ident); ok {
			original := findGenericTypeDecl(file, receiverBaseTypeName(t.X))
			return &dst.FieldList{
				List: []*dst.Field{{
					Names: []*dst.Ident{ident},
					Type:  receiverConstraintAt(original, 0),
				}},
			}
		}
	case *dst.IndexListExpr:
		// GenStruct[T, U, ...] - multiple type parameters
		original := findGenericTypeDecl(file, receiverBaseTypeName(t.X))
		fields := make([]*dst.Field, 0, len(t.Indices))
		for i, idx := range t.Indices {
			if ident, ok := idx.(*dst.Ident); ok {
				fields = append(fields, &dst.Field{
					Names: []*dst.Ident{ident},
					Type:  receiverConstraintAt(original, i),
				})
			}
		}
		if len(fields) > 0 {
			return &dst.FieldList{List: fields}
		}
	}
	return nil
}

// receiverBaseTypeName returns the local identifier naming a receiver's generic
// type, e.g. "GenStruct" for the X in GenStruct[T]. Only a plain identifier can be
// looked up in file's own declarations, so anything else (there is currently no
// valid Go receiver form that produces anything else) returns "".
func receiverBaseTypeName(x dst.Expr) string {
	if ident, ok := x.(*dst.Ident); ok {
		return ident.Name
	}
	return ""
}

// findGenericTypeDecl searches file's top-level declarations for a generic type
// declaration named typeName and returns its type parameter list (with each
// parameter's real constraint, not any), or nil if no such declaration is found in
// file. The declaration may live in a different file of the same package; that case
// is not resolved today, so it falls through to the caller's any fallback.
func findGenericTypeDecl(file *dst.File, typeName string) *dst.FieldList {
	if file == nil || typeName == "" {
		return nil
	}
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, isTypeSpec := spec.(*dst.TypeSpec)
			if isTypeSpec && typeSpec.Name.Name == typeName {
				return typeSpec.TypeParams
			}
		}
	}
	return nil
}

// receiverConstraintAt returns a clone of the constraint type at position idx in
// original's type parameter list, or the any identifier if original is nil or has
// no parameter at that position. original's fields are walked positionally, rather
// than indexed directly, because a single field can carry multiple names sharing one
// constraint (e.g. [T, U comparable]).
func receiverConstraintAt(original *dst.FieldList, idx int) dst.Expr {
	if original != nil {
		pos := 0
		for _, field := range original.List {
			n := len(field.Names)
			if n == 0 {
				n = 1
			}
			if idx < pos+n {
				return util.AssertType[dst.Expr](dst.Clone(field.Type))
			}
			pos += n
		}
	}
	return ast.Ident("any") // Type constraint for the parameter
}

// desugarType desugars parameter type to its original type, if parameter
// is type of ...T, it will be converted to []T
func desugarType(param *dst.Field) dst.Expr {
	if ft, ok := param.Type.(*dst.Ellipsis); ok {
		return ast.ArrayType(ft.Elt)
	}
	return param.Type
}

func rewriteParamMethods(targetFunc *dst.FuncDecl, methodSetParam, methodGetParam *dst.BlockStmt) {
	idx := 0
	if ast.HasReceiver(targetFunc) {
		recvType := targetFunc.Recv.List[0].Type
		clause := setParamClause(idx, recvType)
		methodSetParam.List = append(methodSetParam.List, clause)
		clause = getParamClause(idx, recvType)
		methodGetParam.List = append(methodGetParam.List, clause)
		idx++
	}
	for _, param := range targetFunc.Type.Params.List {
		paramType := desugarType(param)
		for range param.Names {
			clause := setParamClause(idx, paramType)
			methodSetParam.List = append(methodSetParam.List, clause)
			clause = getParamClause(idx, paramType)
			methodGetParam.List = append(methodGetParam.List, clause)
			idx++
		}
	}
}

func rewriteReturnValMethods(targetFunc *dst.FuncDecl, methodSetRetVal, methodGetRetVal *dst.BlockStmt) {
	idx := 0
	for _, retval := range targetFunc.Type.Results.List {
		retType := desugarType(retval)
		for range retval.Names {
			clause := getReturnValClause(idx, retType)
			methodGetRetVal.List = append(methodGetRetVal.List, clause)
			clause = setReturnValClause(idx, retType)
			methodSetRetVal.List = append(methodSetRetVal.List, clause)
			idx++
		}
	}
}

func (ip *instrumentPhase) rewriteHookContextMethods() {
	util.Assert(len(ip.hookCtxMethods) > 4, "sanity check")
	var methodSetParam, methodGetParam, methodGetRetVal, methodSetRetVal *dst.FuncDecl
	for _, decl := range ip.hookCtxMethods {
		switch decl.Name.Name {
		case trampolineSetParamName:
			methodSetParam = decl
		case trampolineGetParamName:
			methodGetParam = decl
		case trampolineGetReturnValName:
			methodGetRetVal = decl
		case trampolineSetReturnValName:
			methodSetRetVal = decl
		}
	}

	// For generic functions, we need to panic the methods that are not supported
	if findTargetGenericType(ip.target, ip.targetFunc) != nil {
		makeMethodPanic(methodGetParam, "GetParam is unsupported for generic functions")
		makeMethodPanic(methodGetRetVal, "GetReturnVal is unsupported for generic functions")
		makeMethodPanic(methodSetParam, "SetParam is unsupported for generic functions")
		makeMethodPanic(methodSetRetVal, "SetReturnVal is unsupported for generic functions")
		return
	}

	// nil handling now lives inside each case, so the type switch is always the
	// first statement of every Get/Set method.
	findSwitchBlock := func(fn *dst.FuncDecl) *dst.BlockStmt {
		stmt := util.AssertType[*dst.SwitchStmt](fn.Body.List[0])
		body := stmt.Body
		body.List = nil
		return body
	}
	methodSetParamBody := findSwitchBlock(methodSetParam)
	methodGetParamBody := findSwitchBlock(methodGetParam)
	rewriteParamMethods(ip.targetFunc, methodSetParamBody, methodGetParamBody)

	// Rewrite SetReturnVal and GetReturnVal methods
	if ip.targetFunc.Type.Results != nil {
		methodSetRetValBody := findSwitchBlock(methodSetRetVal)
		methodGetRetValBody := findSwitchBlock(methodGetRetVal)
		rewriteReturnValMethods(ip.targetFunc, methodSetRetValBody, methodGetRetValBody)
	}
}

// makeMethodPanic replaces a method's body with a panic statement
func makeMethodPanic(method *dst.FuncDecl, message string) {
	panicStmt := ast.ExprStmt(
		ast.CallTo("panic", nil, []dst.Expr{
			&dst.BasicLit{
				Kind:  token.STRING,
				Value: strconv.Quote(message),
			},
		}),
	)
	method.Body.List = []dst.Stmt{panicStmt}
}

// isTypeParameter checks if a type expression is a bare type parameter identifier
func isTypeParameter(t dst.Expr, typeParams *dst.FieldList) bool {
	if typeParams == nil {
		return false
	}
	ident, ok := t.(*dst.Ident)
	if !ok {
		return false
	}
	// Check if this identifier matches any type parameter name
	for _, field := range typeParams.List {
		for _, name := range field.Names {
			if name.Name == ident.Name {
				return true
			}
		}
	}
	return false
}

// replaceTypeParamsWithAny replaces type parameters with interface{} for use in
// non-generic contexts like HookContextImpl methods
func replaceTypeParamsWithAny(t dst.Expr, typeParams *dst.FieldList) dst.Expr {
	if isTypeParameter(t, typeParams) {
		return ast.InterfaceType()
	}

	// For complex types like *T, []T, map[K]V, etc., handle them recursively
	switch tType := t.(type) {
	case *dst.StarExpr:
		// *T -> *interface{}
		return ast.DereferenceOf(replaceTypeParamsWithAny(tType.X, typeParams))
	case *dst.ArrayType:
		// []T -> []interface{}
		return ast.ArrayType(replaceTypeParamsWithAny(tType.Elt, typeParams))
	case *dst.MapType:
		// map[K]V -> map[interface{}]interface{}
		return &dst.MapType{
			Key:   replaceTypeParamsWithAny(tType.Key, typeParams),
			Value: replaceTypeParamsWithAny(tType.Value, typeParams),
		}
	case *dst.ChanType:
		// chan T, <-chan T, chan<- T -> chan interface{}, etc.
		return &dst.ChanType{
			Dir:   tType.Dir,
			Value: replaceTypeParamsWithAny(tType.Value, typeParams),
		}
	case *dst.IndexExpr:
		// GenStruct[T] -> interface{} (for generic receiver methods)
		// The hook function expects interface{} for generic types
		return ast.InterfaceType()
	case *dst.IndexListExpr:
		// GenStruct[T, U] -> interface{} (for generic receiver methods with multiple type params)
		return ast.InterfaceType()
	case *dst.Ellipsis:
		// ...T -> []T
		// Preserve variadic syntax. This maintains variadic semantics in the
		// generated hook signatures
		return ast.Ellipsis(replaceTypeParamsWithAny(tType.Elt, typeParams))
	case *dst.FuncType:
		newFuncType := &dst.FuncType{}
		if tType.Params != nil {
			newFuncType.Params = &dst.FieldList{
				List: processFieldList(tType.Params.List, typeParams),
			}
		}
		if tType.Results != nil {
			newFuncType.Results = &dst.FieldList{
				List: processFieldList(tType.Results.List, typeParams),
			}
		}
		return newFuncType
	case *dst.Ident, *dst.SelectorExpr, *dst.InterfaceType:
		// Base types without type parameters, return as-is
		return t
	default:
		// Unsupported cases:
		// - Other uncommon type expressions
		util.Unimplemented(fmt.Sprintf("unexpected generic type: %T", tType))
		return t
	}
}

func processFieldList(fields []*dst.Field, typeParams *dst.FieldList) []*dst.Field {
	result := make([]*dst.Field, len(fields))
	for i, field := range fields {
		newField := &dst.Field{}
		if field.Names != nil {
			newField.Names = make([]*dst.Ident, len(field.Names))
			for j, name := range field.Names {
				newField.Names[j] = dst.NewIdent(name.Name)
			}
		}
		newField.Type = replaceTypeParamsWithAny(field.Type, typeParams)
		result[i] = newField
	}
	return result
}

func (ip *instrumentPhase) callHookFunc(t *rule.InstFuncRule, before bool) error {
	// Build hook function signature and check if it is correct
	paramTypes, err := ip.buildHookSignature(t, before)
	if err != nil {
		return err
	}
	// Add the body-less real hook function declaration. They will be linked to
	// the real hook function.
	err = ip.addHookDecl(t, paramTypes, before)
	if err != nil {
		return err
	}
	// Add the function call to the real hook code.
	if before {
		ip.callBeforeHook(t)
	} else {
		ip.callAfterHook(t)
	}
	// Fulfill the hook context before calling the real hook code.
	if !ip.populateHookContext(before) {
		return ex.New("failed to populate hook context")
	}
	return nil
}

func (ip *instrumentPhase) createTrampoline(t *rule.InstFuncRule) error {
	// Ensure unsafe package is imported since we use //go:linkname directives
	ip.ensureUnsafeImport()
	// Materialize various declarations from template file, no one wants to see
	// a bunch of manual AST code generation, isn't it?
	err := ip.materializeTemplate()
	if err != nil {
		return err
	}
	// Implement HookContext interface methods dynamically
	ip.implementHookContext(t)
	// Make all HookContext methods type-aware according to the target function
	// signature.
	ip.rewriteHookContextMethods()
	// Rename template function to trampoline function
	ip.renameTrampFunc(t)
	// Build types of trampoline functions. The parameters of the Before trampoline
	// function are the same as the target function, the parameters of the After
	// trampoline function are the same as the target function.
	// Generate calls to real hook functions
	if t.Before != "" {
		ip.buildTrampSignature(trampolineBefore)
		err = ip.callHookFunc(t, trampolineBefore)
		if err != nil {
			return err
		}
	}
	if t.After != "" {
		ip.buildTrampSignature(trampolineAfter)
		err = ip.callHookFunc(t, trampolineAfter)
		if err != nil {
			return err
		}
	}
	return nil
}
