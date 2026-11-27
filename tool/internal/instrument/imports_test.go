// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestAddRuleImports(t *testing.T) {
	tests := []struct {
		name        string
		root        *dst.File
		imports     map[string]string
		expectError bool
		errorMsg    string
		// wantImport is an import, as "alias path", that the file must have
		// after the call. The alias is empty for an unaliased import.
		wantImport string
	}{
		{
			name: "rule alias added next to the file's alias - file uses ctx, rule expects context",
			root: &dst.File{
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
				},
			},
			imports:    map[string]string{"context": "context"},
			wantImport: `context context`,
		},
		{
			name: "rule alias added next to the implicit alias - file uses context, rule expects ctx",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Path: &dst.BasicLit{Value: `"context"`}, // implicit alias "context"
							},
						},
					},
				},
			},
			imports:    map[string]string{"ctx": "context"},
			wantImport: `ctx context`,
		},
		{
			name: "versioned path - no mismatch for implicit alias",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								// go.yaml.in/yaml/v3 declares "package yaml" - resolvePackageName correctly returns "yaml"
								Path: &dst.BasicLit{Value: `"go.yaml.in/yaml/v3"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{"yaml": "go.yaml.in/yaml/v3"},
			expectError: false, // No error - implicit alias matches inferred package name
		},
		{
			name: "no mismatch - aliases match",
			root: &dst.File{
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
				},
			},
			imports:     map[string]string{"ctx": "context"},
			expectError: false,
		},
		{
			name: "no mismatch - default alias matches rule",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Path: &dst.BasicLit{Value: `"context"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{"context": "context"},
			expectError: false,
		},
		{
			name: "blank imports are not checked for alias mismatch",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: dst.NewIdent("_"),
								Path: &dst.BasicLit{Value: `"net/http/pprof"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{"_": "net/http/pprof"},
			expectError: false,
		},
		{
			name:        "new import - no mismatch possible",
			root:        &dst.File{},
			imports:     map[string]string{"ctx": "context"},
			expectError: false, // Would fail later at importcfg resolution, not alias check
		},
		{
			name: "dot-import conflict - file uses explicit alias but rule requires dot-import",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: dst.NewIdent("runtime"),
								Path: &dst.BasicLit{Value: `"runtime"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{".": "runtime"},
			expectError: true,
			errorMsg:    "dot-import conflict",
		},
		{
			name: "dot-import conflict - file uses implicit alias but rule requires dot-import",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Path: &dst.BasicLit{Value: `"runtime"`}, // implicit alias "runtime"
							},
						},
					},
				},
			},
			imports:     map[string]string{".": "runtime"},
			expectError: true,
			errorMsg:    "dot-import conflict",
		},
		{
			name: "dot-import no conflict - file already uses dot-import",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: dst.NewIdent("."),
								Path: &dst.BasicLit{Value: `"runtime"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{".": "runtime"},
			expectError: false, // Both file and rule use dot-import
		},
		{
			name: "dot-import no conflict - path not in file yet",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Path: &dst.BasicLit{Value: `"fmt"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{".": "runtime"},
			expectError: false, // Path doesn't exist in file, no conflict
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock instrumentPhase with no importcfg (to avoid actual file operations)
			ip := &instrumentPhase{}

			err := ip.addRuleImports(t.Context(), tt.root, tt.imports, "test-rule")
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				return
			}
			// The call can still fail at the updateImportConfig step, since there is
			// no real importcfg. The imports are already in the file by then.
			if tt.wantImport != "" {
				assert.Contains(t, fileImports(tt.root), tt.wantImport)
			}
		})
	}
}

// TestInstrumentBlanksImportsTheRulesNoLongerUse checks that an import whose
// last use a rule removed is blanked, and that every other import is kept as
// it is.
func TestInstrumentBlanksImportsTheRulesNoLongerUse(t *testing.T) {
	src := t.TempDir()
	files := map[string]string{
		// The Clone call is the only use of strings.
		"only_use.go": `package p

import "strings"

func A(s string) string { return strings.Clone(s) }
`,
		// strings is still used after the Clone call is replaced.
		"other_use.go": `package p

import "strings"

var _ = strings.ToUpper

func B(s string) string { return strings.Clone(s) }
`,
		// The code of the getenv rule uses strings in place of the Clone call.
		"rule_use.go": `package p

import (
	"os"
	"strings"
)

func C(s string) string { return strings.Clone(s) + os.Getenv("HOME") }
`,
		// os is not used before the rules run, so the compiler must still
		// report it.
		"own_unused.go": `package p

import (
	"os"
	"strings"
)

func D(s string) string { return strings.Clone(s) }
`,
		// The case list of the type switch still uses strings, although the
		// guard variable has the same name.
		"type_switch.go": `package p

import "strings"

func E(s string) string { return strings.Clone(s) }

func keep(v any) {
	switch strings := v.(type) {
	case *strings.Builder:
		_ = strings
	}
}
`,
	}
	cloneRule := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "replace_clone", Imports: map[string]string{"fmt": "fmt"}},
		FunctionCall: "strings.Clone",
		ImportPath:   "strings",
		FuncName:     "Clone",
		Replace:      "fmt.Sprint({{ .CallArguments }})",
	}
	getenvRule := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "trim_getenv", Imports: map[string]string{"strings": "strings"}},
		FunctionCall: "os.Getenv",
		ImportPath:   "os",
		FuncName:     "Getenv",
		Replace:      "strings.TrimSpace({{ . }})",
	}
	rset := rule.NewInstRuleSet("example.com/p")
	rset.PackageName = "p"
	ip := newTestPhase()
	ip.workDir = t.TempDir()
	ip.importNames = map[string]string{"fmt": "fmt", "os": "os", "strings": "strings"}
	ip.compileArgs = []string{"compile", "-p", "example.com/p"}
	for name, content := range files {
		path := filepath.Join(src, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		rset.AddCallRule(path, cloneRule)
		rset.AddCallRule(path, getenvRule)
		ip.compileArgs = append(ip.compileArgs, path)
	}

	require.NoError(t, ip.instrument(t.Context(), rset))

	imports := func(name string) []string {
		root, err := ast.ParseFile(filepath.Join(ip.workDir, name))
		require.NoError(t, err)
		return fileImports(root)
	}
	assert.ElementsMatch(t, []string{"_ strings", " fmt"}, imports("only_use.go"))
	assert.ElementsMatch(t, []string{" strings", " fmt"}, imports("other_use.go"))
	assert.ElementsMatch(t, []string{" os", " strings", " fmt"}, imports("rule_use.go"))
	assert.ElementsMatch(t, []string{" os", "_ strings", " fmt"}, imports("own_unused.go"))
	assert.ElementsMatch(t, []string{" strings", " fmt"}, imports("type_switch.go"))
}

func TestUpdateImportConfigForFile(t *testing.T) {
	t.Run("empty file has no imports to update", func(t *testing.T) {
		ip := &instrumentPhase{}
		root := &dst.File{}

		// Should not error - no imports to process
		err := ip.updateImportConfigForFile(t.Context(), root, "test-rule")
		require.NoError(t, err)
	})

	t.Run("file with imports attempts update", func(t *testing.T) {
		ip := &instrumentPhase{
			// No importcfg path, so updateImportConfig will return early
			importConfigPath: "",
		}
		root := &dst.File{
			Decls: []dst.Decl{
				&dst.GenDecl{
					Tok: token.IMPORT,
					Specs: []dst.Spec{
						&dst.ImportSpec{Path: &dst.BasicLit{Value: `"log"`}},
						&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
					},
				},
			},
		}

		// Should not error - updateImportConfig returns early when no importcfg path
		err := ip.updateImportConfigForFile(t.Context(), root, "test-rule")
		require.NoError(t, err)
	})
}

// --- importAliasMap tests ---

func TestImportAliasMap_ReturnsFileAliases(t *testing.T) {
	root := parseFile(t, `package main

import f "fmt"

func run() {}
`)

	ip := newTestPhase()

	assert.Equal(t, "fmt", ip.importAliasMap(root)["f"])
}

func TestAddRuleImports_MultiAliasFile(t *testing.T) {
	// A file may import one path under several aliases. A rule alias that
	// names one of them needs no new import. A rule alias that names none of
	// them is imported next to them.
	root := parseFile(t, `package main

import (
    a "fmt"
    b "fmt"
)

var _, _ = a.Sprint, b.Sprint

func run() {}
`)

	ip := &instrumentPhase{}
	for _, ruleAlias := range []string{"a", "b"} {
		err := ip.addRuleImports(t.Context(), root, map[string]string{ruleAlias: "fmt"}, "test-rule")
		require.NoError(t, err,
			"rule alias %q is one of the file's aliases for fmt", ruleAlias)
	}
	assert.Equal(t, []string{"a fmt", "b fmt"}, fileImports(root))

	// The call fails at the updateImportConfig step, since there is no real
	// importcfg. The import is already in the file by then.
	_ = ip.addRuleImports(t.Context(), root, map[string]string{"c": "fmt"}, "test-rule")
	assert.Equal(t, []string{"a fmt", "b fmt", "c fmt"}, fileImports(root))
}

// --- injectedRuleAliases tests ---

func TestInjectedRuleAliases_QualifiersOnly(t *testing.T) {
	expr := &dst.CallExpr{
		Fun: &dst.SelectorExpr{X: &dst.Ident{Name: "traced"}, Sel: &dst.Ident{Name: "Call"}},
		Args: []dst.Expr{
			&dst.SelectorExpr{
				X:   &dst.SelectorExpr{X: &dst.Ident{Name: "other"}, Sel: &dst.Ident{Name: "Sub"}},
				Sel: &dst.Ident{Name: "Field"},
			},
			&dst.Ident{Name: "plain"},
		},
	}
	ruleImports := map[string]string{"traced": "fmt", "other": "os", "plain": "strings", "unused": "io"}

	aliases := injectedRuleAliases(ruleImports, expr)

	assert.Equal(t, map[string]bool{"traced": true, "other": true}, aliases,
		"only a rule alias used as a qualifier is returned")
}

func TestInjectedRuleAliases_DotAndBlankNeverReturned(t *testing.T) {
	expr := &dst.SelectorExpr{X: &dst.Ident{Name: "_"}, Sel: &dst.Ident{Name: "X"}}

	aliases := injectedRuleAliases(map[string]string{"_": "net/http/pprof", ".": "fmt"}, expr)

	assert.Empty(t, aliases)
}

func TestInjectedRuleAliases_NoRuleImports(t *testing.T) {
	expr := &dst.SelectorExpr{X: &dst.Ident{Name: "traced"}, Sel: &dst.Ident{Name: "Call"}}

	assert.Empty(t, injectedRuleAliases(nil, expr))
}

// --- usedRuleImports tests ---

func TestUsedRuleImports_BlankAndDotAliasesAlwaysKept(t *testing.T) {
	root := parseFile(t, `package main

func f() {}
`)
	ruleImports := map[string]string{
		"_": "example.com/sideeffect",
		".": "example.com/dotimport",
	}

	used := usedRuleImports(root, ruleImports)

	assert.Equal(t, ruleImports, used)
}

func TestUsedRuleImports_OnlyReferencedAliasesKept(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	traced.Call()
}
`)
	ruleImports := map[string]string{
		"traced":    "fmt",
		"unrelated": "example.com/unrelated",
	}

	used := usedRuleImports(root, ruleImports)

	assert.Equal(t, map[string]string{"traced": "fmt"}, used)
}

func TestUsedRuleImports_EmptyRuleImports(t *testing.T) {
	root := parseFile(t, `package main

func f() {}
`)

	used := usedRuleImports(root, nil)

	assert.Nil(t, used)
}

func TestUsedRuleImports_PlainIdentifierWithoutSelector(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	use(traced)
}
`)
	ruleImports := map[string]string{"traced": "fmt"}

	used := usedRuleImports(root, ruleImports)

	assert.Empty(t, used)
}

func TestUsedRuleImports_ChainedSelector(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	pkg.traced.Call()
}
`)
	ruleImports := map[string]string{"traced": "fmt"}

	used := usedRuleImports(root, ruleImports)

	assert.Empty(t, used)
}

func TestUsedRuleImports_MultipleReferencesCountedOnce(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	traced.Call()
	traced.Call()
}
`)
	ruleImports := map[string]string{"traced": "fmt"}

	used := usedRuleImports(root, ruleImports)

	assert.Equal(t, map[string]string{"traced": "fmt"}, used)
}

func TestUsedRuleImports_MixedAliasKinds(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	traced.Call()
}
`)
	ruleImports := map[string]string{
		"traced": "fmt",
		"unused": "example.com/unused",
		"_":      "example.com/sideeffect",
		".":      "example.com/dotimport",
	}

	used := usedRuleImports(root, ruleImports)

	assert.Equal(t, map[string]string{
		"traced": "fmt",
		"_":      "example.com/sideeffect",
		".":      "example.com/dotimport",
	}, used)
}

func TestCheckRuleAliasShadowing_BodyLevelDeclReportsConflict(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "short var decl",
			body: "f := sink{}\n\t_ = f",
		},
		{
			name: "var decl",
			body: "var f sink\n\t_ = f",
		},
		{
			name: "const decl",
			body: "const f = 1\n\t_ = f",
		},
		{
			name: "type decl",
			body: "type f struct{}\n\tvar _ f",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := parseFile(t, "package main\n\nfunc Run() {\n\t"+tt.body+"\n}\n")
			fn := findFuncDeclInFile(t, root, "Run")

			err := checkRuleAliasShadowing(map[string]bool{"f": true}, fn)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "rule import alias conflict")
			assert.Contains(t, err.Error(), "body-level variable")
		})
	}
}

func TestCheckRuleAliasShadowing_NestedLocalDoesNotReportConflict(t *testing.T) {
	// Locals bound inside nested statements and closures are scoped there;
	// they do not shadow code injected at the function's top level.
	root := parseFile(t, `package main

func Run() {
	if true {
		f := sink{}
		_ = f
	}
	for i := 0; i < 1; i++ {
		var f sink
		_ = f
	}
	func() {
		const f = 1
		_ = f
	}()
}
`)
	fn := findFuncDeclInFile(t, root, "Run")

	err := checkRuleAliasShadowing(map[string]bool{"f": true}, fn)

	assert.NoError(t, err)
}

func TestCheckRuleAliasShadowing_NonDefineAssignDoesNotReportConflict(t *testing.T) {
	// A plain assignment to a selector must not be collected as a binding.
	root := parseFile(t, `package main

func Run() {
	f.field = 1
	f[0] = 1
}
`)
	fn := findFuncDeclInFile(t, root, "Run")

	err := checkRuleAliasShadowing(map[string]bool{"f": true}, fn)

	assert.NoError(t, err)
}

func TestCheckRuleAliasShadowing_NilBodySignatureNameReportsConflict(t *testing.T) {
	// External (e.g. assembly) functions have no body; signature names must
	// still be checked.
	root := parseFile(t, "package main\n\nfunc Run(f sink)\n")
	fn := findFuncDeclInFile(t, root, "Run")
	require.Nil(t, fn.Body)

	err := checkRuleAliasShadowing(map[string]bool{"f": true}, fn)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule import alias conflict")
}

func TestCheckRuleAliasShadowing_NoAliasesOrNilEnclosingReportsNoError(t *testing.T) {
	root := parseFile(t, "package main\n\nfunc Run(f sink) {\n\tg := 1\n\t_ = g\n}\n")
	fn := findFuncDeclInFile(t, root, "Run")

	assert.NoError(t, checkRuleAliasShadowing(nil, fn))
	assert.NoError(t, checkRuleAliasShadowing(map[string]bool{"f": true}, nil))
}

func TestCheckRuleAliasShadowingAt_ScopeOnInjectionPathReportsConflict(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "nested block",
			body: "if true {\n\t\tf := sink{}\n\t\t_ = f\n\t\thttp.Get(\"url\")\n\t}",
		},
		{
			name: "if init statement",
			body: "if f := newSink(); true {\n\t\t_ = f\n\t\thttp.Get(\"url\")\n\t}",
		},
		{
			name: "for init statement",
			body: "for f := 0; f < 1; f++ {\n\t\thttp.Get(\"url\")\n\t}",
		},
		{
			name: "range variables",
			body: "for _, f := range []string{\"a\"} {\n\t\t_ = f\n\t\thttp.Get(\"url\")\n\t}",
		},
		{
			name: "switch init statement",
			body: "switch f := 1; f {\n\tcase 1:\n\t\thttp.Get(\"url\")\n\t}",
		},
		{
			name: "type switch init statement",
			body: "switch f := x.(type) {\n\tcase int:\n\t\t_ = f\n\t\thttp.Get(\"url\")\n\t}",
		},
		{
			name: "case clause",
			body: "switch {\n\tcase true:\n\t\tf := sink{}\n\t\t_ = f\n\t\thttp.Get(\"url\")\n\t}",
		},
		{
			name: "select comm clause",
			body: "select {\n\tcase f := <-ch:\n\t\t_ = f\n\t\thttp.Get(\"url\")\n\t}",
		},
		{
			name: "closure parameter",
			body: "g := func(f sink) {\n\t\thttp.Get(\"url\")\n\t}\n\t_ = g",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := parseFile(t, "package main\n\nfunc Run() {\n\t"+tt.body+"\n}\n")
			fn := findFuncDeclInFile(t, root, "Run")
			call := findCallInFunc(t, fn)

			err := checkRuleAliasShadowingAt(map[string]bool{"f": true}, fn, call)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "rule import alias conflict")
			assert.Contains(t, err.Error(), "injection point")
		})
	}
}

func TestCheckRuleAliasShadowingAt_ScopeOutsideInjectionPathReportsNoError(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "sibling block",
			body: "if true {\n\t\tf := sink{}\n\t\t_ = f\n\t}\n\thttp.Get(\"url\")",
		},
		{
			name: "else block",
			body: "if true {\n\t\tf := sink{}\n\t\t_ = f\n\t} else {\n\t\thttp.Get(\"url\")\n\t}",
		},
		{
			name: "unrelated closure",
			body: "g := func() {\n\t\tf := sink{}\n\t\t_ = f\n\t}\n\t_ = g\n\thttp.Get(\"url\")",
		},
		{
			name: "unrelated loop",
			body: "for {\n\t\tf := sink{}\n\t\t_ = f\n\t\tbreak\n\t}\n\thttp.Get(\"url\")",
		},
		{
			name: "later sibling case clause",
			body: "switch {\n\tcase true:\n\t\tf := sink{}\n\t\t_ = f\n\tcase false:\n\t\thttp.Get(\"url\")\n\t}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := parseFile(t, "package main\n\nfunc Run() {\n\t"+tt.body+"\n}\n")
			fn := findFuncDeclInFile(t, root, "Run")
			call := findCallInFunc(t, fn)

			err := checkRuleAliasShadowingAt(map[string]bool{"f": true}, fn, call)

			assert.NoError(t, err)
		})
	}
}

func TestCheckRuleAliasShadowingAt_NoAliasesOrNilEnclosingReportsNoError(t *testing.T) {
	root := parseFile(t, `package main

func Run() {
	if true {
		f := sink{}
		_ = f
		http.Get("url")
	}
}
`)
	fn := findFuncDeclInFile(t, root, "Run")
	call := findCallInFunc(t, fn)

	assert.NoError(t, checkRuleAliasShadowingAt(nil, fn, call))
	assert.NoError(t, checkRuleAliasShadowingAt(map[string]bool{"f": true}, nil, call))
}

func TestScopeNamesBetween_TargetOutsideEnclosingPanics(t *testing.T) {
	root := parseFile(t, `package main

func Run() {}

func Other() {
	http.Get("url")
}
`)
	run := findFuncDeclInFile(t, root, "Run")
	other := findFuncDeclInFile(t, root, "Other")
	call := findCallInFunc(t, other)

	assert.Panics(t, func() {
		scopeNamesBetween(run, call)
	})
}

// findCallInFunc returns the first Get selector call in fn.
func findCallInFunc(t *testing.T, fn *dst.FuncDecl) *dst.CallExpr {
	t.Helper()
	var found *dst.CallExpr
	dst.Inspect(fn, func(node dst.Node) bool {
		if found != nil {
			return false
		}
		call, ok := node.(*dst.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*dst.SelectorExpr)
		if ok && sel.Sel.Name == "Get" {
			found = call
			return false
		}
		return true
	})
	require.NotNil(t, found, "no Get call in %s", fn.Name.Name)
	return found
}

// fileImports returns root's imports, each as "alias path" with an empty
// alias for an unaliased import.
func fileImports(root *dst.File) []string {
	var specs []string
	for _, decl := range root.Decls {
		gen, ok := decl.(*dst.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gen.Specs {
			imp := spec.(*dst.ImportSpec)
			alias := ""
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			specs = append(specs, alias+" "+strings.Trim(imp.Path.Value, `"`))
		}
	}
	return specs
}
