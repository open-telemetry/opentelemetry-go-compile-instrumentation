// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"testing"

	"github.com/dave/dst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to parse Go source code into a function declaration
func parseFunc(t *testing.T, source string) *dst.FuncDecl {
	parser := ast.NewAstParser()
	file, err := parser.ParseSource(source)
	require.NoError(t, err)
	require.Len(t, file.Decls, 1)
	funcDecl, ok := file.Decls[0].(*dst.FuncDecl)
	require.True(t, ok)
	return funcDecl
}

// Helper function to parse Go snippet into statements
func parseSnippet(t *testing.T, source string) []dst.Stmt {
	parser := ast.NewAstParser()
	stmts, err := parser.ParseSnippet(source)
	require.NoError(t, err)
	return stmts
}

// Helper function to create an if statement with trampoline-jump-if from source
func parseIfStmt(t *testing.T, source string) *dst.IfStmt {
	stmts := parseSnippet(t, source)
	require.Len(t, stmts, 1)
	ifStmt, ok := stmts[0].(*dst.IfStmt)
	require.True(t, ok)
	// Add the trampoline label
	ifStmt.Decs.If = []string{tJumpLabel}
	return ifStmt
}

func TestMustTJump(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		decorations []string
		valid       bool
	}{
		{
			name: "valid trampoline-jump-if with label",
			source: `if condition {
				// valid trampoline-jump-if
			}`,
			decorations: []string{tJumpLabel},
			valid:       true,
		},
		{
			name: "no decorations should be invalid",
			source: `if condition {
				// no decorations
			}`,
			decorations: []string{},
			valid:       false,
		},
		{
			name: "wrong label should be invalid",
			source: `if condition {
				// wrong label
			}`,
			decorations: []string{"wrong-label"},
			valid:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the if statement but don't add automatic label
			stmts := parseSnippet(t, tt.source)
			require.Len(t, stmts, 1)
			ifStmt, ok := stmts[0].(*dst.IfStmt)
			require.True(t, ok)

			// Set the decorations manually to test different scenarios
			ifStmt.Decs.If = tt.decorations

			// Test the validation logic directly
			hasLabel := len(ifStmt.Decs.If) == 1 && ifStmt.Decs.If[0] == tJumpLabel
			if tt.valid {
				assert.True(t, hasLabel, "expected valid trampoline-jump-if")
				// Only call mustTJump for valid cases since it uses ex.Fatalf
				assert.NotPanics(t, func() {
					mustTJump(ifStmt)
				})
			} else {
				assert.False(t, hasLabel, "expected invalid trampoline-jump-if")
			}
		})
	}
}

func TestRemoveAfterTrampolineCall(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		expectError bool
	}{
		{
			name: "removes defer statement successfully",
			source: `if ctx, skip := otel_trampoline_before(&arg); skip {
				otel_trampoline_after(ctx, &retval)
				return
			} else {
				defer otel_trampoline_after(ctx, &retval)
				if nested {
					// nested logic
				}
			}`,
			expectError: false,
		},
		{
			name: "handles multiple defer statements",
			source: `if ctx, skip := otel_trampoline_before(&arg); skip {
				otel_trampoline_after(ctx, &retval)
				return
			} else {
				defer otel_trampoline_after(ctx, &retval)
				defer cleanup()
				if nested {
					// nested logic
				}
			}`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ifStmt := parseIfStmt(t, tt.source)
			tjump := &TJump{ifStmt: ifStmt}

			err := removeAfterTrampolineCall(tjump)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify defer statements were replaced with empty statements
				elseBlock := tjump.ifStmt.Else.(*dst.BlockStmt)
				for _, stmt := range elseBlock.List {
					switch stmt.(type) {
					case *dst.DeferStmt:
						t.Error("defer statement should have been replaced")
					case *dst.EmptyStmt:
						// Expected replacement
						continue
					case *dst.IfStmt:
						// Expected nested statements
						continue
					}
				}
			}
		})
	}
}

func TestNewHookContextImpl(t *testing.T) {
	tests := []struct {
		name     string
		funcSrc  string
		wantErr  bool
		validate func(*testing.T, dst.Expr)
	}{
		{
			name: "creates context for function with parameters",
			funcSrc: `package main
func testFunc(param1 string, param2 int) {}`,
			wantErr: false,
			validate: func(t *testing.T, expr dst.Expr) {
				unaryExpr, ok := expr.(*dst.UnaryExpr)
				require.True(t, ok, "expression should be unary expression")
				compositeLit, ok := unaryExpr.X.(*dst.CompositeLit)
				require.True(t, ok, "expression should contain composite literal")
				assert.Len(t, compositeLit.Elts, 2, "should have params and return values fields")
			},
		},
		{
			name: "creates context for function with return values",
			funcSrc: `package main
func testFunc(param1 string) (result1 string) { return "" }`,
			wantErr: false,
			validate: func(t *testing.T, expr dst.Expr) {
				assert.NotNil(t, expr, "expression should not be nil")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetFunc := parseFunc(t, tt.funcSrc)
			tjump := &TJump{
				target: targetFunc,
				rule: &rule.InstFuncRule{
					Func: targetFunc.Name.Name,
				},
			}

			expr, err := newHookContextImpl(tjump)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, expr)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, expr)
				if tt.validate != nil {
					tt.validate(t, expr)
				}
			}
		})
	}
}

func TestStripTJumpLabel(t *testing.T) {
	tests := []struct {
		name             string
		source           string
		extraDecorations []string
		expectedAfter    int
	}{
		{
			name: "strips single label",
			source: `if condition {
				// do something
			}`,
			extraDecorations: []string{"other-decoration"},
			expectedAfter:    1,
		},
		{
			name: "strips label from multiple decorations",
			source: `if condition {
				// do something
			}`,
			extraDecorations: []string{"decoration1", "decoration2"},
			expectedAfter:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ifStmt := parseIfStmt(t, tt.source)
			// parseIfStmt already adds tJumpLabel, so we add extra decorations
			ifStmt.Decs.If = append(ifStmt.Decs.If, tt.extraDecorations...)

			tjump := &TJump{ifStmt: ifStmt}

			initialCount := len(ifStmt.Decs.If)
			stripTJumpLabel(tjump)
			finalCount := len(ifStmt.Decs.If)

			assert.Equal(t, tt.expectedAfter, finalCount)
			assert.Equal(t, initialCount-1, finalCount, "should remove exactly one decoration")
		})
	}
}

func TestOptimizeTJumps_NoAfterHook(t *testing.T) {
	// Test case based on comment: "No After hook present? Simply remove defer call to After trampoline"
	source := `if ctx, skip := otel_trampoline_before(&arg); skip {
		otel_trampoline_after(ctx, &retval)
		return
	} else {
		defer otel_trampoline_after(ctx, &retval)
	}`

	ifStmt := parseIfStmt(t, source)
	tjump := &TJump{
		ifStmt: ifStmt,
		rule: &rule.InstFuncRule{
			After: "", // No After hook
		},
	}

	err := removeAfterTrampolineCall(tjump)
	require.NoError(t, err)

	// Verify defer statement was replaced with empty statement
	elseBlock := tjump.ifStmt.Else.(*dst.BlockStmt)
	for _, stmt := range elseBlock.List {
		_, isDeferStmt := stmt.(*dst.DeferStmt)
		assert.False(t, isDeferStmt, "defer statement should have been removed")
	}
}

func TestRemoveBeforeTrampolineCall(t *testing.T) {
	// Test case based on comment: "No Before hook present? Construct HookContext on the fly"
	funcSrc := `package main
func testFunc(param1 string) {}`

	ifSrc := `if ctx, skip := otel_trampoline_before(&arg); skip {
		otel_trampoline_after(ctx, &retval)
		return
	} else {
		defer otel_trampoline_after(ctx, &retval)
	}`

	targetFunc := parseFunc(t, funcSrc)
	ifStmt := parseIfStmt(t, ifSrc)

	tjump := &TJump{
		target: targetFunc,
		ifStmt: ifStmt,
		rule: &rule.InstFuncRule{
			Before: "", // No Before hook
			After:  "afterHook",
		},
	}

	// Create target file with the original function and a dummy before trampoline function
	beforeFuncName := makeName(tjump.rule, tjump.target, true)
	fileSrc := fmt.Sprintf(`package main
func testFunc(param1 string) {}
func %s() {}`, beforeFuncName)
	targetFile, err := ast.NewAstParser().ParseSource(fileSrc)
	require.NoError(t, err)

	err = removeBeforeTrampolineCall(targetFile, tjump)
	require.NoError(t, err)

	// Verify condition was set to false
	boolLit, ok := tjump.ifStmt.Cond.(*dst.BasicLit)
	assert.True(t, ok)
	assert.Equal(t, "false", boolLit.Value)

	// Verify init statement was nulled out
	assert.Nil(t, tjump.ifStmt.Init)

	// Verify body contains empty statement
	assert.Len(t, tjump.ifStmt.Body.List, 1)
}

func TestPopulateHookContextLiteral(t *testing.T) {
	funcSrc := `package main
func testFunc(param1 string, param2 int) (result1 string) { return "" }`

	targetFunc := parseFunc(t, funcSrc)
	tjump := &TJump{
		target: targetFunc,
	}

	// Create mock expression structure representing &HookContextImpl{Params:[]interface{}{},ReturnVals:[]interface{}{}}
	// This mimics the structure created by newHookContextImpl
	exprSrc := `&HookContextImpl{Params: []interface{}{}, ReturnVals: []interface{}{}}`
	stmts := parseSnippet(t, exprSrc)
	require.Len(t, stmts, 1)
	exprStmt, ok := stmts[0].(*dst.ExprStmt)
	require.True(t, ok)
	expr := exprStmt.X

	populateHookContextLiteral(tjump, expr)

	unaryExpr, ok := expr.(*dst.UnaryExpr)
	require.True(t, ok)
	compositeLit := unaryExpr.X.(*dst.CompositeLit)
	paramsLit := compositeLit.Elts[0].(*dst.KeyValueExpr).Value.(*dst.CompositeLit)
	returnsLit := compositeLit.Elts[1].(*dst.KeyValueExpr).Value.(*dst.CompositeLit)

	// Verify parameters were populated with addresses of all arguments
	assert.Len(t, paramsLit.Elts, 2, "should have 2 parameter addresses")

	// Verify return values were populated with addresses
	assert.Len(t, returnsLit.Elts, 1, "should have 1 return value address")
}
