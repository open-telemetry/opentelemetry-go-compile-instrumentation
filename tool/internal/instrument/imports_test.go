// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleRuleImports_AliasMismatch(t *testing.T) {
	tests := []struct {
		name        string
		root        *dst.File
		imports     map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name: "alias mismatch - file uses ctx but rule expects context",
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
			imports:     map[string]string{"context": "context"},
			expectError: true,
			errorMsg:    "import alias mismatch",
		},
		{
			name: "implicit alias mismatch - file uses context but rule expects ctx",
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
			imports:     map[string]string{"ctx": "context"},
			expectError: true,
			errorMsg:    "import alias mismatch",
		},
		{
			name: "gopkg.in style path - no mismatch for implicit alias",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								// gopkg.in/yaml.v3 declares "package yaml" - resolvePackageName correctly returns "yaml"
								Path: &dst.BasicLit{Value: `"gopkg.in/yaml.v3"`},
							},
						},
					},
				},
			},
			imports:     map[string]string{"yaml": "gopkg.in/yaml.v3"},
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
			} else if err != nil {
				// Note: This may still fail at updateImportConfig step since we don't have
				// a real importcfg setup. We're only testing the alias mismatch detection.
				// If there's an error, it shouldn't be about alias mismatch.
				assert.NotContains(t, err.Error(), "import alias mismatch")
			}
		})
	}
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

// --- resolveImportOverrides tests ---

func TestResolveImportOverrides_NoRuleImportsProducesNoOverrides(t *testing.T) {
	root := parseFile(t, `package main

func f() {}
`)

	ip := newTestPhase()
	importAliases, overrides := ip.resolveImportOverrides(root, nil)

	assert.Empty(t, importAliases)
	assert.Empty(t, overrides)
}

func TestResolveImportOverrides_ReturnsBothFileAliasesAndOverrides(t *testing.T) {
	root := parseFile(t, `package main

import f "fmt"

func run() {}
`)

	ip := newTestPhase()
	importAliases, overrides := ip.resolveImportOverrides(root, map[string]string{"traced": "fmt"})

	require.Equal(t, "fmt", importAliases["f"])
	assert.Equal(t, map[string]string{"traced": "f"}, overrides)
}

// --- resolveAliasOverrides tests ---

func TestResolveAliasOverrides_MismatchProducesOverride(t *testing.T) {
	ruleImports := map[string]string{"traced": "fmt"}
	existingAliases := map[string]string{"fmt": "f"}

	overrides := resolveAliasOverrides(ruleImports, existingAliases)

	assert.Equal(t, map[string]string{"traced": "f"}, overrides)
}

func TestResolveAliasOverrides_MatchingAliasProducesNoOverride(t *testing.T) {
	ruleImports := map[string]string{"redis": "github.com/redis/go-redis/v9"}
	existingAliases := map[string]string{"github.com/redis/go-redis/v9": "redis"}

	overrides := resolveAliasOverrides(ruleImports, existingAliases)

	assert.Empty(t, overrides)
}

func TestResolveAliasOverrides_PathNotYetImportedProducesNoOverride(t *testing.T) {
	ruleImports := map[string]string{"redis": "github.com/redis/go-redis/v9"}
	existingAliases := map[string]string{} // path not present in the file yet

	overrides := resolveAliasOverrides(ruleImports, existingAliases)

	assert.Empty(t, overrides)
}

func TestResolveAliasOverrides_DotAndBlankAliasesAreExempt(t *testing.T) {
	ruleImports := map[string]string{".": "fmt", "_": "net/http"}
	existingAliases := map[string]string{"fmt": "f", "net/http": "h"}

	overrides := resolveAliasOverrides(ruleImports, existingAliases)

	assert.Empty(t, overrides, "'.' and '_' aliases must never be substituted")
}

// --- replaceQualifierAliases tests ---

func TestReplaceQualifierAliases_RewritesMatchingQualifiers(t *testing.T) {
	expr := &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "traced"},
			Sel: &dst.Ident{Name: "Call"},
		},
		Args: []dst.Expr{
			&dst.SelectorExpr{X: &dst.Ident{Name: "traced"}, Sel: &dst.Ident{Name: "Option"}},
			&dst.Ident{Name: "unrelated"},
		},
	}

	replaceQualifierAliases(expr, map[string]string{"traced": "f"})

	sel := expr.Fun.(*dst.SelectorExpr)
	assert.Equal(t, "f", sel.X.(*dst.Ident).Name)
	argSel := expr.Args[0].(*dst.SelectorExpr)
	assert.Equal(t, "f", argSel.X.(*dst.Ident).Name)
	assert.Equal(t, "unrelated", expr.Args[1].(*dst.Ident).Name)
}

func TestReplaceQualifierAliases_NoOverridesLeavesExprUntouched(t *testing.T) {
	expr := &dst.SelectorExpr{X: &dst.Ident{Name: "traced"}, Sel: &dst.Ident{Name: "Call"}}

	replaceQualifierAliases(expr, nil)

	assert.Equal(t, "traced", expr.X.(*dst.Ident).Name)
}

func TestReplaceQualifierAliases_NonIdentQualifier(t *testing.T) {
	expr := &dst.SelectorExpr{
		X: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "traced"},
			Sel: &dst.Ident{Name: "Sub"},
		},
		Sel: &dst.Ident{Name: "Call"},
	}

	replaceQualifierAliases(expr, map[string]string{"traced": "f"})

	inner := expr.X.(*dst.SelectorExpr)
	assert.Equal(t, "f", inner.X.(*dst.Ident).Name, "the nested identifier must still be rewritten")
	assert.Equal(t, "Sub", inner.Sel.Name)
	assert.Equal(t, "Call", expr.Sel.Name)
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
