// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package imports

import (
	"go/token"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			result := Existing(t.Context(), tt.root)
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
			name: "alias already declared in the file",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok:   token.VAR,
						Specs: []dst.Spec{&dst.ValueSpec{Names: []*dst.Ident{dst.NewIdent("traced")}}},
					},
				},
			},
			newImports:  map[string]string{"traced": "fmt"},
			expectError: true,
			errorMsg:    "is already declared in the file",
		},
		{
			name: "method name does not block the alias",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.FuncDecl{
						Recv: &dst.FieldList{List: []*dst.Field{{Type: dst.NewIdent("T")}}},
						Name: dst.NewIdent("traced"),
						Type: &dst.FuncType{},
					},
				},
			},
			newImports: map[string]string{"traced": "fmt"},
			checkResult: func(t *testing.T, root *dst.File) {
				genDecl, ok := root.Decls[0].(*dst.GenDecl)
				require.True(t, ok, "the import declaration must come first")
				require.Len(t, genDecl.Specs, 1)
			},
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
			name: "same path different alias - adds a second import",
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
					useIdent("ctx"),
				},
			},
			newImports: map[string]string{"c": "context"},
			checkResult: func(t *testing.T, root *dst.File) {
				genDecl := root.Decls[0].(*dst.GenDecl)
				require.Len(t, genDecl.Specs, 2)
				for i, alias := range []string{"ctx", "c"} {
					spec := genDecl.Specs[i].(*dst.ImportSpec)
					require.NotNil(t, spec.Name)
					assert.Equal(t, alias, spec.Name.Name)
					assert.Equal(t, `"context"`, spec.Path.Value)
				}
			},
		},
		{
			name: "same path, file's import unused - renamed to the rule's alias",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Name: dst.NewIdent("ctx"), Path: &dst.BasicLit{Value: `"context"`}},
						},
					},
				},
			},
			newImports: map[string]string{"c": "context"},
			checkResult: func(t *testing.T, root *dst.File) {
				assert.Equal(t, []string{"c context"}, specsOf(root))
			},
		},
		{
			name: "same path, unaliased import unused - renamed to the rule's alias",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok:   token.IMPORT,
						Specs: []dst.Spec{&dst.ImportSpec{Path: &dst.BasicLit{Value: `"context"`}}},
					},
				},
			},
			newImports: map[string]string{"c": "context"},
			checkResult: func(t *testing.T, root *dst.File) {
				assert.Equal(t, []string{"c context"}, specsOf(root))
			},
		},
		{
			name: "same path, used import kept and unused import renamed",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Name: dst.NewIdent("x"), Path: &dst.BasicLit{Value: `"context"`}},
							&dst.ImportSpec{Name: dst.NewIdent("y"), Path: &dst.BasicLit{Value: `"context"`}},
						},
					},
					useIdent("x"),
				},
			},
			newImports: map[string]string{"c": "context"},
			checkResult: func(t *testing.T, root *dst.File) {
				assert.Equal(t, []string{"x context", "c context"}, specsOf(root))
			},
		},
		{
			name: "same path, one unused import and two rule aliases - renames once",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Name: dst.NewIdent("ctx"), Path: &dst.BasicLit{Value: `"context"`}},
						},
					},
				},
			},
			newImports: map[string]string{"a": "context", "b": "context"},
			checkResult: func(t *testing.T, root *dst.File) {
				assert.Equal(t, []string{"a context", "b context"}, specsOf(root))
			},
		},
		{
			name: "unused blank import of the same path is never renamed",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Name: dst.NewIdent("_"), Path: &dst.BasicLit{Value: `"context"`}},
						},
					},
				},
			},
			newImports: map[string]string{"c": "context"},
			checkResult: func(t *testing.T, root *dst.File) {
				assert.Equal(t, []string{"_ context", "c context"}, specsOf(root))
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
			err := AddToFile(t.Context(), tt.root, tt.newImports, nil)
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
			expectedNew:      map[string]string{"c": "context"},
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
			expectedNew:      map[string]string{"ctx": "context"},
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
			result := FindNew(t.Context(), tt.root, tt.ruleImports, nil)
			assert.Equal(t, tt.expectedNew, result.NewImports)
			assert.Equal(t, tt.expectedAliases, result.ExistingAliases)
			assert.Equal(t, tt.expectedExplicit, result.ExplicitAliases)
		})
	}
}

func TestFindNew_MultiAliasSets(t *testing.T) {
	// A file may import one path under several aliases. ExistingAliasSets
	// keeps every alias, sorted. ExistingAliases keeps one alias, and the
	// last spec wins there.
	root := &dst.File{
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok: token.IMPORT,
				Specs: []dst.Spec{
					&dst.ImportSpec{Name: dst.NewIdent("a"), Path: &dst.BasicLit{Value: `"fmt"`}},
					&dst.ImportSpec{Name: dst.NewIdent("b"), Path: &dst.BasicLit{Value: `"fmt"`}},
				},
			},
		},
	}

	result := FindNew(t.Context(), root, map[string]string{"a": "fmt"}, nil)

	assert.Equal(t, map[string][]string{"fmt": {"a", "b"}}, result.ExistingAliasSets)
	assert.Equal(t, "b", result.ExistingAliases["fmt"], "the last spec wins in the single-alias view")
	assert.Empty(t, result.NewImports, "alias a already names fmt in the file")
}

func TestCollectImportPaths(t *testing.T) {
	tests := []struct {
		name     string
		root     *dst.File
		expected map[string]string
	}{
		{
			name:     "nil file",
			root:     nil,
			expected: nil,
		},
		{
			name:     "empty file",
			root:     &dst.File{},
			expected: map[string]string{},
		},
		{
			name: "various import styles",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `"fmt"`}},
							&dst.ImportSpec{Name: dst.NewIdent("ctx"), Path: &dst.BasicLit{Value: `"context"`}},
							&dst.ImportSpec{Name: dst.NewIdent("_"), Path: &dst.BasicLit{Value: `"net/http/pprof"`}},
							&dst.ImportSpec{Name: dst.NewIdent("."), Path: &dst.BasicLit{Value: `"testing"`}},
						},
					},
				},
			},
			expected: map[string]string{
				"fmt":            "fmt",
				"context":        "context",
				"net/http/pprof": "net/http/pprof",
				"testing":        "testing",
			},
		},
		{
			name: "non-import decls and edge-case specs",
			root: &dst.File{
				Decls: []dst.Decl{
					&dst.FuncDecl{Name: dst.NewIdent("main")},
					&dst.GenDecl{Tok: token.VAR, Specs: []dst.Spec{&dst.ValueSpec{}}},
					&dst.GenDecl{
						Tok: token.IMPORT,
						Specs: []dst.Spec{
							&dst.ValueSpec{},
							&dst.ImportSpec{Path: nil},
							&dst.ImportSpec{Path: &dst.BasicLit{Value: "invalid_unquoted"}},
							&dst.ImportSpec{Path: &dst.BasicLit{Value: `""`}},
						},
					},
				},
			},
			expected: map[string]string{
				"invalid_unquoted": "invalid_unquoted",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CollectImportPaths(tt.root)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestQualifierNames(t *testing.T) {
	root := parseSource(t, `package p

import "fmt"

var cfg struct{ Name string }

func f(s fmt.Stringer) {
	local := s
	_ = local.String()
	_ = cfg.Name
	_ = other.Value
}
`)

	// cfg and local are declared in the file. other is declared in another
	// file of the package, so the parser leaves it unresolved.
	assert.Equal(t, map[string]bool{"fmt": true, "other": true}, QualifierNames(root))
}

func TestBlankUnused(t *testing.T) {
	resolvedNames := map[string]string{
		"example.com/go-lib": "lib",
		"example.com/lib":    "lib",
		"example.com/wrap":   "wrap",
	}
	tests := []struct {
		name string
		// before is the file before the rules changed it, and after is the
		// file the rules wrote.
		before, after string
		wantSpecs     []string
		wantBlanked   []string
	}{
		{
			name: "aliased import whose only use was removed is blanked",
			before: `package p

import l "example.com/lib"

var _ = l.New()
`,
			after: `package p

import (
	l "example.com/lib"
	"example.com/wrap"
)

var _ = wrap.New()
`,
			wantSpecs:   []string{"_ example.com/lib", " example.com/wrap"},
			wantBlanked: []string{"example.com/lib"},
		},
		{
			name: "unaliased import is blanked under its package name",
			before: `package p

import "example.com/go-lib"

var _ = lib.New()
`,
			after: `package p

import "example.com/go-lib"

var _ = 1
`,
			wantSpecs:   []string{"_ example.com/go-lib"},
			wantBlanked: []string{"example.com/go-lib"},
		},
		{
			name: "import used elsewhere in the file is kept",
			before: `package p

import "example.com/lib"

var _ = lib.New()

var _ lib.Client
`,
			after: `package p

import "example.com/lib"

var _ = 1

var _ lib.Client
`,
			wantSpecs: []string{" example.com/lib"},
		},
		{
			name: "import the file did not use is kept so the compiler still reports it",
			before: `package p

import (
	"example.com/lib"
	"example.com/wrap"
)

// lib.New is only mentioned in a comment.
var _ = wrap.New()
`,
			after: `package p

import (
	"example.com/lib"
	"example.com/wrap"
)

// lib.New is only mentioned in a comment.
var _ = 1
`,
			wantSpecs:   []string{" example.com/lib", "_ example.com/wrap"},
			wantBlanked: []string{"example.com/wrap"},
		},
		{
			name: "import is kept while a local named like it is still used",
			before: `package p

import "example.com/lib"

func f() { lib.New() }

func g(lib fmt.Stringer) { lib.String() }
`,
			after: `package p

import "example.com/lib"

func f() { wrap.New() }

func g(lib fmt.Stringer) { lib.String() }
`,
			wantSpecs: []string{" example.com/lib"},
		},
		{
			// The parser resolves lib in the case list to the guard variable,
			// but the compiler resolves it to the package.
			name: "import used in the case list of a type switch with a guard of its name is kept",
			before: `package p

import "example.com/lib"

var _ = lib.New()

func f(v any) {
	switch lib := v.(type) {
	case *lib.Client:
		_ = lib
	}
}
`,
			after: `package p

import "example.com/lib"

var _ = 1

func f(v any) {
	switch lib := v.(type) {
	case *lib.Client:
		_ = lib
	}
}
`,
			wantSpecs: []string{" example.com/lib"},
		},
		{
			name: "blank, dot and cgo imports are not changed",
			before: `package p

import (
	"C"
	. "example.com/dot"
	l "example.com/lib"
	_ "example.com/side"
)

var _ = C.int(l.New())
`,
			after: `package p

import (
	"C"
	. "example.com/dot"
	l "example.com/lib"
	_ "example.com/side"
)

var _ = 1
`,
			wantSpecs:   []string{" C", ". example.com/dot", "_ example.com/lib", "_ example.com/side"},
			wantBlanked: []string{"example.com/lib"},
		},
		{
			name: "of two imports of one path only the one that lost its use is blanked",
			before: `package p

import (
	a "example.com/lib"
	b "example.com/lib"
)

var _, _ = a.New(), b.New()
`,
			after: `package p

import (
	a "example.com/lib"
	b "example.com/lib"
)

var _, _ = 1, b.New()
`,
			wantSpecs:   []string{"_ example.com/lib", "b example.com/lib"},
			wantBlanked: []string{"example.com/lib"},
		},
		{
			name: "no name lost leaves the file as it is",
			before: `package p

import "example.com/lib"

var _ = lib.New()
`,
			after: `package p

import "example.com/lib"

var _ = lib.New(lib.Option())
`,
			wantSpecs: []string{" example.com/lib"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usedBefore := QualifierNames(parseSource(t, tt.before))
			after := parseSource(t, tt.after)

			blanked := BlankUnused(t.Context(), after, usedBefore, resolvedNames)

			assert.Equal(t, tt.wantBlanked, blanked)
			assert.Equal(t, tt.wantSpecs, specsOf(after))
		})
	}
}

// parseSource parses src with object resolution, as otelc parses the files
// it instruments.
func parseSource(t *testing.T, src string) *dst.File {
	t.Helper()
	root, err := decorator.Parse(src)
	require.NoError(t, err)
	return root
}

// useIdent returns `var _ = name.X`, a declaration that uses name as the
// qualifier of a selector.
func useIdent(name string) dst.Decl {
	return &dst.GenDecl{
		Tok: token.VAR,
		Specs: []dst.Spec{&dst.ValueSpec{
			Names:  []*dst.Ident{dst.NewIdent("_")},
			Values: []dst.Expr{&dst.SelectorExpr{X: dst.NewIdent(name), Sel: dst.NewIdent("X")}},
		}},
	}
}

// specsOf returns every import spec of root as "name path", where name is
// empty for an unaliased import.
func specsOf(root *dst.File) []string {
	var specs []string
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		for _, spec := range genDecl.Specs {
			importSpec := spec.(*dst.ImportSpec)
			name := ""
			if importSpec.Name != nil {
				name = importSpec.Name.Name
			}
			specs = append(specs, name+" "+strings.Trim(importSpec.Path.Value, `"`))
		}
	}
	return specs
}
