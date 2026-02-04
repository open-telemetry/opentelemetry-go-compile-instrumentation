// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package imports

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferPackageName(t *testing.T) {
	tests := []struct {
		importPath string
		expected   string
	}{
		{"fmt", "fmt"},
		{"encoding/json", "json"},
		{"net/http", "http"},
		{"context", "context"},
		{"io", "io"},
		{"strings", "strings"},
		{"sync", "sync"},
		{"time", "time"},
		{"github.com/dave/dst", "dst"},
		{"github.com/stretchr/testify/assert", "assert"},
	}

	for _, tt := range tests {
		t.Run(tt.importPath, func(t *testing.T) {
			result := ResolvePackageName(t.Context(), tt.importPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetExisting(t *testing.T) {
	tests := []struct {
		name     string
		root     *dst.File
		expected map[string]string
	}{
		{
			name:     "no imports",
			root:     &dst.File{},
			expected: map[string]string{},
		},
		{
			name: "standard imports",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"context"`}},
						},
					},
				},
			},
			expected: map[string]string{"fmt": "fmt", "context": "context"},
		},
		{
			name: "aliased imports",
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
			expected: map[string]string{"ctx": "context"},
		},
		{
			name: "blank import",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: dst.NewIdent("_"),
								Path: &dst.BasicLit{Value: `"unsafe"`},
							},
						},
					},
				},
			},
			expected: map[string]string{"_": "unsafe"},
		},
		{
			name: "multiple import groups",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
						},
					},
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"strings"`}},
						},
					},
				},
			},
			expected: map[string]string{"fmt": "fmt", "strings": "strings"},
		},
		{
			name: "path with package name",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"net/http"`}},
						},
					},
				},
			},
			expected: map[string]string{"http": "net/http"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getExisting(t.Context(), tt.root)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateSpec(t *testing.T) {
	tests := []struct {
		name        string
		alias       string
		importPath  string
		expectName  bool
		expectAlias string
	}{
		{
			name:       "standard import no alias",
			alias:      "fmt",
			importPath: "fmt",
			expectName: false,
		},
		{
			name:        "explicit alias",
			alias:       "ctx",
			importPath:  "context",
			expectName:  true,
			expectAlias: "ctx",
		},
		{
			name:        "blank import",
			alias:       "_",
			importPath:  "unsafe",
			expectName:  true,
			expectAlias: "_",
		},
		{
			name:       "package name matches path base - http",
			alias:      "http",
			importPath: "net/http",
			expectName: false,
		},
		{
			name:       "package name matches path base - json",
			alias:      "json",
			importPath: "encoding/json",
			expectName: false,
		},
		{
			name:        "explicit alias different from path base",
			alias:       "myjson",
			importPath:  "encoding/json",
			expectName:  true,
			expectAlias: "myjson",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := createSpec(t.Context(), tt.alias, tt.importPath)
			require.NotNil(t, spec)
			require.NotNil(t, spec.Path)

			if tt.expectName {
				require.NotNil(t, spec.Name, "expected Name to be set")
				assert.Equal(t, tt.expectAlias, spec.Name.Name)
			} else {
				assert.Nil(t, spec.Name, "expected Name to be nil")
			}
		})
	}
}

func TestFindFirstDecl(t *testing.T) {
	tests := []struct {
		name     string
		root     *dst.File
		expected bool
	}{
		{
			name:     "no imports",
			root:     &dst.File{},
			expected: false,
		},
		{
			name: "has import",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "import after other decls",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.FuncDecl{Name: &dst.Ident{Name: "main"}},
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findFirstDecl(tt.root)
			if tt.expected {
				assert.NotNil(t, result)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestAddToFile(t *testing.T) {
	tests := []struct {
		name        string
		root        *dst.File
		newImports  map[string]string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, *dst.File)
	}{
		{
			name:       "add to empty file",
			root:       &dst.File{},
			newImports: map[string]string{"fmt": "fmt"},
			checkResult: func(t *testing.T, root *dst.File) {
				require.Len(t, root.Decls, 1)
				genDecl, ok := root.Decls[0].(*dst.GenDecl)
				require.True(t, ok)
				assert.Equal(t, token.IMPORT, genDecl.Tok)
				require.Len(t, genDecl.Specs, 1)
			},
		},
		{
			name: "import conflict",
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
			newImports:  map[string]string{"ctx": "other/context"},
			expectError: true,
			errorMsg:    "import conflict",
		},
		{
			name: "duplicate import ignored",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
						},
					},
				},
			},
			newImports: map[string]string{"fmt": "fmt"},
			checkResult: func(t *testing.T, root *dst.File) {
				require.Len(t, root.Decls, 1)
				genDecl := root.Decls[0].(*dst.GenDecl)
				assert.Len(t, genDecl.Specs, 1)
			},
		},
		{
			name: "add to existing import block",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
						},
					},
				},
			},
			newImports: map[string]string{"strings": "strings"},
			checkResult: func(t *testing.T, root *dst.File) {
				require.Len(t, root.Decls, 1)
				genDecl := root.Decls[0].(*dst.GenDecl)
				assert.Len(t, genDecl.Specs, 2)
			},
		},
		{
			name:       "empty imports map",
			root:       &dst.File{},
			newImports: map[string]string{},
			checkResult: func(t *testing.T, root *dst.File) {
				assert.Empty(t, root.Decls)
			},
		},
		{
			name: "same path different alias - should skip",
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
			newImports: map[string]string{"c": "context"},
			checkResult: func(t *testing.T, root *dst.File) {
				require.Len(t, root.Decls, 1)
				genDecl := root.Decls[0].(*dst.GenDecl)
				assert.Len(t, genDecl.Specs, 1)
				spec := genDecl.Specs[0].(*dst.ImportSpec)
				require.NotNil(t, spec.Name)
				assert.Equal(t, "ctx", spec.Name.Name)
			},
		},
		{
			name: "allow multiple blank imports",
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
			newImports: map[string]string{"_": "github.com/dave/dst"},
			checkResult: func(t *testing.T, root *dst.File) {
				require.Len(t, root.Decls, 1)
				genDecl := root.Decls[0].(*dst.GenDecl)
				assert.Len(t, genDecl.Specs, 2)
			},
		},
		{
			name: "allow multiple dot imports",
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
			newImports: map[string]string{".": "runtime"},
			checkResult: func(t *testing.T, root *dst.File) {
				require.Len(t, root.Decls, 1)
				genDecl := root.Decls[0].(*dst.GenDecl)
				assert.Len(t, genDecl.Specs, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AddToFile(t.Context(), tt.root, tt.newImports)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				if tt.checkResult != nil {
					tt.checkResult(t, tt.root)
				}
			}
		})
	}
}

func TestFindNew(t *testing.T) {
	tests := []struct {
		name             string
		root             *dst.File
		ruleImports      map[string]string
		expectedNew      map[string]string
		expectedAliases  map[string]string
		expectedExplicit map[string]bool
	}{
		{
			name:             "nil rule imports",
			root:             &dst.File{},
			ruleImports:      nil,
			expectedNew:      map[string]string{},
			expectedAliases:  map[string]string{},
			expectedExplicit: map[string]bool{},
		},
		{
			name:             "empty rule imports",
			root:             &dst.File{},
			ruleImports:      map[string]string{},
			expectedNew:      map[string]string{},
			expectedAliases:  map[string]string{},
			expectedExplicit: map[string]bool{},
		},
		{
			name:             "no existing imports returns all rule imports",
			root:             &dst.File{},
			ruleImports:      map[string]string{"fmt": "fmt", "strings": "strings"},
			expectedNew:      map[string]string{"fmt": "fmt", "strings": "strings"},
			expectedAliases:  map[string]string{},
			expectedExplicit: map[string]bool{},
		},
		{
			name: "all imports already exist - implicit aliases",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"strings"`}},
						},
					},
				},
			},
			ruleImports:      map[string]string{"fmt": "fmt", "strings": "strings"},
			expectedNew:      map[string]string{},
			expectedAliases:  map[string]string{"fmt": "fmt", "strings": "strings"},
			expectedExplicit: map[string]bool{},
		},
		{
			name: "some imports are new",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
						},
					},
				},
			},
			ruleImports:      map[string]string{"fmt": "fmt", "strings": "strings", "io": "io"},
			expectedNew:      map[string]string{"strings": "strings", "io": "io"},
			expectedAliases:  map[string]string{"fmt": "fmt"},
			expectedExplicit: map[string]bool{},
		},
		{
			name: "explicit alias - should track as explicit",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: &dst.Ident{Name: "ctx"},
								Path: &dst.BasicLit{Value: `"context"`},
							},
						},
					},
				},
			},
			ruleImports:      map[string]string{"c": "context"},
			expectedNew:      map[string]string{},
			expectedAliases:  map[string]string{"context": "ctx"},
			expectedExplicit: map[string]bool{"context": true},
		},
		{
			name: "implicit alias - no explicit flag",
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
			ruleImports:      map[string]string{"ctx": "context"},
			expectedNew:      map[string]string{},
			expectedAliases:  map[string]string{"context": "context"},
			expectedExplicit: map[string]bool{},
		},
		{
			name: "blank import - explicit alias",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{
								Name: &dst.Ident{Name: "_"},
								Path: &dst.BasicLit{Value: `"net/http/pprof"`},
							},
						},
					},
				},
			},
			ruleImports:      map[string]string{"_": "go.opentelemetry.io/otel"},
			expectedNew:      map[string]string{"_": "go.opentelemetry.io/otel"},
			expectedAliases:  map[string]string{"net/http/pprof": "_"},
			expectedExplicit: map[string]bool{"net/http/pprof": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindNew(t.Context(), tt.root, tt.ruleImports)
			assert.Equal(t, tt.expectedNew, result.NewImports)
			assert.Equal(t, tt.expectedAliases, result.ExistingAliases)
			assert.Equal(t, tt.expectedExplicit, result.ExplicitAliases)
		})
	}
}

func TestCollectPaths(t *testing.T) {
	tests := []struct {
		name     string
		root     *dst.File
		expected map[string]string
	}{
		{
			name:     "empty file",
			root:     &dst.File{},
			expected: map[string]string{},
		},
		{
			name: "standard imports",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"log"`}},
						},
					},
				},
			},
			expected: map[string]string{
				"fmt": "fmt",
				"log": "log",
			},
		},
		{
			name: "mixed imports - blank, dot, and normal",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
							&dst.ImportSpec{
								Name: dst.NewIdent("_"),
								Path: &dst.BasicLit{Value: `"net/http/pprof"`},
							},
							&dst.ImportSpec{
								Name: dst.NewIdent("."),
								Path: &dst.BasicLit{Value: `"testing"`},
							},
							&dst.ImportSpec{
								Name: dst.NewIdent("ctx"),
								Path: &dst.BasicLit{Value: `"context"`},
							},
						},
					},
				},
			},
			expected: map[string]string{
				"fmt":            "fmt",
				"net/http/pprof": "net/http/pprof",
				"testing":        "testing",
				"context":        "context",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CollectPaths(t.Context(), tt.root)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolvePackageFiles(t *testing.T) {
	ctx := t.Context()

	// Test with a standard library package
	archives, err := ResolvePackageInfo(ctx, "fmt")
	require.NoError(t, err)

	// Should have fmt and its dependencies
	fmtArchive, exists := archives["fmt"]
	assert.True(t, exists, "fmt should be in the result")
	assert.NotEmpty(t, fmtArchive, "fmt archive path should not be empty")

	// fmt depends on other packages, so we should have more than one
	assert.Greater(t, len(archives), 1, "should have dependencies")

	t.Logf("Resolved %d packages for fmt", len(archives))
	t.Logf("fmt archive: %s", fmtArchive)
}

func TestResolvePackageFiles_InvalidPackage(t *testing.T) {
	ctx := t.Context()

	// Test with a non-existent package
	_, err := ResolvePackageInfo(ctx, "this/package/does/not/exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go list failed")
}

func TestResolvePackageFiles_MultiplePackages(t *testing.T) {
	ctx := t.Context()

	// Test with net/http which has many dependencies
	archives, err := ResolvePackageInfo(ctx, "net/http")
	require.NoError(t, err)

	// Should include net/http itself
	httpArchive, exists := archives["net/http"]
	assert.True(t, exists, "net/http should be in the result")
	assert.NotEmpty(t, httpArchive, "net/http archive path should not be empty")

	// Should include some of its dependencies
	assert.Contains(t, archives, "net")
	assert.Contains(t, archives, "fmt")

	t.Logf("Resolved %d packages for net/http", len(archives))
}

func TestResolvePackageFiles_EmptyOutput(t *testing.T) {
	ctx := t.Context()

	// Test with "unsafe" which has no export archive
	archives, err := ResolvePackageInfo(ctx, "unsafe")
	require.Error(t, err, "unsafe package should not have an export archive")
	assert.Contains(t, err.Error(), "not found in go list output")
	assert.Nil(t, archives)
}
