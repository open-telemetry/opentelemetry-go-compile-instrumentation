// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	goast "go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestApplyFuncRuleSignatureFilterMismatchIsLookupMiss(t *testing.T) {
	parser := ast.NewAstParser()
	root, err := parser.ParseSource(`package main

func Target(value string) error { return nil }
`)
	require.NoError(t, err)

	sig := rule.FuncSignature{Args: []string{"int"}, Returns: []string{"error"}}
	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "mismatch"},
		Func:         "Target",
		Before:       "BeforeTarget",
		Signature:    &sig,
	}

	err = newTestPhase().applyFuncRule(context.Background(), funcRule, root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can not find function Target")
}

// testHash stands in for InstFuncRule.Identity() in these unit tests, since
// they exercise collectArguments/collectReturnValues without a real rule.
const testHash = "42"

func syntheticParam(idx int) string {
	return fmt.Sprintf("%s_%s_%d", ignoredParam, testHash, idx)
}

func syntheticUnnamedRetVal(idx int) string {
	return fmt.Sprintf("%s_%s_%d", unnamedRetValName, testHash, idx)
}

func syntheticIgnoredRetVal(idx int) string {
	return fmt.Sprintf("%s_%s_%d", ignoredRetValName, testHash, idx)
}

func TestCollectArguments(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected []string
	}{
		{
			name:     "no params no receiver",
			src:      "package main\nfunc F() {}",
			expected: []string{},
		},
		{
			name:     "named params",
			src:      "package main\nfunc F(a int, b string) {}",
			expected: []string{"a", "b"},
		},
		{
			name:     "unnamed params (len(Names) == 0)",
			src:      "package main\nfunc F(int, string) {}",
			expected: []string{syntheticParam(0), syntheticParam(1)},
		},
		{
			name:     "mixed named and unnamed params via group",
			src:      "package main\nfunc F(a, b int) {}",
			expected: []string{"a", "b"},
		},
		{
			name:     "underscore params",
			src:      "package main\nfunc F(_ int, _ string) {}",
			expected: []string{syntheticParam(0), syntheticParam(1)},
		},
		{
			name:     "named receiver",
			src:      "package main\ntype T struct{}\nfunc (t T) F() {}",
			expected: []string{"t"},
		},
		{
			name:     "unnamed receiver",
			src:      "package main\ntype T struct{}\nfunc (T) F() {}",
			expected: []string{syntheticParam(0)},
		},
		{
			name:     "underscore receiver",
			src:      "package main\ntype T struct{}\nfunc (_ T) F() {}",
			expected: []string{syntheticParam(0)},
		},
		{
			name:     "named receiver with params",
			src:      "package main\ntype T struct{}\nfunc (t T) F(a int, b string) {}",
			expected: []string{"t", "a", "b"},
		},
		{
			name:     "unnamed receiver with unnamed params",
			src:      "package main\ntype T struct{}\nfunc (T) F(int, string) {}",
			expected: []string{syntheticParam(0), syntheticParam(1), syntheticParam(2)},
		},
		{
			name:     "underscore param collides with existing synthetic-looking param name",
			src:      fmt.Sprintf("package main\nfunc F(%s int, _ string) {}", syntheticParam(0)),
			expected: []string{syntheticParam(0), syntheticParam(1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcDecl := parseFunc(t, tt.src)
			args := collectArguments(funcDecl, testHash)
			assert.Equal(t, tt.expected, args)
		})
	}
}

func TestCollectReturnValues(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected []string
	}{
		{
			name:     "no return values",
			src:      "package main\nfunc F() {}",
			expected: nil,
		},
		{
			name:     "named return values",
			src:      "package main\nfunc F() (a int, b string) { return }",
			expected: []string{"a", "b"},
		},
		{
			name:     "unnamed return values",
			src:      "package main\nfunc F() (int, string) { return 0, \"\" }",
			expected: []string{syntheticUnnamedRetVal(0), syntheticUnnamedRetVal(1)},
		},
		{
			name:     "underscore return values",
			src:      "package main\nfunc F() (_ int, _ string) { return }",
			expected: []string{syntheticIgnoredRetVal(0), syntheticIgnoredRetVal(1)},
		},
		{
			name: "underscore return collides with existing synthetic-looking return name",
			src: fmt.Sprintf(
				"package main\nfunc F() (%s error, _ bool) { return nil, false }",
				syntheticIgnoredRetVal(0),
			),
			expected: []string{syntheticIgnoredRetVal(0), syntheticIgnoredRetVal(1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcDecl := parseFunc(t, tt.src)
			retVals := collectReturnValues(funcDecl, testHash)
			assert.Equal(t, tt.expected, retVals)
		})
	}
}

// Regression for #736: a blank param/receiver and a blank named return share
// one scope, so the two collectors must not rename them to the same identifier.
func TestCollectNamesNoCollision(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "blank param and blank named return",
			src:  "package main\nfunc F(_ int) (_ error) { return nil }",
		},
		{
			name: "unnamed param and blank named return",
			src:  "package main\nfunc F(int) (_ error) { return nil }",
		},
		{
			name: "unnamed receiver and blank named return",
			src:  "package main\ntype T struct{}\nfunc (T) M() (_ error) { return nil }",
		},
		{
			name: "blank receiver and blank named return",
			src:  "package main\ntype T struct{}\nfunc (_ T) M() (_ error) { return nil }",
		},
		{
			name: "unnamed param and unnamed return (control)",
			src:  "package main\nfunc F(int) (int) { return 0 }",
		},
		{
			name: "multiple blanks on both sides",
			src:  "package main\nfunc F(_ int, _ string) (_ error, _ bool) { return nil, false }",
		},
		{
			name: "blank param collides with existing synthetic-looking param name",
			src:  fmt.Sprintf("package main\nfunc F(%s int, _ string) {}", syntheticParam(0)),
		},
		{
			name: "blank return collides with existing synthetic-looking return name",
			src: fmt.Sprintf(
				"package main\nfunc F() (%s error, _ bool) { return nil, false }",
				syntheticIgnoredRetVal(0),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, funcDecl := parseFileFunc(t, tt.src)

			// Mirror insertTJump: returns are collected first, then arguments.
			retVals := collectReturnValues(funcDecl, testHash)
			args := collectArguments(funcDecl, testHash)

			seen := make(map[string]struct{})
			for _, name := range append(append([]string{}, retVals...), args...) {
				require.NotEqual(t, ast.IdentIgnore, name, "blank binding was left unnamed")
				_, dup := seen[name]
				require.Falsef(t, dup, "binding %q generated in two positions", name)
				seen[name] = struct{}{}
			}

			requireTypeChecks(t, renderFile(t, file))
		})
	}
}

// Regression for #1014: syntheticNamer previously only checked names against
// the function's own signature, so a blank param/return could still be
// renamed to something that shadows a package-level identifier written in
// the bare "prefixN" style the old scheme produced. Salting every generated
// name with the rule's identity hash means it can never collide with that
// bare form, so the global is never shadowed - without having to resolve
// every identifier visible in the function's scope.
func TestSyntheticNamesDoNotShadowBareStyleGlobal(t *testing.T) {
	src := "package main\n" +
		"var _ignoredParam0 = \"important\"\n" +
		"func foo(_ int, _ignoredParam1 string) string { return _ignoredParam0 }"
	funcDecl := parseFunc(t, src)

	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "foo-rule"},
		Func:         "foo",
		Before:       "BeforeFoo",
		Path:         "example.com/hook",
	}

	args := collectArguments(funcDecl, funcRule.Identity())
	assert.NotContains(t, args, "_ignoredParam0", "synthetic name must not shadow the package-level global")
	assert.Equal(t, "_ignoredParam1", args[1], "named param must be left untouched")
}

// parseFileFunc parses source into a file and returns it alongside the first
// function declaration it contains.
func parseFileFunc(t *testing.T, source string) (*dst.File, *dst.FuncDecl) {
	t.Helper()
	file := parseFile(t, source)
	for _, decl := range file.Decls {
		if funcDecl, ok := decl.(*dst.FuncDecl); ok {
			return file, funcDecl
		}
	}
	require.Fail(t, "no function declaration found in source")
	return nil, nil
}

// renderFile restores a dst.File back to Go source code.
func renderFile(t *testing.T, file *dst.File) string {
	t.Helper()
	var buf strings.Builder
	require.NoError(t, decorator.Fprint(&buf, file))
	return buf.String()
}

// requireTypeChecks fails the test unless src is valid, type-correct Go.
func requireTypeChecks(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "src.go", src, parser.AllErrors)
	require.NoErrorf(t, err, "generated code does not parse:\n%s", src)
	conf := types.Config{Importer: importer.Default()}
	_, err = conf.Check("main", fset, []*goast.File{parsed}, nil)
	require.NoErrorf(t, err, "generated code does not type-check:\n%s", src)
}

// newTrampolineJump builds an if statement shaped like the one buildTJump
// produces: a labelled if whose else block holds the deferred after-call. The
// label is what findJumpPoint keys on, so a jump without it is just an
// ordinary if as far as this function is concerned.
func newTrampolineJump(labelled bool) *dst.IfStmt {
	// Mirrors buildTJump: the init defines the hook context and skip flag from
	// the before-call, and the else block defers the after-call.
	init := ast.DefineStmts(
		ast.Exprs(ast.Ident("hookCtx"), ast.Ident("skip")),
		ast.Exprs(ast.CallTo("beforeCall", nil, nil)),
	)
	elseBlock := ast.Block(ast.DeferStmt(ast.CallTo("afterCall", nil, nil)))

	jump := ast.IfStmt(init, ast.Ident("skip"), ast.Block(ast.EmptyStmt()), elseBlock)
	if labelled {
		jump.Decs.If.Append(tJumpLabel)
	}
	return jump
}

// chainJump nests inner inside outer's else block the way insertToFunc does,
// separating the existing statement from the new jump with an empty statement.
// That leaves the else block with more than one statement, which is the
// condition findJumpPoint recurses on.
func chainJump(t *testing.T, outer, inner *dst.IfStmt) {
	t.Helper()

	elseBlock, ok := outer.Else.(*dst.BlockStmt)
	require.True(t, ok, "a trampoline jump should always carry a block else")
	elseBlock.List = append(elseBlock.List, ast.EmptyStmt(), inner)
}

func TestFindJumpPointIgnoresUnlabelledIf(t *testing.T) {
	// Without the trampoline label this is an ordinary if statement that
	// happened to be first in the function body. Returning a block here would
	// splice a trampoline into unrelated user code.
	jump := newTrampolineJump(false)

	assert.Nil(t, findJumpPoint(jump))
}

func TestFindJumpPointReturnsElseBlockOfLoneJump(t *testing.T) {
	jump := newTrampolineJump(true)

	point := findJumpPoint(jump)

	require.NotNil(t, point)
	assert.Equal(t, jump.Else, point, "the only jump present is the insertion point")
}

func TestFindJumpPointDescendsToLastJumpInChain(t *testing.T) {
	// Two rules already applied to the same function, so the second jump lives
	// inside the first one's else block. A third has to land inside the second,
	// not the first, or the rules stop nesting in the order they were applied.
	first := newTrampolineJump(true)
	second := newTrampolineJump(true)
	chainJump(t, first, second)

	point := findJumpPoint(first)

	require.NotNil(t, point)
	assert.Equal(t, second.Else, point, "insertion belongs in the innermost jump")
	assert.NotEqual(t, first.Else, point)
}

func TestFindJumpPointDescendsThroughSeveralJumps(t *testing.T) {
	// The descent is recursive, so a longer chain must still reach the end
	// rather than stopping one level in.
	first := newTrampolineJump(true)
	second := newTrampolineJump(true)
	third := newTrampolineJump(true)
	chainJump(t, first, second)
	chainJump(t, second, third)

	point := findJumpPoint(first)

	require.NotNil(t, point)
	assert.Equal(t, third.Else, point, "insertion belongs at the end of the chain")
}

func TestFindJumpPointStopsAtUnlabelledTail(t *testing.T) {
	// A chain whose last statement is not a labelled jump has no further
	// insertion point beneath it, so the descent reports nothing rather than
	// guessing at a block.
	first := newTrampolineJump(true)
	tail := newTrampolineJump(false)
	chainJump(t, first, tail)

	assert.Nil(t, findJumpPoint(first))
}

// context.go is the source of truth that api.tmpl mirrors; it lives in the
// sibling pkg/ module, so the path is relative to this package dir.
const hookContextSource = "../../../pkg/hook/context.go"

// TestAPITemplateMatchesHookContext keeps api.tmpl byte-identical to
// pkg/hook/context.go (issue #899). api.tmpl is embedded and parsed for its
// HookContext decls, so any drift silently desyncs the generated interface.
// We compare the embedded templateAPI, i.e. the exact bytes the tool builds in.
func TestAPITemplateMatchesHookContext(t *testing.T) {
	source, err := os.ReadFile(hookContextSource)
	require.NoError(t, err)

	require.Equal(t, string(source), templateAPI,
		"%s and api.tmpl have drifted; re-sync with `make build` "+
			"(or `cp %s tool/internal/instrument/api.tmpl`)",
		hookContextSource, hookContextSource)
}

func TestParseFileMissingFile(t *testing.T) {
	ip := &instrumentPhase{}
	_, err := ip.parseFile("/nonexistent/path/source.go")
	require.Error(t, err)
}

func TestInstrumentParseFileError(t *testing.T) {
	ip := &instrumentPhase{}

	rset := rule.NewInstRuleSet("example.com/pkg")
	rset.FuncRules["/nonexistent/does-not-exist.go"] = []*rule.InstFuncRule{
		{
			InstBaseRule: rule.InstBaseRule{Name: "test-rule"},
			Func:         "Foo",
			Before:       "BeforeFoo",
			Path:         "example.com/hook",
		},
	}

	err := ip.instrument(context.Background(), rset)
	require.Error(t, err)
}

func TestInstrument_WriteInstrumentedError(t *testing.T) {
	// A valid source file whose path does not match compileArgs causes writeInstrumented to fail.
	tmp := t.TempDir()
	src := filepath.Join(tmp, "source.go")
	require.NoError(t, os.WriteFile(src, []byte("package main\nvar X = 1\n"), 0o644))

	ip := &instrumentPhase{
		logger:      slog.New(slog.DiscardHandler),
		workDir:     tmp,
		compileArgs: []string{"other.go"}, // doesn't match src, causing writeInstrumented to error
	}

	rset := rule.NewInstRuleSet("main")
	rset.DeclRules[src] = []*rule.InstDeclRule{
		{
			InstBaseRule: rule.InstBaseRule{Name: "test-rule"},
			Identifier:   "X",
			Replace:      "2",
		},
	}

	err := ip.instrument(context.Background(), rset)
	require.Error(t, err)
}
