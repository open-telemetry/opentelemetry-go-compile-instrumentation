// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
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

// fileWithImport builds a minimal *dst.File declaring a single import, with
// an explicit alias if given, or an implicit one (resolved from path) if not.
func fileWithImport(alias, path string) *dst.File {
	spec := &dst.ImportSpec{Path: &dst.BasicLit{Value: `"` + path + `"`}}
	if alias != "" {
		spec.Name = dst.NewIdent(alias)
	}
	return &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{Tok: token.IMPORT, Specs: []dst.Spec{spec}},
		},
	}
}

func TestInjectConstraintImports_SameFileNoOp(t *testing.T) {
	ip := newTestPhase()
	ip.target = fileWithImport("", "example.com/constraints")
	fields := &dst.FieldList{List: []*dst.Field{{
		Names: []*dst.Ident{ast.Ident("T")},
		Type:  &dst.SelectorExpr{X: ast.Ident("constraints"), Sel: ast.Ident("Ordered")},
	}}}

	err := ip.injectConstraintImports(t.Context(), ip.target, fields)
	require.NoError(t, err)
	// declFile == ip.target must short-circuit before touching imports at all.
	assert.Len(t, ip.target.Decls, 1, "same-file case must not add a second import decl")
}

func TestInjectConstraintImports_AddsMissingImport(t *testing.T) {
	ip := newTestPhase()
	ip.target = &dst.File{} // no imports yet
	declFile := fileWithImport("", "fmt")
	fields := &dst.FieldList{List: []*dst.Field{{
		Names: []*dst.Ident{ast.Ident("T")},
		Type:  &dst.SelectorExpr{X: ast.Ident("fmt"), Sel: ast.Ident("Stringer")},
	}}}

	err := ip.injectConstraintImports(t.Context(), declFile, fields)
	require.NoError(t, err)

	genDecl, ok := ip.target.Decls[0].(*dst.GenDecl)
	require.True(t, ok)
	require.Len(t, genDecl.Specs, 1)
	spec, ok := genDecl.Specs[0].(*dst.ImportSpec)
	require.True(t, ok)
	assert.Equal(t, `"fmt"`, spec.Path.Value)
}

func TestInjectConstraintImports_CompositeConstraint(t *testing.T) {
	ip := newTestPhase()
	ip.target = &dst.File{}
	declFile := fileWithImport("", "fmt")
	// A constraint nested inside a slice type: []fmt.Stringer. The qualifier
	// must still be found by walking the field's type, not just type-asserting
	// it directly to *dst.SelectorExpr.
	fields := &dst.FieldList{List: []*dst.Field{{
		Names: []*dst.Ident{ast.Ident("T")},
		Type: &dst.ArrayType{
			Elt: &dst.SelectorExpr{X: ast.Ident("fmt"), Sel: ast.Ident("Stringer")},
		},
	}}}

	err := ip.injectConstraintImports(t.Context(), declFile, fields)
	require.NoError(t, err)

	genDecl, ok := ip.target.Decls[0].(*dst.GenDecl)
	require.True(t, ok)
	require.Len(t, genDecl.Specs, 1)
}

func TestInjectConstraintImports_UnionConstraint(t *testing.T) {
	ip := newTestPhase()
	ip.target = &dst.File{}
	declFile := fileWithImport("", "fmt")
	// A union constraint referencing a qualified type on one side: T
	// fmt.Stringer | int. The walk must find the selector inside a
	// *dst.BinaryExpr, not just inside array/slice element types.
	fields := &dst.FieldList{List: []*dst.Field{{
		Names: []*dst.Ident{ast.Ident("T")},
		Type: &dst.BinaryExpr{
			X:  &dst.SelectorExpr{X: ast.Ident("fmt"), Sel: ast.Ident("Stringer")},
			Op: token.OR,
			Y:  ast.Ident("int"),
		},
	}}}

	err := ip.injectConstraintImports(t.Context(), declFile, fields)
	require.NoError(t, err)

	genDecl, ok := ip.target.Decls[0].(*dst.GenDecl)
	require.True(t, ok)
	require.Len(t, genDecl.Specs, 1)
	spec, ok := genDecl.Specs[0].(*dst.ImportSpec)
	require.True(t, ok)
	assert.Equal(t, `"fmt"`, spec.Path.Value)
}

func TestInjectConstraintImports_MultipleFieldsBatchIntoOneCall(t *testing.T) {
	ip := newTestPhase()
	ip.target = &dst.File{}
	// Both paths are stdlib on purpose: AddToFile's createSpec calls
	// pkgload.ResolvePackageName to decide whether an alias needs to be
	// spelled out explicitly, which shells out to the real build system and
	// fatals the whole process on an unresolvable path (as opposed to
	// returning an error) -- so a fake or non-stdlib path could crash this
	// test rather than fail it cleanly, depending on what else the module
	// graph happens to pull in.
	declFile := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{Tok: token.IMPORT, Specs: []dst.Spec{
				&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
				&dst.ImportSpec{Path: &dst.BasicLit{Value: `"os"`}},
			}},
		},
	}
	// Two type parameters, each needing a different import, resolved in one
	// extractReceiverTypeParams-style call -- both should land in ip.target
	// from a single injectConstraintImports invocation.
	fields := &dst.FieldList{List: []*dst.Field{
		{
			Names: []*dst.Ident{ast.Ident("T")},
			Type:  &dst.SelectorExpr{X: ast.Ident("fmt"), Sel: ast.Ident("Stringer")},
		},
		{
			Names: []*dst.Ident{ast.Ident("U")},
			Type:  &dst.SelectorExpr{X: ast.Ident("os"), Sel: ast.Ident("File")},
		},
	}}

	err := ip.injectConstraintImports(t.Context(), declFile, fields)
	require.NoError(t, err)

	genDecl, ok := ip.target.Decls[0].(*dst.GenDecl)
	require.True(t, ok)
	require.Len(t, genDecl.Specs, 2, "both needed imports should be added")

	gotPaths := make(map[string]bool)
	for _, spec := range genDecl.Specs {
		importSpec, isImportSpec := spec.(*dst.ImportSpec)
		require.True(t, isImportSpec)
		gotPaths[importSpec.Path.Value] = true
	}
	assert.True(t, gotPaths[`"fmt"`])
	assert.True(t, gotPaths[`"os"`])
}

func TestInjectConstraintImports_NoQualifiedConstraint(t *testing.T) {
	ip := newTestPhase()
	ip.target = &dst.File{}
	declFile := fileWithImport("", "golang.org/x/exp/constraints")
	// A plain built-in constraint, e.g. "comparable" -- no package qualifier
	// anywhere in it, so nothing should be added.
	fields := &dst.FieldList{List: []*dst.Field{{
		Names: []*dst.Ident{ast.Ident("T")},
		Type:  ast.Ident("comparable"),
	}}}

	err := ip.injectConstraintImports(t.Context(), declFile, fields)
	require.NoError(t, err)
	assert.Empty(t, ip.target.Decls, "no import should be added when the constraint has no package qualifier")
}

func TestInjectConstraintImports_AliasConflict(t *testing.T) {
	ip := newTestPhase()
	// ip.target already imports a different path under the alias "fmt".
	ip.target = fileWithImport("fmt", "some/other/package")
	declFile := fileWithImport("", "fmt")
	fields := &dst.FieldList{List: []*dst.Field{{
		Names: []*dst.Ident{ast.Ident("T")},
		Type:  &dst.SelectorExpr{X: ast.Ident("fmt"), Sel: ast.Ident("Stringer")},
	}}}

	err := ip.injectConstraintImports(t.Context(), declFile, fields)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import conflict")
}

func TestInjectConstraintImports_NilOrEmptyGuards(t *testing.T) {
	ip := newTestPhase()
	ip.target = &dst.File{}

	require.NoError(t, ip.injectConstraintImports(t.Context(), nil, &dst.FieldList{}))
	require.NoError(t, ip.injectConstraintImports(t.Context(), fileWithImport("", "fmt"), nil))
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
