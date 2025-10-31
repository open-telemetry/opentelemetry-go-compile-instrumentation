// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"strings"

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
// Note that this optimization pass is fraigle as it really heavily depends on
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
		if _, ok := stmt.(*dst.DeferStmt); ok {
			// Replace defer statement with an empty statement
			elseBlock.List[i] = ast.EmptyStmt()
			break
		} else if _, ok := stmt.(*dst.IfStmt); ok {
			// Expected statement type and do nothing
		} else {
			// Unexpected statement type
			util.ShouldNotReachHere()
		}
	}
	return nil
}

func populateHookContextLiteral(tjump *TJump, expr dst.Expr) {
	rawFunc := tjump.target
	// Populate call context literal with addresses of all arguments
	names := make([]dst.Expr, 0)
	for _, name := range getNames(rawFunc.Type.Params) {
		names = append(names, ast.AddressOf(ast.Ident(name)))
	}
	elems := expr.(*dst.UnaryExpr).X.(*dst.CompositeLit).Elts
	paramLiteral := elems[0].(*dst.KeyValueExpr).Value.(*dst.CompositeLit)
	paramLiteral.Elts = names
	// Populate return values literal with addresses of all return values
	if rawFunc.Type.Results != nil {
		rets := make([]dst.Expr, 0)
		for _, name := range getNames(rawFunc.Type.Results) {
			rets = append(rets, ast.AddressOf(ast.Ident(name)))
		}
		elems = expr.(*dst.UnaryExpr).X.(*dst.CompositeLit).Elts
		returnLiteral := elems[1].(*dst.KeyValueExpr).Value.(*dst.CompositeLit)
		returnLiteral.Elts = rets
	}
}

// newHookContextImpl constructs a new HookContextImpl structure literal and
// populatees its Params && ReturnValues field with addresses of all arguments.
// The HookContextImpl structure is used to pass arguments to the exit trampoline
func newHookContextImpl(tjump *TJump) (dst.Expr, error) {
	// TODO: This generated structure construction can also be marked via line
	// directive
	// One line please, otherwise debugging line number will be a nightmare
	tmpl := fmt.Sprintf("&HookContextImpl%s{Params:[]interface{}{},ReturnVals:[]interface{}{}}",
		util.CRC32(tjump.rule.String()))
	p := ast.NewAstParser()
	astRoot, err := p.ParseSnippet(tmpl)
	if err != nil {
		return nil, err
	}
	ctxExpr, ok := astRoot[0].(*dst.ExprStmt)
	util.Assert(ok, "ctxExpr is not a ExprStmt")
	// Populate call context by passing addresses of all arguments
	populateHookContextLiteral(tjump, ctxExpr.X)
	return ctxExpr.X, nil
}

func removeBeforeTrampolineCall(targetFile *dst.File, tjump *TJump) error {
	// Construct HookContext on the fly and pass to After trampoline defer call
	callContextExpr, err := newHookContextImpl(tjump)
	if err != nil {
		return err
	}
	// Find defer call to After and replace its call context with new one
	found := false
	for _, stmt := range tjump.ifStmt.Else.(*dst.BlockStmt).List {
		// Replace call context argument of defer statement to structure literal
		if deferStmt, ok := stmt.(*dst.DeferStmt); ok {
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
		if funcDecl, ok := decl.(*dst.FuncDecl); ok {
			if funcDecl.Name.Name == makeName(tjump.rule, tjump.target, true) {
				targetFile.Decls = append(targetFile.Decls[:i], targetFile.Decls[i+1:]...)
				return nil
			}
		}
	}
	return ex.Newf("can not remove Before trampoline function")
}
func flattenTJump(tjump *TJump, removedAfter bool) {
	ifStmt := tjump.ifStmt
	initStmt := ifStmt.Init.(*dst.AssignStmt)
	util.Assert(len(initStmt.Lhs) == 2, "must be")

	ifStmt.Cond = ast.BoolFalse()
	ifStmt.Body = ast.Block(ast.EmptyStmt())

	if removedAfter {
		// We removed the last reference to call context after nulling out body
		// block, at this point, all lhs are unused, replace assignment to simple
		// function call
		ifStmt.Init = ast.ExprStmt(initStmt.Rhs[0])
		// TODO: Remove After trampoline function
	} else {
		// Otherwise, mark skipCall identifier as unused
		skipCallIdent := initStmt.Lhs[1].(*dst.Ident)
		ast.MakeUnusedIdent(skipCallIdent)
	}
}

func stripTJumpLabel(tjump *TJump) {
	ifStmt := tjump.ifStmt
	ifStmt.Decs.If = ifStmt.Decs.If[1:]
}

func (ip *InstrumentPhase) optimizeTJumps() (err error) {
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
		removedAfter := false
		if rule.After == "" {
			err = removeAfterTrampolineCall(tjump)
			if err != nil {
				return err
			}
			removedAfter = true
		}

		// No Before hook present? Construct HookContext on the fly and pass it
		// to After trampoline defer call and rewrite the whole condition to
		// always false, then null out its initialization statement.
		if rule.Before == "" {
			err = removeBeforeTrampolineCall(ip.target, tjump)
			if err != nil {
				return err
			}
		}

		// No SkipCall used in Before hook? Rewrite cond of trampoline-jump-if
		// to always false, and remove return statement in then block, they are
		// memory aware and may generate memory SSA values during compilation.
		// This further simplifies the trampoline-jump-if and gives more chances
		// for optimization passes to kick in.
		if rule.Before != "" {
			beforeHook, err := getHookFunc(rule, true)
			if err != nil {
				return err
			}
			foundPoison := false
			const poison = "SkipCall"
			// FIXME: We should traverse the call graph to find all possible
			// usage of SkipCall, but for now, we just check the Before hook
			// function body.
			dst.Inspect(beforeHook, func(node dst.Node) bool {
				if ident, ok := node.(*dst.Ident); ok {
					if strings.Contains(ident.Name, poison) {
						foundPoison = true
						return false
					}
				}
				if foundPoison {
					return false
				}
				return true
			})
			if !foundPoison {
				flattenTJump(tjump, removedAfter)
			}
		}
	}
	return nil
}
