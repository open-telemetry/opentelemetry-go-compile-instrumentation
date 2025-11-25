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
	unsafeImport := ast.ImportDecl(ast.IdentIgnore, unsafePackageName)
	ip.target.Decls = append([]dst.Decl{unsafeImport}, ip.target.Decls...)
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
	splitParams := ast.SplitMultiNameFields(target.Type.Params)
	// Find which parameter is type of interface{}
	for i, field := range splitParams.List {
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
	call := ast.ExprStmt(ast.CallTo(fnName, nil, args))
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
	call := ast.ExprStmt(ast.CallTo(fnName, nil, args))
	iff := ast.IfNotNilStmt(
		ast.Ident(fnName),
		ast.Block(call),
		nil,
	)
	insertAtEnd(ip.afterHookFunc, iff)
	return nil
}

// replaceTypeWithAny replaces parameter types with interface{} based on generic type parameters.
func replaceTypeWithAny(traits []ParamTrait, paramTypes, genericTypes *dst.FieldList) error {
	if len(paramTypes.List) != len(traits) {
		return ex.New("hook func signature can not match with target function")
	}

	for i, field := range paramTypes.List {
		trait := traits[i]
		if trait.IsInterfaceAny {
			// Hook explicitly uses interface{} for this parameter
			field.Type = ast.InterfaceType()
		} else {
			// Replace type parameters with interface{} (for linkname compatibility)
			field.Type = replaceTypeParamsWithAny(field.Type, genericTypes)
		}
	}
	return nil
}

func (ip *InstrumentPhase) addHookFuncVar(t *rule.InstFuncRule,
	traits []ParamTrait, before bool,
) error {
	paramTypes, genericTypes := ip.buildTrampolineType(before)
	addHookContext(paramTypes)
	err := replaceTypeWithAny(traits, paramTypes, genericTypes)
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

//nolint:revive // Return types are clear from context and usage. It collides with nonamedreturns
func (ip *InstrumentPhase) buildTrampolineType(before bool) (*dst.FieldList, *dst.FieldList) {
	// Since target function parameter names might be "_", we may use the target
	// function parameters in the trampoline function, which would cause a syntax
	// error, so we assign them a specific name and use them.
	idx := 0
	renameField := func(field *dst.Field, prefix string) {
		for _, names := range field.Names {
			names.Name = fmt.Sprintf("%s%d", prefix, idx)
			idx++
		}
	}
	// Build parameter list of trampoline function.
	// For before trampoline, it's signature is:
	// func S(h* HookContext, recv type, arg1 type, arg2 type, ...)
	// For after trampoline, it's signature is:
	// func S(h* HookContext, arg1 type, arg2 type, ...)
	// All grouped parameters (like a, b int) are expanded into separate parameters (a int, b int)
	paramTypes := &dst.FieldList{List: []*dst.Field{}}
	if before {
		if ast.HasReceiver(ip.targetFunc) {
			splitRecv := ast.SplitMultiNameFields(ip.targetFunc.Recv)
			recvField := util.AssertType[*dst.Field](dst.Clone(splitRecv.List[0]))
			renameField(recvField, "recv")
			paramTypes.List = append(paramTypes.List, recvField)
		}
		splitParams := ast.SplitMultiNameFields(ip.targetFunc.Type.Params)
		for _, field := range splitParams.List {
			paramField := util.AssertType[*dst.Field](dst.Clone(field))
			renameField(paramField, "param")
			paramTypes.List = append(paramTypes.List, paramField)
		}
	} else if ip.targetFunc.Type.Results != nil {
		splitResults := ast.SplitMultiNameFields(ip.targetFunc.Type.Results)
		for _, field := range splitResults.List {
			retField := util.AssertType[*dst.Field](dst.Clone(field))
			renameField(retField, "arg")
			paramTypes.List = append(paramTypes.List, retField)
		}
	}
	// Build type parameter list of trampoline function according to the target
	// function's type parameters and receiver type parameters
	genericTypes := combineTypeParams(ip.targetFunc)
	return paramTypes, ast.CloneTypeParams(genericTypes)
}

func (ip *InstrumentPhase) buildTrampolineTypes() {
	beforeHookFunc, afterHookFunc := ip.beforeHookFunc, ip.afterHookFunc
	beforeHookFunc.Type.Params, beforeHookFunc.Type.TypeParams = ip.buildTrampolineType(true)
	afterHookFunc.Type.Params, afterHookFunc.Type.TypeParams = ip.buildTrampolineType(false)
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
	structType := util.AssertType[*dst.TypeSpec](ip.hookCtxDecl.Specs[0])
	util.Assert(structType.Name.Name == trampolineHookContextImplType,
		"sanity check")
	structType.Name.Name += suffix             // type declaration
	for _, method := range ip.hookCtxMethods { // method declaration
		t1 := util.AssertType[*dst.StarExpr](method.Recv.List[0].Type)
		t2 := util.AssertType[*dst.Ident](t1.X)
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

// extractReceiverTypeParams extracts type parameters from a receiver type expression
// For example: *GenStruct[T] or GenStruct[T, U] -> FieldList with T and U as type parameters
func extractReceiverTypeParams(recvType dst.Expr) *dst.FieldList {
	switch t := recvType.(type) {
	case *dst.StarExpr:
		// *GenStruct[T] - recurse into X
		return extractReceiverTypeParams(t.X)
	case *dst.IndexExpr:
		// GenStruct[T] - single type parameter
		if ident, ok := t.Index.(*dst.Ident); ok {
			return &dst.FieldList{
				List: []*dst.Field{{
					Names: []*dst.Ident{ident},
					Type:  ast.Ident("any"), // Type constraint for the parameter
				}},
			}
		}
	case *dst.IndexListExpr:
		// GenStruct[T, U, ...] - multiple type parameters
		fields := make([]*dst.Field, 0, len(t.Indices))
		for _, idx := range t.Indices {
			if ident, ok := idx.(*dst.Ident); ok {
				fields = append(fields, &dst.Field{
					Names: []*dst.Ident{ident},
					Type:  ast.Ident("any"), // Type constraint for the parameter
				})
			}
		}
		if len(fields) > 0 {
			return &dst.FieldList{List: fields}
		}
	}
	return nil
}

// combineTypeParams combines type parameters from the receiver and function type parameters.
// For methods on generic types, it extracts type parameters from the receiver and merges
// them with the function's type parameters.
// Receiver type parameters come first, followed by function type parameters.
//
// Example:
//
//	Original: func (c *Container[K]) Transform[V any]() V
//	Result: [K, V]
//
//	Generated trampolines:
//	  func OtelBeforeTrampoline_Container_Transform[K comparable, V any](
//	      hookContext *HookContext,
//	      recv0 *Container[K],  // ← Uses K
//	  ) { ... }
//
//	  func OtelAfterTrampoline_Container_Transform[K comparable, V any](
//	      hookContext *HookContext,
//	      arg0 *V,  // ← Uses V (return type)
//	  ) { ... }
func combineTypeParams(targetFunc *dst.FuncDecl) *dst.FieldList {
	var trampolineTypeParams *dst.FieldList
	if ast.HasReceiver(targetFunc) {
		receiverTypeParams := extractReceiverTypeParams(targetFunc.Recv.List[0].Type)
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
	return trampolineTypeParams
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

	combinedTypeParams := combineTypeParams(ip.targetFunc)

	// Rewrite SetParam and GetParam methods
	// Don't believe what you see in template, we will null out it and rewrite
	// the whole switch statement
	findSwitchBlock := func(fn *dst.FuncDecl, idx int) *dst.BlockStmt {
		stmt := util.AssertType[*dst.SwitchStmt](fn.Body.List[idx])
		body := stmt.Body
		body.List = nil
		return body
	}

	// For generic functions, SetParam and SetReturnVal should panic
	// as modifying parameters/return values is unsupported for generic functions
	if combinedTypeParams != nil {
		makeMethodPanic(methodSetParam, "SetParam is unsupported for generic functions")
		makeMethodPanic(methodSetRetVal, "SetReturnVal is unsupported for generic functions")
		methodGetParamBody := findSwitchBlock(methodGetParam, 0)
		methodGetRetValBody := findSwitchBlock(methodGetRetVal, 0)

		ip.rewriteHookContextParams(nil, methodGetParamBody, combinedTypeParams)
		ip.rewriteHookContextResults(nil, methodGetRetValBody, combinedTypeParams)
	} else {
		methodSetParamBody := findSwitchBlock(methodSetParam, 1)
		methodGetParamBody := findSwitchBlock(methodGetParam, 0)
		methodSetRetValBody := findSwitchBlock(methodSetRetVal, 1)
		methodGetRetValBody := findSwitchBlock(methodGetRetVal, 0)

		ip.rewriteHookContextParams(methodSetParamBody, methodGetParamBody, combinedTypeParams)
		ip.rewriteHookContextResults(methodSetRetValBody, methodGetRetValBody, combinedTypeParams)
	}
}

func (ip *InstrumentPhase) rewriteHookContextParams(
	methodSetParamBody, methodGetParamBody *dst.BlockStmt,
	combinedTypeParams *dst.FieldList,
) {
	isGeneric := combinedTypeParams != nil
	idx := 0
	if ast.HasReceiver(ip.targetFunc) {
		splitRecv := ast.SplitMultiNameFields(ip.targetFunc.Recv)
		recvType := replaceTypeParamsWithAny(splitRecv.List[0].Type, combinedTypeParams)
		if !isGeneric {
			clause := setParamClause(idx, recvType)
			methodSetParamBody.List = append(methodSetParamBody.List, clause)
		}
		clause := getParamClause(idx, recvType)
		methodGetParamBody.List = append(methodGetParamBody.List, clause)
		idx++
	}
	splitParams := ast.SplitMultiNameFields(ip.targetFunc.Type.Params)
	for _, param := range splitParams.List {
		paramType := replaceTypeParamsWithAny(desugarType(param), combinedTypeParams)
		if !isGeneric {
			clause := setParamClause(idx, paramType)
			methodSetParamBody.List = append(methodSetParamBody.List, clause)
		}
		clause := getParamClause(idx, paramType)
		methodGetParamBody.List = append(methodGetParamBody.List, clause)
		idx++
	}
}

func (ip *InstrumentPhase) rewriteHookContextResults(
	methodSetRetValBody, methodGetRetValBody *dst.BlockStmt,
	combinedTypeParams *dst.FieldList,
) {
	isGeneric := combinedTypeParams != nil
	if ip.targetFunc.Type.Results != nil {
		idx := 0
		splitResults := ast.SplitMultiNameFields(ip.targetFunc.Type.Results)
		for _, retval := range splitResults.List {
			retType := replaceTypeParamsWithAny(desugarType(retval), combinedTypeParams)
			clause := getReturnValClause(idx, retType)
			methodGetRetValBody.List = append(methodGetRetValBody.List, clause)
			if !isGeneric {
				clause = setReturnValClause(idx, retType)
				methodSetRetValBody.List = append(methodSetRetValBody.List, clause)
			}
			idx++
		}
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
	case *dst.Ident, *dst.SelectorExpr, *dst.InterfaceType:
		// Base types without type parameters, return as-is
		return t
	default:
		// Unsupported cases:
		// - *dst.FuncType (function types with type parameters)
		// - Other uncommon type expressions
		util.Unimplemented(fmt.Sprintf("unexpected generic type: %T", tType))
		return t
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
