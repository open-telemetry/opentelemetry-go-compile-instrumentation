// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	_ "embed"
	"fmt"
	"go/parser"
	"path/filepath"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	tJumpLabel      = "/* __TRAMPOLINE_JUMP_IF__ */"
	otelGlobalsFile = "otel.globals.go"
)

func makeName(r *rule.InstFuncRule, funcDecl *dst.FuncDecl, isBefore bool) string {
	prefix := trampolineAfterName
	if isBefore {
		prefix = trampolineBeforeName
	}
	return fmt.Sprintf("%s_%s%s",
		prefix, funcDecl.Name.Name, util.CRC32(r.String()))
}

func findJumpPoint(jumpIf *dst.IfStmt) *dst.BlockStmt {
	// Multiple func rules may apply to the same function, we need to find the
	// appropriate jump point to insert trampoline jump.
	if len(jumpIf.Decs.If) == 1 && jumpIf.Decs.If[0] == tJumpLabel {
		// Insert trampoline jump within the else block
		elseBlock := util.AssertType[*dst.BlockStmt](jumpIf.Else)
		if len(elseBlock.List) > 1 {
			// One trampoline jump already exists, recursively find last one
			ifStmt := util.AssertType[*dst.IfStmt](elseBlock.List[len(elseBlock.List)-1])
			return findJumpPoint(ifStmt)
		}
		// Otherwise, this is the appropriate jump point
		return elseBlock
	}
	return nil
}

func collectReturnValues(funcDecl *dst.FuncDecl) []string {
	// Add explicit names for return values, they can be further referenced if
	// we're willing
	var retVals []string // nil by default
	if retList := funcDecl.Type.Results; retList != nil {
		idx := 0
		for _, field := range retList.List {
			if field.Names == nil {
				// Rename (for referenceability)
				name := fmt.Sprintf("%s%d", unnamedRetValName, idx)
				field.Names = []*dst.Ident{ast.Ident(name)}
				idx++
				// Collect (for further use)
				retVals = append(retVals, name)
			} else {
				// Collect only (for further use)
				for _, name := range field.Names {
					retVals = append(retVals, name.Name)
				}
			}
		}
	}

	return retVals
}

func collectArguments(funcDecl *dst.FuncDecl) []string {
	args := make([]string, 0)
	if ast.HasReceiver(funcDecl) {
		receiver := funcDecl.Recv.List[0].Names[0].Name
		args = append(args, receiver)
	}
	for _, field := range funcDecl.Type.Params.List {
		for _, name := range field.Names {
			args = append(args, name.Name)
		}
	}
	return args
}

func createHookArgs(names []string) []dst.Expr {
	exprs := make([]dst.Expr, 0)
	// If we find "a type" in target func, we pass "&a" to trampoline func,
	// if we find "_ type" in target func, we pass "nil" to trampoline func,
	for _, name := range names {
		if name == ast.IdentIgnore {
			exprs = append(exprs, ast.Nil())
		} else {
			exprs = append(exprs, ast.AddressOf(name))
		}
	}
	return exprs
}

func createTJumpIf(t *rule.InstFuncRule, funcDecl *dst.FuncDecl,
	args, retVals []string,
) *dst.IfStmt {
	funcSuffix := util.CRC32(t.String())
	// Transparently pass the target function's parameters to trampoline func,
	// with the only exception being that if the target func parameter is "_",
	// then we directly pass "nil"
	argsToBefore := createHookArgs(args)
	argsToAfter := createHookArgs(retVals)
	argHookContext := ast.Ident(trampolineHookContextName + funcSuffix)
	argsToAfter = append([]dst.Expr{argHookContext}, argsToAfter...)
	beforeCall := ast.CallTo(makeName(t, funcDecl, true), funcDecl.Type.TypeParams, argsToBefore)
	afterCall := ast.CallTo(makeName(t, funcDecl, false), funcDecl.Type.TypeParams, argsToAfter)
	tjumpInit := ast.DefineStmts(
		ast.Exprs(
			ast.Ident(trampolineHookContextName+funcSuffix),
			ast.Ident(trampolineSkipName+funcSuffix),
		),
		ast.Exprs(beforeCall),
	)
	tjumpCond := ast.Ident(trampolineSkipName + funcSuffix)
	tjumpReturn := make([]dst.Expr, 0)
	for _, retVal := range retVals {
		tjumpReturn = append(tjumpReturn, ast.Ident(retVal))
	}
	tjumpBody := ast.BlockStmts(
		ast.ExprStmt(afterCall),
		ast.ReturnStmt(tjumpReturn),
	)
	tjumpElse := ast.Block(ast.DeferStmt(afterCall))
	tjump := ast.IfStmt(tjumpInit, tjumpCond, tjumpBody, tjumpElse)
	tjump.Decs.If.Append(tJumpLabel)
	return tjump
}

func (ip *InstrumentPhase) insertToFunc(funcDecl *dst.FuncDecl, tjump *dst.IfStmt) {
	found := false
	if len(funcDecl.Body.List) > 0 {
		firstStmt := funcDecl.Body.List[0]
		if ifStmt, ok := firstStmt.(*dst.IfStmt); ok {
			point := findJumpPoint(ifStmt)
			if point != nil {
				point.List = append(point.List, ast.EmptyStmt())
				point.List = append(point.List, tjump)
				found = true
			}
		}
	}
	if !found {
		// Tag the trampoline-jump-if with a special line directive so that
		// debugger can show the correct line number
		tjump.Decs.Before = dst.NewLine
		tjump.Decs.Start.Append("//line <generated>:1")
		pos := ip.parser.FindPosition(funcDecl.Body)
		if len(funcDecl.Body.List) > 0 {
			// It does happens because we may insert raw code snippets at the
			// function entry. These dynamically generated nodes do not have
			// corresponding node positions. We need to keep looking downward
			// until we find a node that contains position information, and then
			// annotate it with a line directive.
			for _, stmt := range funcDecl.Body.List {
				pos = ip.parser.FindPosition(stmt)
				if !pos.IsValid() {
					continue
				}
				tag := fmt.Sprintf("//line %s", pos.String())
				stmt.Decorations().Before = dst.NewLine
				stmt.Decorations().Start.Append(tag)
			}
		} else {
			tag := fmt.Sprintf("//line %s", pos.String())
			empty := ast.EmptyStmt()
			empty.Decs.Before = dst.NewLine
			empty.Decs.Start.Append(tag)
			funcDecl.Body.List = append(funcDecl.Body.List, empty)
		}
		funcDecl.Body.List = append([]dst.Stmt{tjump}, funcDecl.Body.List...)
	}
}

func (ip *InstrumentPhase) insertTJump(t *rule.InstFuncRule, funcDecl *dst.FuncDecl) error {
	util.Assert(funcDecl.Name.Name == t.Func, "sanity check")

	// Record the target function for the whole trampoline creation process
	ip.targetFunc = funcDecl

	// Collect return values from target function
	retVals := collectReturnValues(funcDecl)

	// Collect all arguments from target function, including the receiver
	args := collectArguments(funcDecl)

	// Generate the trampoline-jump-if. The trampoline-jump-if is a conditional
	// jump that jumps to the trampoline function, it looks something like this
	//
	//	if ctx, skip := otel_trampoline_before(&arg); skip {
	//	    otel_trampoline_after(ctx, &retval)
	//	    return ...
	//	} else {
	//	    defer otel_trampoline_after(ctx, &retval)
	//	    ...
	//	}
	//
	// The trampoline function is just a relay station that properly assembles
	// the context, handles exceptions, etc, and ultimately jumps to the real
	// hook code. By inserting trampoline-jump-if at the target function entry,
	// we can intercept the original function and execute before/after hooks.
	tjump := createTJumpIf(t, funcDecl, args, retVals)

	// Record the trampoline-jump-if as they can be optimized later, they are
	// performance-critical
	ip.tjumps = append(ip.tjumps, &TJump{target: funcDecl, ifStmt: tjump, rule: t})

	// Find if there is already a trampoline-jump-if, insert new tjump if so,
	// otherwise prepend to block body.
	ip.insertToFunc(funcDecl, tjump)

	// Trampoline-jump-if ultimately jumps to the trampoline function, which
	// typically has the following form
	//
	//	func otel_trampoline_before(arg) (HookContext, bool) {
	//	    defer func () { /* handle panic */ }()
	//	    // prepare hook context for real hook code
	//	    hookctx := &HookContextImpl_abc{}
	//	    ...
	//	    // Call the real hook code
	//		realHook(ctx, arg)
	//	    return ctx, skip
	//	}
	//
	// It catches any potential panic from the real hook code, and prepare the
	// hook context for the real hook code. Once all preparations are done, it
	// jumps to the real hook code. Note that each trampoline has its own hook
	// context implementation, which is generated dynamically.
	return ip.createTrampoline(t)
}

func (ip *InstrumentPhase) addCompileArg(newArg string) {
	ip.compileArgs = append(ip.compileArgs, newArg)
}

//go:embed api.tmpl
var templateAPI string

func (ip *InstrumentPhase) writeGlobals(pkgName string) error {
	// Prepare trampoline code header
	p := ast.NewAstParser()
	trampoline, err := p.ParseSource("package " + pkgName)
	if err != nil {
		return err
	}
	// Declare common variable declarations
	trampoline.Decls = append(trampoline.Decls, ip.varDecls...)

	// Declare the hook context interface
	api, err := p.ParseSource(templateAPI)
	if err != nil {
		return err
	}
	trampoline.Decls = append(trampoline.Decls, api.Decls...)

	// Write trampoline code to file
	path := filepath.Join(ip.workDir, otelGlobalsFile)
	err = ast.WriteFile(path, trampoline)
	if err != nil {
		return err
	}
	ip.addCompileArg(path)
	ip.keepForDebug(path)
	return nil
}

func (ip *InstrumentPhase) writeInstrumented(root *dst.File, oldFile string) error {
	// Write the instrumented AST to the new file in the working directory
	newFile := filepath.Join(ip.workDir, filepath.Base(oldFile))
	err := ast.WriteFile(newFile, root)
	if err != nil {
		return err
	}
	ip.keepForDebug(newFile)

	// Replace the original file with the new file in the compile command
	replace := false
	for i, arg := range ip.compileArgs {
		// Files in the compile command maybe relative or absolute, we need to
		// consolidate them to absolute path
		abs, err1 := filepath.Abs(arg)
		if err1 != nil {
			return ex.Wrap(err1)
		}
		if abs == oldFile {
			ip.compileArgs[i] = newFile
			replace = true
			break
		}
	}
	if !replace {
		return ex.Newf("cannot replace %s with %s during %v",
			oldFile, newFile, ip.compileArgs)
	}
	ip.Info("Write instrumented AST", "old", oldFile, "new", newFile)
	return nil
}

func (ip *InstrumentPhase) parseFile(file string) (*dst.File, error) {
	ip.parser = ast.NewAstParser()
	root, err := ip.parser.Parse(file, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	ip.target = root
	// Every time we parse a file, we need to reset the trampoline jumps
	// because they are associated with one certain file
	ip.tjumps = make([]*TJump, 0)
	return root, nil
}

func (ip *InstrumentPhase) applyFuncRule(rule *rule.InstFuncRule, root *dst.File) error {
	funcDecl := ast.FindFuncDecl(root, rule.Func, rule.Recv)
	// No function found for the rule, skip
	if funcDecl == nil {
		return ex.Newf("can not find function %s", rule.Func)
	}

	err := ip.insertTJump(rule, funcDecl)
	if err != nil {
		return err
	}
	ip.Info("Apply func rule", "rule", rule)
	return nil
}
