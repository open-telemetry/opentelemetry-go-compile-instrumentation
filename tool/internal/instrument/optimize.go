// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// -----------------------------------------------------------------------------
// Trampoline Optimization
//
// Since trampoline-jump-if and trampoline functions are performance-critical,
// we are trying to optimize them as much as possible. The standard form of
// trampoline-jump-if looks like
//
//	if ctx, skip := otel_trampoline_before(&arg); skip {
//	    otel_trampoline_after(ctx, &retval)
//	    return ...
//	} else {
//	    defer otel_trampoline_after(ctx, &retval)
//	    ...
//	}
//
// The obvious optimization opportunities are cases when Before or After hooks
// are not present. For the latter case, we can replace the defer statement to
// empty statement, you might argue that we can remove the whole else block, since
// there might be more than one trampoline-jump-if in the same function, they are
// nested in the else block, i.e.
//
//	if ctx, skip := otel_trampoline_before(&arg); skip {
//	    otel_trampoline_after(ctx, &retval)
//	    return ...
//	} else {
//	    ;
//	    ...
//	}
//
// For the former case, it's a bit more complicated. We need to manually construct
// HookContext on the fly and pass it to After trampoline defer call and rewrite
// the whole condition to always false. The corresponding code snippet is
//
//	if false {
//	    ;
//	} else {
//	    defer otel_trampoline_after(&HookContext{...}, &retval)
//	    ...
//	}
//
// The If skeleton should be kept as is, otherwise inlining of trampoline-jump-if
// will not work. During compiling, the DCE and SCCP passes will remove the whole
// then block. That's not the whole story. We can further optimize the tjump iff
// the Before hook does not use SkipCall. In this case, we can rewrite condition
// of trampoline-jump-if to always false, remove return statement in then block,
// they are memory-aware and may generate memory SSA values during compilation.
//
//	if ctx,_ := otel_trampoline_before(&arg); false {
//	    ;
//	} else {
//	    defer otel_trampoline_after(ctx, &retval)
//	    ...
//	}
//
// The compiler responsible for hoisting the initialization statement out of the
// if skeleton, and the dce and sccp passes will remove the whole then block. All
// these trampoline functions looks as if they are executed sequentially, i.e.
//
//	ctx,_ := otel_trampoline_before(&arg);
//	defer otel_trampoline_after(ctx, &retval)
//
// Note that this optimization pass is fragile as it really heavily depends on
// the structure of trampoline-jump-if and trampoline functions. Any change in
// tjump should be carefully examined.

// TJump describes a trampoline-jump-if optimization candidate
type TJump struct {
	target *dst.FuncDecl      // Target function we are hooking on
	ifStmt *dst.IfStmt        // Trampoline-jump-if statement
	rule   *rule.InstFuncRule // Rule associated with the trampoline-jump-if
}

func mustTJump(ifStmt *dst.IfStmt) {
	util.Assert(len(ifStmt.Decs.If) == 1, "must be a trampoline-jump-if")
	desc := ifStmt.Decs.If[0]
	util.Assert(desc == tJumpLabel, "must be a trampoline-jump-if")
}

func removeAfterTrampolineCall(tjump *TJump) error {
	ifStmt := tjump.ifStmt
	elseBlock, ok := ifStmt.Else.(*dst.BlockStmt)
	util.Assert(ok, "else block is not a BlockStmt")
	for i, stmt := range elseBlock.List {
		switch stmt.(type) {
		case *dst.DeferStmt:
			// Replace defer statement with an empty statement
			elseBlock.List[i] = ast.EmptyStmt()
		case *dst.IfStmt, *dst.EmptyStmt:
			// Expected statement type and do nothing
		default:
			util.ShouldNotReachHere()
		}
	}
	return nil
}

// newHookContextImpl constructs a new HookContextImpl structure literal and
// populates its Params && ReturnValues field with addresses of all arguments.
// The HookContextImpl structure is used to pass arguments to the exit trampoline
func newHookContextImpl(tjump *TJump) (dst.Expr, error) {
	targetFunc := tjump.target
	structName := "HookContextImpl" + util.CRC32(tjump.rule.String())

	// Build params slice: []interface{}{}
	paramNames := make([]dst.Expr, 0)
	for _, name := range getNames(targetFunc.Type.Params) {
		paramNames = append(paramNames, ast.AddressOf(ast.Ident(name)))
	}
	paramsSlice := &dst.CompositeLit{
		Type: ast.ArrayType(ast.InterfaceType()),
		Elts: paramNames,
	}

	// Build returnVals slice: []interface{}{}
	returnElts := make([]dst.Expr, 0)
	if targetFunc.Type.Results != nil {
		for _, name := range getNames(targetFunc.Type.Results) {
			returnElts = append(returnElts, ast.AddressOf(ast.Ident(name)))
		}
	}
	returnValsSlice := &dst.CompositeLit{
		Type: ast.ArrayType(ast.InterfaceType()),
		Elts: returnElts,
	}

	// Build the struct literal: &HookContextImpl{params:..., returnVals:...}
	ctxExpr := &dst.UnaryExpr{
		Op: token.AND,
		X: &dst.CompositeLit{
			Type: ast.Ident(structName),
			Elts: []dst.Expr{
				&dst.KeyValueExpr{
					Key:   ast.Ident("params"),
					Value: paramsSlice,
				},
				&dst.KeyValueExpr{
					Key:   ast.Ident("returnVals"),
					Value: returnValsSlice,
				},
			},
		},
	}

	return ctxExpr, nil
}

func removeBeforeTrampolineCall(targetFile *dst.File, tjump *TJump) error {
	// Construct HookContext on the fly and pass to After trampoline defer call
	callContextExpr, err := newHookContextImpl(tjump)
	if err != nil {
		return err
	}
	// Find defer call to After and replace its call context with new one
	found := false
	block, ok := tjump.ifStmt.Else.(*dst.BlockStmt)
	util.Assert(ok, "else block is not a BlockStmt")
	for _, stmt := range block.List {
		// Replace call context argument of defer statement to structure literal
		if deferStmt, ok1 := stmt.(*dst.DeferStmt); ok1 {
			args := deferStmt.Call.Args
			util.Assert(len(args) >= 1, "must have at least one argument")
			args[0] = callContextExpr
			found = true
			break
		}
	}
	util.Assert(found, "defer statement not found")
	// Rewrite condition of trampoline-jump-if to always false and null out its
	// initialization statement and then block
	tjump.ifStmt.Init = nil
	tjump.ifStmt.Cond = ast.BoolFalse()
	tjump.ifStmt.Body = ast.Block(ast.EmptyStmt())
	// Remove generated Before trampoline function
	for i, decl := range targetFile.Decls {
		if funcDecl, ok1 := decl.(*dst.FuncDecl); ok1 {
			if funcDecl.Name.Name == makeName(tjump.rule, tjump.target, true) {
				targetFile.Decls = append(targetFile.Decls[:i], targetFile.Decls[i+1:]...)
				return nil
			}
		}
	}
	return ex.Newf("can not remove Before trampoline function")
}

func stripTJumpLabel(tjump *TJump) {
	ifStmt := tjump.ifStmt
	ifStmt.Decs.If = ifStmt.Decs.If[1:]
}

func (ip *InstrumentPhase) optimizeTJumps() error {
	for _, tjump := range ip.tjumps {
		mustTJump(tjump.ifStmt)
		// Strip the trampoline-jump-if anchor label as no longer needed
		stripTJumpLabel(tjump)

		// No After hook present? Simply remove defer call to After trampoline.
		// Why we don't remove the whole else block of trampoline-jump-if? Well,
		// because there might be more than one trampoline-jump-if in the same
		// function, they are nested in the else block. See findJumpPoint for
		// more details.
		// TODO: Remove corresponding HookContextImpl methods
		rule := tjump.rule
		if rule.After == "" {
			return removeAfterTrampolineCall(tjump)
		}

		// No Before hook present? Construct HookContext on the fly and pass it
		// to After trampoline defer call and rewrite the whole condition to
		// always false, then null out its initialization statement.
		if rule.Before == "" {
			return removeBeforeTrampolineCall(ip.target, tjump)
		}
	}
	return nil
}
