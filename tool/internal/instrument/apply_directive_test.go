// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"log/slog"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// The directive sits on a statement inside a function body, so it annotates no
// top-level func and the rule matches nothing.
const directiveInsideBodySource = `package main

func main() {
	//otelc:span
	value := 1
	println(value)
}
`

const directiveOnFuncSource = `package main

//otelc:span
func foo() {
	println("hello")
}
`

func TestApplyDirectiveRule_NoMatchingFuncs(t *testing.T) {
	root, err := ast.NewAstParser().ParseSource(directiveInsideBodySource)
	require.NoError(t, err)

	r := &rule.InstDirectiveRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "span_directive",
			Imports: map[string]string{"fmt": "fmt"},
		},
		Directive: "otelc:span",
		Template:  `fmt.Println("span start: {{.FuncName}}")`,
	}

	modified, err := newTestPhase().applyDirectiveRule(context.Background(), r, root)

	require.NoError(t, err)
	assert.False(t, modified, "a rule that instruments nothing must not request the globals file")
	assert.Zero(t, countImportSpecs(root), "imports must not be injected when no func is instrumented")
}

func TestApplyDirectiveRule_MatchingFunc(t *testing.T) {
	root, err := ast.NewAstParser().ParseSource(directiveOnFuncSource)
	require.NoError(t, err)

	r := &rule.InstDirectiveRule{
		InstBaseRule: rule.InstBaseRule{Name: "span_directive"},
		Directive:    "otelc:span",
		Template:     `println("span start: {{.FuncName}}")`,
	}

	modified, err := newTestPhase().applyDirectiveRule(context.Background(), r, root)

	require.NoError(t, err)
	assert.True(t, modified)
	funcDecl, ok := root.Decls[0].(*dst.FuncDecl)
	require.True(t, ok, "expected *dst.FuncDecl, got %T", root.Decls[0])
	assert.Len(t, funcDecl.Body.List, 2, "rendered statement should be prepended to the body")
}

func TestRenderDirective(t *testing.T) {
	tests := []struct {
		name          string
		src           string
		template      string
		directiveArgs []ast.DirectiveArg
		withImports   bool
		expected      string
	}{
		{
			name:     "FuncName no spaces",
			src:      "package main\nfunc Foo() {}",
			template: "call({{.FuncName}})",
			expected: "call(Foo)",
		},
		{
			name:     "FuncName with spaces",
			src:      "package main\nfunc Foo() {}",
			template: "call({{ .FuncName }})",
			expected: "call(Foo)",
		},
		{
			name:     "FuncArgument",
			src:      "package main\nfunc Foo(ctx int, name string) {}",
			template: "use({{ .FuncArgument 0 }}, {{ .FuncArgument 1 }})",
			expected: "use(ctx, name)",
		},
		{
			name:     "FuncReturn",
			src:      "package main\nfunc Foo() (int, error) { return 0, nil }",
			template: "check({{ .FuncReturn 0 }}, {{ .FuncReturn 1 }})",
			expected: "check(_unnamedRetVal_h1_0, _unnamedRetVal_h1_1)",
		},
		{
			name:     "counts",
			src:      "package main\nfunc Foo(a, b int) (int, error) { return 0, nil }",
			template: "n={{.FuncArgumentCount}} m={{.FuncReturnCount}}",
			expected: "n=2 m=2",
		},
		{
			name:     "template without any Func tag leaves function untouched",
			src:      "package main\nfunc Foo() {}",
			template: `println("static")`,
			expected: `println("static")`,
		},
		{
			name:     "trim markers",
			src:      "package main\nfunc Foo() {}",
			template: "call({{- .FuncName -}})",
			expected: "call(Foo)",
		},
		{
			name:        "FuncArgumentOfType matched",
			src:         "package main\nimport \"context\"\nfunc Foo(ctx context.Context, name string) {}",
			template:    "use({{ .FuncArgumentOfType \"context.Context\" }})",
			withImports: true,
			expected:    "use(ctx)",
		},
		{
			name:        "FuncArgumentOfType no match",
			src:         "package main\nfunc Foo(name string) {}",
			template:    "use({{ .FuncArgumentOfType \"io.Reader\" }})",
			withImports: true,
			expected:    "use()",
		},
		{
			name:        "FuncReturnOfType matched",
			src:         "package main\nfunc Foo() (int, error) { return 0, nil }",
			template:    "check({{ .FuncReturnOfType \"error\" }})",
			withImports: true,
			expected:    "check(_unnamedRetVal_h1_1)",
		},
		{
			name:        "FuncReturnOfType no match",
			src:         "package main\nfunc Foo() (int, error) { return 0, nil }",
			template:    "check({{ .FuncReturnOfType \"string\" }})",
			withImports: true,
			expected:    "check()",
		},
		{
			name:          "DirectiveArgs ranges over key:value pairs",
			src:           "package main\nfunc Foo() {}",
			template:      "{{ range .DirectiveArgs }}{{.Key}}={{.Value}} {{ end }}",
			directiveArgs: []ast.DirectiveArg{{Key: "span.name", Value: "my-op"}, {Key: "tag", Value: "v1"}},
			expected:      "span.name=my-op tag=v1 ",
		},
		{
			name:          "DirectiveArg looks up a single key",
			src:           "package main\nfunc Foo() {}",
			template:      "name={{ .DirectiveArg \"span.name\" }}",
			directiveArgs: []ast.DirectiveArg{{Key: "span.name", Value: "my-op"}},
			expected:      "name=my-op",
		},
		{
			name:          "DirectiveArg missing key returns empty",
			src:           "package main\nfunc Foo() {}",
			template:      "name={{ .DirectiveArg \"missing\" }}",
			directiveArgs: []ast.DirectiveArg{{Key: "span.name", Value: "my-op"}},
			expected:      "name=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				funcDecl *dst.FuncDecl
				imports  map[string]string
			)
			if tt.withImports {
				funcDecl, imports = parseFuncWithImports(t, tt.src)
			} else {
				funcDecl = parseFunc(t, tt.src)
			}
			tmpl, err := rule.ParseFuncTemplate(tt.template)
			require.NoError(t, err)

			result, err := renderDirective(tmpl, newFuncTemplateData(funcDecl, tt.directiveArgs, imports, "h1"))

			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseFuncTemplate_UnknownTagFails(t *testing.T) {
	_, err := rule.ParseFuncTemplate("{{Bogus}}")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not defined")
}

func TestParseFuncTemplate_CompositeLiteralFails(t *testing.T) {
	// text/template treats every "{{ ... }}" as an action, so incidental
	// adjacent Go braces (e.g. a composite literal like []Point{{X: 1, Y: 2}})
	// fail to parse. Datadog/orchestrion's code.Template has the same
	// limitation for the same reason (plain text/template.Parse with no
	// escaping).
	_, err := rule.ParseFuncTemplate(`attrs := []Point{{X: 1, Y: 2}}; call({{.FuncName}})`)

	require.Error(t, err)
}

func TestRenderDirective_OutOfRangeArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	tmpl, err := rule.ParseFuncTemplate("{{.FuncArgument 0}}")
	require.NoError(t, err)

	_, err = renderDirective(tmpl, newFuncTemplateData(funcDecl, nil, nil, "h1"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestApplyDirectiveRule(t *testing.T) {
	t.Run("successfully applies directive to function with body", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{Name: "test_directive"},
			Directive:    "otelc:test",
			Template:     "println(\"{{.FuncName}}\")",
		}
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{
				Params: &dst.FieldList{},
			},
			Body: &dst.BlockStmt{
				List: []dst.Stmt{
					&dst.ExprStmt{
						X: &dst.CallExpr{
							Fun: dst.NewIdent("println"),
							Args: []dst.Expr{
								&dst.BasicLit{Kind: token.STRING, Value: `"body"`},
							},
						},
					},
				},
			},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					Start: dst.Decorations{"//otelc:test\n"},
				},
			},
		}
		root := &dst.File{
			Decls: []dst.Decl{funcDecl},
		}

		ip := &instrumentPhase{logger: slog.Default()}
		modified, err := ip.applyDirectiveRule(context.Background(), r, root)
		require.NoError(t, err)
		assert.True(t, modified)
		assert.Len(t, funcDecl.Body.List, 2)
	})

	t.Run("returns error on invalid template syntax", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{Name: "test_directive"},
			Directive:    "otelc:test",
			Template:     "println(\"{{FuncName\")",
		}
		// A matching func must be present: applyDirectiveRule now checks for
		// matches before validating the template (see #1045), so an empty file
		// would short-circuit to (false, nil) before ever reaching the invalid
		// template and this test would no longer exercise the path it's testing.
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{Params: &dst.FieldList{}},
			Body: &dst.BlockStmt{List: []dst.Stmt{}},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					Start: dst.Decorations{"//otelc:test\n"},
				},
			},
		}
		root := &dst.File{Decls: []dst.Decl{funcDecl}}

		ip := &instrumentPhase{logger: slog.Default()}
		modified, err := ip.applyDirectiveRule(context.Background(), r, root)
		require.Error(t, err)
		assert.False(t, modified)
	})

	t.Run("returns error on unknown template tag", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{Name: "test_directive"},
			Directive:    "otelc:test",
			Template:     "println(\"{{.UnknownTag}}\")",
		}
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{Params: &dst.FieldList{}},
			Body: &dst.BlockStmt{List: []dst.Stmt{}},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					Start: dst.Decorations{"//otelc:test\n"},
				},
			},
		}
		root := &dst.File{Decls: []dst.Decl{funcDecl}}

		ip := &instrumentPhase{logger: slog.Default()}
		modified, err := ip.applyDirectiveRule(context.Background(), r, root)
		require.Error(t, err)
		assert.False(t, modified)
		assert.Contains(t, err.Error(), "can't evaluate field UnknownTag")
	})

	t.Run("returns error on invalid Go syntax in rendered snippet", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{Name: "test_directive"},
			Directive:    "otelc:test",
			Template:     "println( { {{.FuncName}}",
		}
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{Params: &dst.FieldList{}},
			Body: &dst.BlockStmt{List: []dst.Stmt{}},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					Start: dst.Decorations{"//otelc:test\n"},
				},
			},
		}
		root := &dst.File{Decls: []dst.Decl{funcDecl}}

		ip := &instrumentPhase{logger: slog.Default()}
		modified, err := ip.applyDirectiveRule(context.Background(), r, root)
		require.Error(t, err)
		assert.False(t, modified)
		assert.Contains(t, err.Error(), "parsing rendered template")
	})

	t.Run("returns error when directive args fail to parse", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{Name: "test_directive"},
			Directive:    "otelc:test",
			Template:     "println(\"{{.FuncName}}\")",
		}
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{Params: &dst.FieldList{}},
			Body: &dst.BlockStmt{List: []dst.Stmt{}},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					// Unclosed quote makes tokenize (via FindFuncsByDirective) fail
					// before any imports or template work happens.
					Start: dst.Decorations{"//otelc:test key:\"unterminated\n"},
				},
			},
		}
		root := &dst.File{Decls: []dst.Decl{funcDecl}}

		ip := &instrumentPhase{logger: slog.Default()}
		modified, err := ip.applyDirectiveRule(context.Background(), r, root)
		require.Error(t, err)
		assert.False(t, modified)
		assert.Contains(t, err.Error(), "parsing directive args")
	})

	t.Run("returns error when rule imports conflict with an existing alias", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{
				Name:    "test_directive",
				Imports: map[string]string{"context": "context"},
			},
			Directive: "otelc:test",
			Template:  "println(\"{{.FuncName}}\")",
		}
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{Params: &dst.FieldList{}},
			Body: &dst.BlockStmt{List: []dst.Stmt{}},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					Start: dst.Decorations{"//otelc:test\n"},
				},
			},
		}
		root := &dst.File{
			Decls: []dst.Decl{
				&dst.GenDecl{
					Tok: token.IMPORT,
					Specs: []dst.Spec{
						&dst.ImportSpec{
							Name: dst.NewIdent("ctx"),
							Path: &dst.BasicLit{Value: `"context"`},
						},
					},
				},
				funcDecl,
			},
		}

		ip := &instrumentPhase{logger: slog.Default()}
		modified, err := ip.applyDirectiveRule(context.Background(), r, root)
		require.Error(t, err)
		assert.False(t, modified)
		assert.Contains(t, err.Error(), "import alias mismatch")
	})

	t.Run("template parse failure short-circuits before imports are added", func(t *testing.T) {
		r := &rule.InstDirectiveRule{
			InstBaseRule: rule.InstBaseRule{
				Name:    "test_directive",
				Imports: map[string]string{"context": "context"},
			},
			Directive: "otelc:test",
			Template:  "println(\"{{FuncName\")",
		}
		funcDecl := &dst.FuncDecl{
			Name: dst.NewIdent("myFunc"),
			Type: &dst.FuncType{Params: &dst.FieldList{}},
			Body: &dst.BlockStmt{List: []dst.Stmt{}},
			Decs: dst.FuncDeclDecorations{
				NodeDecs: dst.NodeDecs{
					Start: dst.Decorations{"//otelc:test\n"},
				},
			},
		}
		root := &dst.File{Decls: []dst.Decl{funcDecl}}

		ip := &instrumentPhase{logger: slog.Default()}
		modified, err := ip.applyDirectiveRule(context.Background(), r, root)
		require.Error(t, err)
		assert.False(t, modified)
		// The template is parsed before imports are added, so a rule with a
		// broken template must fail without ever touching the file's imports.
		assert.Zero(t, countImportSpecs(root))
	})
}
