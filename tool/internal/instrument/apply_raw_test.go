// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// renameReturnValues deliberately does not salt with a rule hash: raw and
// directive rules reference the resulting name literally, in their injected
// code, by its bare positional form. For example,
// instrumentation/runtime/otelc.yaml's goroutine_propagate rule references
// runtime.newproc1's first unnamed return value as "_unnamedRetVal0". Salting
// this name broke every build that instruments runtime.newproc1.
func TestRenameReturnValuesUsesStableBareNames(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc F() (int, string) { return 0, \"\" }")

	renameReturnValues(funcDecl)

	names := []string{funcDecl.Type.Results.List[0].Names[0].Name, funcDecl.Type.Results.List[1].Names[0].Name}
	assert.Equal(t, []string{unnamedRetValName + "0", unnamedRetValName + "1"}, names)
}

func TestInsertRaw_SharedSyntheticName(t *testing.T) {
	ctx := util.ContextWithLogger(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	ruleA, err := rule.NewInstRawRule([]byte(`
target: main
func: Foo
raw: "use({{ .FuncArgument 0 }})"
`), "ruleA")
	require.NoError(t, err)
	ruleB, err := rule.NewInstRawRule([]byte(`
target: main
func: Foo
raw: "log({{ .FuncArgument 0 }})"
`), "ruleB")
	require.NoError(t, err)
	require.NotEqual(t, ruleA.Identity(), ruleB.Identity(),
		"test setup: the two rules must actually differ for this check to mean anything")

	// Mirrors InstrumentPhase.instrument: multiple rules for one file are
	// applied to the same parsed root/decl in sequence, not to independent
	// copies (see groupRules/instrument in instrument.go).
	funcDecl := parseFunc(t, "package main\nfunc Foo(int) {}")
	require.NoError(t, insertRaw(ctx, ruleA, funcDecl, nil))
	require.NoError(t, insertRaw(ctx, ruleB, funcDecl, nil))

	require.Len(t, funcDecl.Body.List, 2)
	argOf := func(stmt dst.Stmt) (string, string) {
		call := stmt.(*dst.ExprStmt).X.(*dst.CallExpr)
		return call.Fun.(*dst.Ident).Name, call.Args[0].(*dst.Ident).Name
	}
	var argForUse, argForLog string
	for _, stmt := range funcDecl.Body.List {
		fn, arg := argOf(stmt)
		switch fn {
		case "use":
			argForUse = arg
		case "log":
			argForLog = arg
		default:
			t.Fatalf("unexpected call %q in generated body", fn)
		}
	}

	assert.Equal(t, argForUse, argForLog,
		"both rules must resolve the unnamed parameter to the same identifier")
	assert.Equal(t, fmt.Sprintf("_ignoredParam_%s_0", ruleA.Identity()), argForUse,
		"the parameter must be salted with the first rule's Identity, since ruleA runs first")
}

func TestInsertRawAtPattern(t *testing.T) {
	ctx := util.ContextWithLogger(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	tests := []struct {
		name           string
		src            string
		pattern        string
		placement      string
		expectInserted bool
		expected       string
	}{
		{
			name: "basic insert",
			src: `package main

func a() {
	println("x")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	print("Hello, ")
	print("World!")
	println("x")
}
`,
		},
		{
			name: "only first match",
			src: `package main

func a() {
	println("x")
	println("x")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	print("Hello, ")
	print("World!")
	println("x")
	println("x")
}
`,
		},
		{
			name: "nested block",
			src: `package main

func a() {
	if true {
		println("x")
	}
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	if true {
		print("Hello, ")
		print("World!")
		println("x")
	}
}
`,
		},
		{
			name: "first match in nested block",
			src: `package main

func a() {
	if true {
		println("x")
	}
	println("x")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	if true {
		print("Hello, ")
		print("World!")
		println("x")
	}
	println("x")
}
`,
		},
		{
			name: "match block statement header",
			src: `package main

func a() {
	go func() {
		println("x")
	}()
}
`,
			pattern:        `^go func\(\) \{`,
			expectInserted: true,
			expected: `package main

func a() {
	print("Hello, ")
	print("World!")
	go func() {
		println("x")
	}()
}
`,
		},
		{
			name: "multiple statements in a single line",
			src: `package main

func a() {
	println("y"); println("x")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	println("y")
	print("Hello, ")
	print("World!")
	println("x")
}
`,
		},
		{
			name: "place after the matched statement",
			src: `package main

func a() {
	println("y")
	println("x")
}
`,
			pattern:        `^println\("y"\)$`,
			placement:      "after",
			expectInserted: true,
			expected: `package main

func a() {
	println("y")
	print("Hello, ")
	print("World!")
	println("x")
}
`,
		},
		{
			name: "empty block",
			src: `package main

func a() {}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: false,
			expected: `package main

func a() {}
`,
		},
		{
			name: "no matches",
			src: `package main

func a() {
	println("y")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: false,
			expected: `package main

func a() {
	println("y")
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, parseErr := parser.ParseFile(fset, "", tt.src, parser.ParseComments)
			require.NoError(t, parseErr)

			dec := decorator.NewDecorator(fset)
			dstFile, decorateErr := dec.DecorateFile(f)
			require.NoError(t, decorateErr)

			restorer := decorator.NewRestorer()
			_, restoreErr := restorer.RestoreFile(dstFile)
			require.NoError(t, restoreErr)

			var fn *dst.FuncDecl
			for _, decl := range dstFile.Decls {
				if f, ok := decl.(*dst.FuncDecl); ok && f.Name.Name == "a" {
					fn = f
					break
				}
			}
			require.NotNil(t, fn, "function a not found")

			stmts := []dst.Stmt{
				&dst.ExprStmt{
					X: &dst.CallExpr{
						Fun:  dst.NewIdent("print"),
						Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: `"Hello, "`}},
					},
				},
				&dst.ExprStmt{
					X: &dst.CallExpr{
						Fun:  dst.NewIdent("print"),
						Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: `"World!"`}},
					},
				},
			}

			pos := insertPos{
				pattern:   regexp.MustCompile(tt.pattern),
				placement: tt.placement,
			}
			inserted := insertRawAtPattern(ctx, fn, restorer, pos, stmts)
			require.Equal(t, tt.expectInserted, inserted)

			var modifiedSrc strings.Builder
			decorator.Fprint(&modifiedSrc, dstFile)

			require.Equal(t, tt.expected, modifiedSrc.String())
		})
	}
}

func TestInsertRawInvalidRegexPattern(t *testing.T) {
	ctx := util.ContextWithLogger(context.Background(), slog.New(slog.DiscardHandler))

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", "package main\nfunc main() {}", parser.ParseComments)
	require.NoError(t, err)

	dec := decorator.NewDecorator(fset)
	dstFile, err := dec.DecorateFile(f)
	require.NoError(t, err)

	fn := dstFile.Decls[0].(*dst.FuncDecl)

	rawRule := &rule.InstRawRule{
		InstBaseRule: rule.InstBaseRule{Name: "invalid-regex"},
		Func:         "main",
		Raw:          `println("test")`,
		Pattern:      `[unclosed-bracket`,
	}

	err = insertRaw(ctx, rawRule, fn, dstFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid raw rule pattern")
}

func TestRenderRawCode(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		raw      string
		expected string
	}{
		{
			name:     "no braces left untouched",
			src:      "package main\nfunc Foo(a int) {}",
			raw:      `println("static")`,
			expected: `println("static")`,
		},
		{
			name:     "FuncName",
			src:      "package main\nfunc Foo() {}",
			raw:      "call({{.FuncName}})",
			expected: "call(Foo)",
		},
		{
			name:     "FuncArgument",
			src:      "package main\nfunc Foo(ctx int, name string) {}",
			raw:      "use({{ .FuncArgument 0 }}, {{ .FuncArgument 1 }})",
			expected: "use(ctx, name)",
		},
		{
			name:     "FuncReturn",
			src:      "package main\nfunc Foo() (int, error) { return 0, nil }",
			raw:      "check({{ .FuncReturn 0 }}, {{ .FuncReturn 1 }})",
			expected: "check(_unnamedRetVal_h1_0, _unnamedRetVal_h1_1)",
		},
		{
			name:     "counts",
			src:      "package main\nfunc Foo(a, b int) (int, error) { return 0, nil }",
			raw:      "n={{.FuncArgumentCount}} m={{.FuncReturnCount}}",
			expected: "n=2 m=2",
		},
		{
			name:     "trim markers",
			src:      "package main\nfunc Foo() {}",
			raw:      "call({{- .FuncName -}})",
			expected: "call(Foo)",
		},
		{
			name:     "receiver excluded from FuncArgument",
			src:      "package main\ntype T struct{}\nfunc (t T) Foo(a int) {}",
			raw:      "use({{ .FuncArgument 0 }})",
			expected: "use(a)",
		},
		{
			name:     "Receiver",
			src:      "package main\ntype T struct{}\nfunc (t T) Foo(a int) {}",
			raw:      "use({{ .Receiver }}, {{ .FuncArgument 0 }})",
			expected: "use(t, a)",
		},
		{
			name:     "blank receiver gets a synthetic name",
			src:      "package main\ntype T struct{}\nfunc (_ T) Foo(a int) {}",
			raw:      "use({{ .Receiver }})",
			expected: "use(_ignoredParam_h1_0)",
		},
		{
			name:     "unnamed receiver gets a synthetic name",
			src:      "package main\ntype T struct{}\nfunc (T) Foo(a int) {}",
			raw:      "use({{ .Receiver }})",
			expected: "use(_ignoredParam_h1_0)",
		},
		{
			name:     "unnamed parameter gets a synthetic name",
			raw:      "use({{ .FuncArgument 0 }})",
			src:      "package main\nfunc Foo(int) {}",
			expected: "use(_ignoredParam_h1_0)",
		},
		{
			name:     "blank parameter gets a synthetic name",
			src:      "package main\nfunc Foo(_ int) {}",
			raw:      "use({{ .FuncArgument 0 }})",
			expected: "use(_ignoredParam_h1_0)",
		},
		{
			name:     "blank named return gets a synthetic name",
			src:      "package main\nfunc Foo() (_ int) { return 0 }",
			raw:      "check({{ .FuncReturn 0 }})",
			expected: "check(_ignoredRetVal_h1_0)",
		},
		{
			name:     "named return values are collected as-is",
			src:      "package main\nfunc Foo() (a int, b error) { return 0, nil }",
			raw:      "check({{ .FuncReturn 0 }}, {{ .FuncReturn 1 }})",
			expected: "check(a, b)",
		},
		{
			name:     "control-flow actions are available",
			src:      "package main\nfunc Foo(a int) {}",
			raw:      "{{ if gt .FuncArgumentCount 0 }}has args{{ else }}no args{{ end }}",
			expected: "has args",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			funcDecl := parseFunc(t, tt.src)

			result, err := renderRawCode(tt.raw, funcDecl, "h1")

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderRawCode_HashSaltsSyntheticNames(t *testing.T) {
	// Each call parses its own funcDecl
	src := "package main\nfunc Foo(int) {}"
	raw := "use({{ .FuncArgument 0 }})"

	result1, err := renderRawCode(raw, parseFunc(t, src), "h1")
	require.NoError(t, err)

	result2, err := renderRawCode(raw, parseFunc(t, src), "h2")
	require.NoError(t, err)

	assert.NotEqual(t, result1, result2, "different hashes must salt the synthetic name differently")
	assert.Equal(t, "use(_ignoredParam_h1_0)", result1)
	assert.Equal(t, "use(_ignoredParam_h2_0)", result2)
}

func TestRenderRawCode_UnknownTagFails(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{Foo}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not defined")
}

func TestRenderRawCode_CompositeLiteralFails(t *testing.T) {
	// text/template treats every "{{ ... }}" as an action, so incidental
	// adjacent Go braces (e.g. a composite literal like []Point{{X: 1, Y: 2}})
	// fail to parse. Datadog/orchestrion's code.Template has the same
	// limitation for the same reason (plain text/template.Parse with no
	// escaping).
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode(`attrs := []Point{{X: 1, Y: 2}}; call({{.FuncName}})`, funcDecl, "h1")

	require.Error(t, err)
}

func TestRenderRawCode_OutOfRangeArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{.FuncArgument 0}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestRenderRawCode_NegativeArgumentIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")

	_, err := renderRawCode("{{.FuncArgument -1}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestRenderRawCode_OutOfRangeReturn(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{.FuncReturn 0}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestRenderRawCode_NegativeReturnIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() (int, error) { return 0, nil }")

	_, err := renderRawCode("{{.FuncReturn -1}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestRenderRawCode_ReceiverOnFunctionWithoutReceiver(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{.Receiver}}", funcDecl, "h1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no receiver")
}

func TestRenderRawCode_InvalidTemplateSyntax(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")

	_, err := renderRawCode("{{.FuncName", funcDecl, "h1")

	require.Error(t, err)
}

func TestRenderRawCode_NonIntegerArgumentIndex(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")

	_, err := renderRawCode("{{.FuncArgument abc}}", funcDecl, "h1")

	require.Error(t, err)
}
