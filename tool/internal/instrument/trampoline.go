// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	_ "embed"
	"fmt"
	"go/token"
	"strconv"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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
	trampolineSkipName              = "skip"
	trampolineSetParamName          = "SetParam"
	trampolineGetParamName          = "GetParam"
	trampolineSetReturnValName      = "SetReturnVal"
	trampolineGetReturnValName      = "GetReturnVal"
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

func (ip *InstrumentPhase) addDecl(decl dst.Decl) {
	util.Assert(ip.target != nil, "sanity check")
	ip.target.Decls = append(ip.target.Decls, decl)
}

// ensureUnsafeImport ensures that the unsafe package is imported in the target file.
// This is required when using //go:linkname directives.
func (ip *InstrumentPhase) ensureUnsafeImport() {
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
	ip.target.Decls = append([]dst.Decl{ast.ImportDecl("_", unsafePackageName)}, ip.target.Decls...)
}

func (ip *InstrumentPhase) materializeTemplate() error {
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
				ip.beforeHookFunc = decl
				ip.addDecl(decl)
			case trampolineAfterName:
				ip.afterHookFunc = decl
				ip.addDecl(decl)
			default:
				if ast.HasReceiver(decl) {
					// We know exactly this is HookContextImpl method
					t, ok1 := decl.Recv.List[0].Type.(*dst.StarExpr)
					util.Assert(ok1, "t is not a StarExpr")
					t2, ok2 := t.X.(*dst.Ident)
					util.Assert(ok2, "t2 is not a Ident")
					util.Assert(t2.Name == trampolineHookContextImplType, "sanity check")
					ip.hookCtxMethods = append(ip.hookCtxMethods, decl)
					ip.addDecl(decl)
				}
			}
		}
		// Materialize variable declarations
		if decl, ok := node.(*dst.GenDecl); ok {
			// No further processing for variable declarations, just append them
			//nolint:exhaustive
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
		ip.beforeHookFunc != nil &&
		ip.afterHookFunc != nil, "sanity check")
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

func makeOnXName(t *rule.InstFuncRule, before bool) string {
	if before {
		return t.Before
	}
	return t.After
}

type ParamTrait struct {
	Index          int
	IsVariadic     bool
	IsInterfaceAny bool
}

func isHookDefined(root *dst.File, rule *rule.InstFuncRule) bool {
	util.Assert(rule.Before != "" || rule.After != "", "hook must be set")
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
	files, err0 := listRuleFiles(rule.Path)
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

func getHookParamTraits(t *rule.InstFuncRule, before bool) ([]ParamTrait, error) {
	target, err := getHookFunc(t, before)
	if err != nil {
		return nil, err
	}
	attrs := make([]ParamTrait, 0)
	// Find which parameter is type of interface{}
	for i, field := range target.Type.Params.List {
		attr := ParamTrait{Index: i}
		if ast.IsInterfaceType(field.Type) {
			attr.IsInterfaceAny = true
		}
		if ast.IsEllipsis(field.Type) {
			attr.IsVariadic = true
		}
		attrs = append(attrs, attr)
	}
	return attrs, nil
}

func (ip *InstrumentPhase) callBeforeHook(t *rule.InstFuncRule, traits []ParamTrait) error {
	// The actual parameter list of hook function should be the same as the
	// target function
	if len(traits) != (len(ip.beforeHookFunc.Type.Params.List) + 1) {
		return ex.Newf("hook func signature mismatch, expected %d, got %d",
			len(ip.beforeHookFunc.Type.Params.List)+1, len(traits))
	}
	// Hook: 	   func beforeFoo(hookContext* HookContext, p*[]int)
	// Trampoline: func OtelBeforeTrampoline_foo(p *[]int)
	args := []dst.Expr{ast.Ident(trampolineHookContextName)}
	for idx, field := range ip.beforeHookFunc.Type.Params.List {
		trait := traits[idx+1 /*HookContext*/]
		for _, name := range field.Names { // syntax of n1,n2 type
			if trait.IsVariadic {
				args = append(args, ast.DereferenceOf(ast.Ident(name.Name+"...")))
			} else {
				args = append(args, ast.DereferenceOf(ast.Ident(name.Name)))
			}
		}
	}
	fnName := makeOnXName(t, true)
	call := ast.ExprStmt(ast.CallTo(fnName, args))
	iff := ast.IfNotNilStmt(
		ast.Ident(fnName),
		ast.Block(call),
		nil,
	)
	insertAt(ip.beforeHookFunc, iff, len(ip.beforeHookFunc.Body.List)-1)
	return nil
}

func (ip *InstrumentPhase) callAfterHook(t *rule.InstFuncRule, traits []ParamTrait) error {
	// The actual parameter list of hook function should be the same as the
	// target function
	if len(traits) != len(ip.afterHookFunc.Type.Params.List) {
		return ex.Newf("hook func signature mismatch, expected %d, got %d",
			len(ip.afterHookFunc.Type.Params.List), len(traits))
	}
	// Hook: 	   func afterFoo(ctx* HookContext, p*[]int)
	// Trampoline: func OtelAfterTrampoline_foo(ctx* HookContext, p *[]int)
	var args []dst.Expr
	for idx, field := range ip.afterHookFunc.Type.Params.List {
		if idx == 0 {
			args = append(args, ast.Ident(trampolineHookContextName))
			continue
		}
		trait := traits[idx]
		for _, name := range field.Names { // syntax of n1,n2 type
			if trait.IsVariadic {
				arg := ast.DereferenceOf(ast.Ident(name.Name + "..."))
				args = append(args, arg)
			} else {
				arg := ast.DereferenceOf(ast.Ident(name.Name))
				args = append(args, arg)
			}
		}
	}
	fnName := makeOnXName(t, false)
	call := ast.ExprStmt(ast.CallTo(fnName, args))
	iff := ast.IfNotNilStmt(
		ast.Ident(fnName),
		ast.Block(call),
		nil,
	)
	insertAtEnd(ip.afterHookFunc, iff)
	return nil
}

func rectifyAnyType(paramList *dst.FieldList, traits []ParamTrait) error {
	if len(paramList.List) != len(traits) {
		return ex.New("hook func signature can not match with target function")
	}
	for i, field := range paramList.List {
		trait := traits[i]
		if trait.IsInterfaceAny {
			// Rectify type to "interface{}"
			field.Type = ast.InterfaceType()
		}
	}
	return nil
}

func (ip *InstrumentPhase) addHookFuncVar(t *rule.InstFuncRule,
	traits []ParamTrait, before bool,
) error {
	paramTypes := ip.buildTrampolineType(before)
	addHookContext(paramTypes)
	// Hook functions may uses interface{} as parameter type, as some types of
	// raw function is not exposed
	err := rectifyAnyType(paramTypes, traits)
	if err != nil {
		return err
	}

	// Generate var decl and append it to the target file, note that many target
	// functions may match the same hook function, it's a fatal error to append
	// multiple hook function declarations to the same file, so we need to check
	// if the hook function variable is already declared in the target file
	fnName := makeOnXName(t, before)
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

func (ip *InstrumentPhase) renameTrampolineFunc(t *rule.InstFuncRule) {
	// Randomize trampoline function names
	ip.beforeHookFunc.Name.Name = makeName(t, ip.targetFunc, trampolineBefore)
	dst.Inspect(ip.beforeHookFunc, func(node dst.Node) bool {
		if basicLit, ok := node.(*dst.BasicLit); ok {
			// Replace OtelBeforeTrampolinePlaceHolder to real hook func name
			if basicLit.Value == trampolineBeforeNamePlaceholder {
				basicLit.Value = strconv.Quote(t.Before)
			}
		}
		return true
	})
	ip.afterHookFunc.Name.Name = makeName(t, ip.targetFunc, trampolineAfter)
	dst.Inspect(ip.afterHookFunc, func(node dst.Node) bool {
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

func (ip *InstrumentPhase) buildTrampolineType(before bool) *dst.FieldList {
	paramList := &dst.FieldList{List: []*dst.Field{}}
	if before {
		if ast.HasReceiver(ip.targetFunc) {
			recvField, ok := dst.Clone(ip.targetFunc.Recv.List[0]).(*dst.Field)
			util.Assert(ok, "recvField is not a Field")
			paramList.List = append(paramList.List, recvField)
		}
		for _, field := range ip.targetFunc.Type.Params.List {
			paramField, ok := dst.Clone(field).(*dst.Field)
			util.Assert(ok, "paramField is not a Field")
			paramList.List = append(paramList.List, paramField)
		}
	} else if ip.targetFunc.Type.Results != nil {
		for _, field := range ip.targetFunc.Type.Results.List {
			retField, ok := dst.Clone(field).(*dst.Field)
			util.Assert(ok, "retField is not a Field")
			paramList.List = append(paramList.List, retField)
		}
	}
	return paramList
}

func (ip *InstrumentPhase) buildTrampolineTypes() {
	beforeHookFunc, afterHookFunc := ip.beforeHookFunc, ip.afterHookFunc
	beforeHookFunc.Type.Params = ip.buildTrampolineType(true)
	afterHookFunc.Type.Params = ip.buildTrampolineType(false)
	candidate := []*dst.FieldList{
		beforeHookFunc.Type.Params,
		afterHookFunc.Type.Params,
	}
	for _, list := range candidate {
		for i := range len(list.List) {
			paramField := list.List[i]
			paramFieldType := desugarType(paramField)
			paramField.Type = ast.DereferenceOf(paramFieldType)
		}
	}
	addHookContext(afterHookFunc.Type.Params)
}

func assignString(assignStmt *dst.AssignStmt, val string) bool {
	rhs := assignStmt.Rhs
	if len(rhs) == 1 {
		rhsExpr := rhs[0]
		if basicLit, ok2 := rhsExpr.(*dst.BasicLit); ok2 {
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
func (ip *InstrumentPhase) populateHookContext(before bool) bool {
	funcDecl := ip.beforeHookFunc
	if !before {
		funcDecl = ip.afterHookFunc
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
func (ip *InstrumentPhase) implementHookContext(t *rule.InstFuncRule) {
	suffix := util.CRC32(t.String())
	structType, ok := ip.hookCtxDecl.Specs[0].(*dst.TypeSpec)
	util.Assert(ok, "structType is not a TypeSpec")
	util.Assert(structType.Name.Name == trampolineHookContextImplType,
		"sanity check")
	structType.Name.Name += suffix             // type declaration
	for _, method := range ip.hookCtxMethods { // method declaration
		t1, ok1 := method.Recv.List[0].Type.(*dst.StarExpr)
		util.Assert(ok1, "t1 is not a StarExpr")
		t2, ok2 := t1.X.(*dst.Ident)
		util.Assert(ok2, "t2 is not a Ident")
		t2.Name += suffix
	}
	for _, node := range []dst.Node{ip.beforeHookFunc, ip.afterHookFunc} {
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
	// *(c.Params[idx].(*int)) = val.(int)
	// c.Params[idx] = val iff type is interface{}
	se := ast.SelectorExpr(ast.Ident(trampolineCtxIdentifier), field)
	ie := ast.IndexExpr(se, ast.IntLit(idx))
	te := ast.TypeAssertExpr(ie, ast.DereferenceOf(t))
	pe := ast.ParenExpr(te)
	de := ast.DereferenceOf(pe)
	val := ast.Ident(trampolineValIdentifier)
	assign := ast.AssignStmt(de, ast.TypeAssertExpr(val, t))
	if ast.IsInterfaceType(t) {
		assign = ast.AssignStmt(ie, val)
	}
	caseClause := ast.SwitchCase(
		ast.Exprs(ast.IntLit(idx)),
		ast.Stmts(assign),
	)
	return caseClause
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

// desugarType desugars parameter type to its original type, if parameter
// is type of ...T, it will be converted to []T
func desugarType(param *dst.Field) dst.Expr {
	if ft, ok := param.Type.(*dst.Ellipsis); ok {
		return ast.ArrayType(ft.Elt)
	}
	return param.Type
}

func (ip *InstrumentPhase) rewriteHookContext() {
	const expectMinMethodCount = 4
	util.Assert(len(ip.hookCtxMethods) > expectMinMethodCount, "sanity check")
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
	// Rewrite SetParam and GetParam methods
	// Don't believe what you see in template, we will null out it and rewrite
	// the whole switch statement
	findSwitchBlock := func(fn *dst.FuncDecl, idx int) *dst.BlockStmt {
		stmt, ok := fn.Body.List[idx].(*dst.SwitchStmt)
		util.Assert(ok, "sanity check")
		body := stmt.Body
		body.List = nil
		return body
	}
	methodSetParamBody := findSwitchBlock(methodSetParam, 1)
	methodGetParamBody := findSwitchBlock(methodGetParam, 0)
	methodSetRetValBody := findSwitchBlock(methodSetRetVal, 1)
	methodGetRetValBody := findSwitchBlock(methodGetRetVal, 0)
	idx := 0
	if ast.HasReceiver(ip.targetFunc) {
		recvType := ip.targetFunc.Recv.List[0].Type
		clause := setParamClause(idx, recvType)
		methodSetParamBody.List = append(methodSetParamBody.List, clause)
		clause = getParamClause(idx, recvType)
		methodGetParamBody.List = append(methodGetParamBody.List, clause)
		idx++
	}
	for _, param := range ip.targetFunc.Type.Params.List {
		paramType := desugarType(param)
		for range param.Names {
			clause := setParamClause(idx, paramType)
			methodSetParamBody.List = append(methodSetParamBody.List, clause)
			clause = getParamClause(idx, paramType)
			methodGetParamBody.List = append(methodGetParamBody.List, clause)
			idx++
		}
	}
	// Rewrite GetReturnVal and SetReturnVal methods
	if ip.targetFunc.Type.Results != nil {
		idx = 0
		for _, retval := range ip.targetFunc.Type.Results.List {
			retType := desugarType(retval)
			for range retval.Names {
				clause := getReturnValClause(idx, retType)
				methodGetRetValBody.List = append(methodGetRetValBody.List, clause)
				clause = setReturnValClause(idx, retType)
				methodSetRetValBody.List = append(methodSetRetValBody.List, clause)
				idx++
			}
		}
	}
}

func (ip *InstrumentPhase) callHookFunc(t *rule.InstFuncRule, before bool) error {
	traits, err := getHookParamTraits(t, before)
	if err != nil {
		return err
	}
	// Add the body-less real hook function declaration. They will be linked to
	// the real hook function.
	err = ip.addHookFuncVar(t, traits, before)
	if err != nil {
		return err
	}
	// Add the function call to the real hook code.
	if before {
		err = ip.callBeforeHook(t, traits)
	} else {
		err = ip.callAfterHook(t, traits)
	}
	if err != nil {
		return err
	}
	// Fulfill the hook context before calling the real hook code.
	if !ip.populateHookContext(before) {
		return ex.New("failed to populate hook context")
	}
	return nil
}

func (ip *InstrumentPhase) createTrampoline(t *rule.InstFuncRule) error {
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
	// Rewrite type-aware HookContext APIs
	// Make all HookContext methods type-aware according to the target function
	// signature.
	ip.rewriteHookContext()
	// Rename template function to trampoline function
	ip.renameTrampolineFunc(t)
	// Build types of trampoline functions. The parameters of the Before trampoline
	// function are the same as the target function, the parameters of the After
	// trampoline function are the same as the target function.
	ip.buildTrampolineTypes()
	// Generate calls to real hook functions
	if t.Before != "" {
		err = ip.callHookFunc(t, trampolineBefore)
		if err != nil {
			return err
		}
	}
	if t.After != "" {
		err = ip.callHookFunc(t, trampolineAfter)
		if err != nil {
			return err
		}
	}
	return nil
}
