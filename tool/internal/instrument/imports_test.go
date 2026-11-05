// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/imports"
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
			name: "different alias - file uses ctx, rule adds context as a second import",
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
			expectError: false,
		},
		{
			name: "implicit alias - file uses context, rule adds ctx as a second import",
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
			expectError: false,
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
			} else if err != nil {
				// Note: This may still fail at updateImportConfig since we don't have
				// a real importcfg setup. If there's an error, it must not be about
				// an alias mismatch, which is no longer an error.
				assert.NotContains(t, err.Error(), "import alias mismatch")
			}
		})
	}
}

func TestAddRuleImports_SequentialAliasesSeeAddedImports(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
	}{
		{"unrelated import already exists", "package main\nimport \"fmt\"\nfunc run() { stra.ToUpper(\"x\"); strb.ToLower(\"X\") }"},
		{"package initially absent", "package main\nfunc run() { stra.ToUpper(\"x\"); strb.ToLower(\"X\") }"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := parseFile(t, tt.src)
			ip := &instrumentPhase{}
			require.NoError(t, ip.addRuleImports(t.Context(), root, map[string]string{"stra": "strings"}, "first"))
			require.NoError(t, ip.addRuleImports(t.Context(), root, map[string]string{"strb": "strings"}, "second"))

			aliases := map[string]bool{}
			for _, decl := range root.Decls {
				gen, ok := decl.(*dst.GenDecl)
				if !ok || gen.Tok != token.IMPORT {
					continue
				}
				for _, spec := range gen.Specs {
					imp := spec.(*dst.ImportSpec)
					if imp.Path.Value == `"strings"` && imp.Name != nil {
						aliases[imp.Name.Name] = true
					}
				}
			}
			assert.True(t, aliases["stra"])
			assert.True(t, aliases["strb"])
		})
	}
}

func TestRuleAliasAvoidsPackageLevelTracerCollision(t *testing.T) {
	const importPath = "github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	root := parseFile(t, `package main

import helper "`+importPath+`"

func tracer() {}

func run() {
	__dd_tracer.Start()
}
`)

	resolution := imports.FindNew(t.Context(), root, map[string]string{
		"__dd_tracer": importPath,
	})

	require.Equal(t, map[string]string{"__dd_tracer": importPath}, resolution.NewImports)
	assert.Equal(t, []string{"helper"}, resolution.ExistingAliasSets[importPath])
	assert.Equal(t, map[string]string{"__dd_tracer": importPath},
		usedRuleImports(root, map[string]string{"__dd_tracer": importPath}))
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

func TestAddRuleImports_MultiAliasFileAcceptsEveryExistingAlias(t *testing.T) {
	// A file may import one path under several aliases. A rule alias that
	// names one of them must pass validation; a rule alias that names none
	// of them must fail it.
	root := parseFile(t, `package main

import (
    a "fmt"
    b "fmt"
)

func run() {}
`)

	ip := &instrumentPhase{}
	for _, ruleAlias := range []string{"a", "b"} {
		err := ip.addRuleImports(t.Context(), root, map[string]string{ruleAlias: "fmt"}, "test-rule")
		require.NoError(t, err,
			"rule alias %q is one of the file's aliases for fmt", ruleAlias)
	}

	// A rule alias the file doesn't use is added as another import.
	err := ip.addRuleImports(t.Context(), root, map[string]string{"c": "fmt"}, "test-rule")
	require.NoError(t, err)

	var found bool
	dst.Inspect(root, func(n dst.Node) bool {
		if spec, ok := n.(*dst.ImportSpec); ok && spec.Name != nil && spec.Name.Name == "c" {
			found = true
		}
		return true
	})
	assert.True(t, found, "rule alias c should be added as a second import of fmt")
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

func TestUsedRuleImports_RuleAliasRetainedWhenReferenced(t *testing.T) {
	root := parseFile(t, `package main

func f() {
	traced.Value()
}
`)
	ruleImports := map[string]string{"traced": "fmt"}
	used := usedRuleImports(root, ruleImports)

	assert.Equal(t, ruleImports, used)
}
