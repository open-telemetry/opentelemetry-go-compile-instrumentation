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
	TJumpLabel      = "/* TRAMPOLINE_JUMP_IF */"
	OtelGlobalsFile = "otel.globals.go"
)

func makeName(r *rule.InstFuncRule, funcDecl *dst.FuncDecl, isBefore bool) string {
	prefix := TrampolineAfterName
	if isBefore {
		prefix = TrampolineBeforeName
	}
	return fmt.Sprintf("%s_%s%s",
		prefix, funcDecl.Name.Name, util.CRC32(r.String()))
}

func findJumpPoint(jumpIf *dst.IfStmt) *dst.BlockStmt {
	// Multiple func rules may apply to the same function, we need to find the
	// appropriate jump point to insert trampoline jump.
	if len(jumpIf.Decs.If) == 1 && jumpIf.Decs.If[0] == TJumpLabel {
		// Insert trampoline jump within the else block
		elseBlock, ok := jumpIf.Else.(*dst.BlockStmt)
		util.Assert(ok, "elseBlock is not a BlockStmt")
		if len(elseBlock.List) > 1 {
			// One trampoline jump already exists, recursively find last one
			ifStmt, ok1 := elseBlock.List[len(elseBlock.List)-1].(*dst.IfStmt)
			util.Assert(ok1, "unexpected statement in trampoline-jump-if")
			return findJumpPoint(ifStmt)
		}
		// Otherwise, this is the appropriate jump point
		return elseBlock
	}
	return nil
}

func collectReturnValues(funcDecl *dst.FuncDecl) []dst.Expr {
	// Add explicit names for return values, they can be further referenced if
	// we're willing
	var retVals []dst.Expr // nil by default
	if retList := funcDecl.Type.Results; retList != nil {
		idx := 0
		for _, field := range retList.List {
			if field.Names == nil {
				// Rename
				name := fmt.Sprintf("_retVal%d", idx)
				field.Names = []*dst.Ident{ast.Ident(name)}
				idx++
				// Collect (for further use)
				i, ok := dst.Clone(ast.Ident(name)).(*dst.Ident)
				util.Assert(ok, "ident is not a Ident")
				retVals = append(retVals, i)
			} else {
				// Collect only (for further use)
				for _, name := range field.Names {
					retVals = append(retVals, ast.Ident(name.Name))
				}
			}
		}
	}

	return retVals
}

func collectArguments(funcDecl *dst.FuncDecl) []dst.Expr {
	args := make([]dst.Expr, 0)
	if ast.HasReceiver(funcDecl) {
		receiver := funcDecl.Recv.List[0].Names[0].Name
		args = append(args, ast.AddressOf(ast.Ident(receiver)))
	}
	for _, field := range funcDecl.Type.Params.List {
		for _, name := range field.Names {
			args = append(args, ast.AddressOf(ast.Ident(name.Name)))
		}
	}
	return args
}

func createTJumpIf(t *rule.InstFuncRule, funcDecl *dst.FuncDecl,
	args []dst.Expr, retVals []dst.Expr,
) *dst.IfStmt {
	funcSuffix := util.CRC32(t.String())
	beforeCall := ast.CallTo(makeName(t, funcDecl, true), args)
	afterCall := ast.CallTo(makeName(t, funcDecl, false), func() []dst.Expr {
		// NB. DST framework disallows duplicated node in the
		// AST tree, we need to replicate the return values
		// as they are already used in return statement above
		clone := make([]dst.Expr, len(retVals)+1)
		clone[0] = ast.Ident(TrampolineHookContextName + funcSuffix)
		for i := 1; i < len(clone); i++ {
			clone[i] = ast.AddressOf(retVals[i-1])
		}
		return clone
	}())
	tjumpInit := ast.DefineStmts(
		ast.Exprs(
			ast.Ident(TrampolineHookContextName+funcSuffix),
			ast.Ident(TrampolineSkipName+funcSuffix),
		),
		ast.Exprs(beforeCall),
	)
	tjumpCond := ast.Ident(TrampolineSkipName + funcSuffix)
	tjumpBody := ast.BlockStmts(
		ast.ExprStmt(afterCall),
		ast.ReturnStmt(retVals),
	)
	tjumpElse := ast.Block(ast.DeferStmt(afterCall))
	tjump := ast.IfStmt(tjumpInit, tjumpCond, tjumpBody, tjumpElse)
	tjump.Decs.If.Append(TJumpLabel)
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
	util.Assert(t.Before != "" || t.After != "", "sanity check")

	// Collect return values for the trampoline function
	retVals := collectReturnValues(funcDecl)

	// Collect all arguments for the trampoline function, including the receiver
	// and the original target function arguments
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
	path := filepath.Join(ip.workDir, OtelGlobalsFile)
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
		if arg == oldFile {
			ip.compileArgs[i] = newFile
			replace = true
			break
		}
	}
	if !replace {
		return ex.Errorf(nil, "cannot apply the instrumented %s for %v",
			oldFile, ip.compileArgs)
	}
	ip.Info("Write instrumented AST", "old", oldFile, "new", newFile)
	return nil
}

func (ip *InstrumentPhase) applyFuncRule(rule *rule.InstFuncRule, args []string) error {
	files := make([]string, 0)

	// Find all go source files from compile command
	for _, arg := range args {
		if util.IsGoFile(arg) {
			files = append(files, arg)
		}
	}
	// Parse each go source file to see if there are any matched functions
	// and then insert tjump if so
	instrumented := false
	for _, file := range files {
		ip.parser = ast.NewAstParser()
		root, err := ip.parser.Parse(file, parser.ParseComments)
		if err != nil {
			return err
		}
		ip.target = root
		funcDecls, err := ast.FindFuncDecl(root, rule.GetFuncName())
		if err != nil {
			return err
		}
		// No function found for the rule, skip
		if len(funcDecls) == 0 {
			continue
		}
		for _, funcDecl := range funcDecls {
			ip.rawFunc = funcDecl
			err = ip.insertTJump(rule, funcDecl)
			instrumented = true
			if err != nil {
				return err
			}
			ip.Info("Apply func rule", "rule", rule, "args", args)
		}
		// Write the instrumented AST to new file and replace the original
		// file in the compile command
		err = ip.writeInstrumented(root, file)
		if err != nil {
			return err
		}
	}

	// Write globals file if any function is instrumented because injected code
	// always requires some global variables and auxiliary declarations
	if instrumented {
		return ip.writeGlobals(ip.packageName)
	}
	return nil
}
