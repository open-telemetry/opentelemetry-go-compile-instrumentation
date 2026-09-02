// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTypeName(t *testing.T) {
	tests := []struct {
		input      string
		wantErr    bool
		wantImport string
		wantName   string
		wantPtr    bool
	}{
		{input: "error", wantName: "error"},
		{input: "int", wantName: "int"},
		{input: "float32", wantName: "float32"},
		{input: "any", wantName: "any"},
		{input: "context.Context", wantImport: "context", wantName: "Context"},
		{input: "io.Reader", wantImport: "io", wantName: "Reader"},
		{input: "*http.Request", wantImport: "http", wantName: "Request", wantPtr: true},
		{input: "*T", wantName: "T", wantPtr: true},
		{input: "example.com/pkg.Type", wantImport: "example.com/pkg", wantName: "Type"},
		{input: "", wantErr: true},
		{input: "[]string", wantErr: true},
		{input: "map[string]int", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseTypeName(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantImport, got.importPath)
			assert.Equal(t, tt.wantName, got.name)
			assert.Equal(t, tt.wantPtr, got.pointer)
		})
	}
}

func TestTypeNameMatches(t *testing.T) {
	tests := []struct {
		name    string
		typeStr string
		node    dst.Expr
		want    bool
	}{
		{
			name:    "builtin ident matches",
			typeStr: "error",
			node:    &dst.Ident{Name: "error", Path: ""},
			want:    true,
		},
		{
			name:    "builtin ident mismatch",
			typeStr: "error",
			node:    &dst.Ident{Name: "string", Path: ""},
			want:    false,
		},
		{
			name:    "selector matches",
			typeStr: "context.Context",
			node: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "context", Path: ""},
				Sel: &dst.Ident{Name: "Context"},
			},
			want: true,
		},
		{
			name:    "selector package mismatch",
			typeStr: "io.Reader",
			node: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "context", Path: ""},
				Sel: &dst.Ident{Name: "Reader"},
			},
			want: false,
		},
		{
			name:    "pointer matches",
			typeStr: "*http.Request",
			node: &dst.StarExpr{
				X: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "http", Path: ""},
					Sel: &dst.Ident{Name: "Request"},
				},
			},
			want: true,
		},
		{
			name:    "pointer type does not match non-pointer",
			typeStr: "*T",
			node:    &dst.Ident{Name: "T", Path: ""},
			want:    false,
		},
		{
			name:    "non-pointer does not match pointer",
			typeStr: "T",
			node:    &dst.StarExpr{X: &dst.Ident{Name: "T", Path: ""}},
			want:    false,
		},
		{
			name:    "any matches empty interface",
			typeStr: "any",
			node:    &dst.InterfaceType{Methods: &dst.FieldList{}},
			want:    true,
		},
		{
			name:    "single type param matches",
			typeStr: "context.Context",
			node: &dst.IndexExpr{
				X:     &dst.SelectorExpr{X: &dst.Ident{Name: "context"}, Sel: &dst.Ident{Name: "Context"}},
				Index: &dst.Ident{Name: "T"},
			},
			want: true,
		},
		{
			name:    "multiple type params matches",
			typeStr: "Map",
			node: &dst.IndexListExpr{
				X:       &dst.Ident{Name: "Map"},
				Indices: []dst.Expr{&dst.Ident{Name: "K"}, &dst.Ident{Name: "V"}},
			},
			want: true,
		},
		{
			name:    "unsupported array type node returns false without panic",
			typeStr: "string",
			node:    &dst.ArrayType{Elt: &dst.Ident{Name: "string"}},
			want:    false,
		},
		{
			name:    "unsupported map type node returns false without panic",
			typeStr: "string",
			node:    &dst.MapType{Key: &dst.Ident{Name: "string"}, Value: &dst.Ident{Name: "int"}},
			want:    false,
		},
		{
			name:    "unsupported chan type node returns false without panic",
			typeStr: "int",
			node:    &dst.ChanType{Value: &dst.Ident{Name: "int"}},
			want:    false,
		},
		{
			name:    "unsupported func type node returns false without panic",
			typeStr: "error",
			node:    &dst.FuncType{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tn, err := parseTypeName(tt.typeStr)
			require.NoError(t, err)
			assert.Equal(t, tt.want, tn.matches(tt.node, nil))
		})
	}
}

// If ident.Path is populated, it should be used to match the type instead
// of the import path map.
func TestTypeNameMatches_PreResolvedIdentPath(t *testing.T) {
	node := &dst.SelectorExpr{
		X:   &dst.Ident{Name: "http", Path: "net/http"},
		Sel: &dst.Ident{Name: "Request"},
	}
	tn, err := parseTypeName("net/http.Request")
	require.NoError(t, err)

	t.Run("matches via ident.Path", func(t *testing.T) {
		imports := map[string]string{"http": "example.com/other/http"}
		assert.True(t, tn.matches(node, imports))
	})

	t.Run("mismatched import path does not match", func(t *testing.T) {
		other, parseErr := parseTypeName("example.com/other/http.Request")
		require.NoError(t, parseErr)
		assert.False(t, other.matches(node, nil))
	})
}

func TestTypeNameMatches_FullyQualifiedMultiSegmentImport(t *testing.T) {
	node := &dst.StarExpr{
		X: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "http"},
			Sel: &dst.Ident{Name: "Request"},
		},
	}

	tn, err := parseTypeName("*net/http.Request")
	require.NoError(t, err)

	t.Run("no import context falls back to path-tail match", func(t *testing.T) {
		assert.True(t, tn.matches(node, nil))
	})

	t.Run("resolved import context matches", func(t *testing.T) {
		imports := map[string]string{"http": "net/http"}
		assert.True(t, tn.matches(node, imports))
	})

	t.Run("import context resolving to a different package does not match", func(t *testing.T) {
		// "http" is aliased to an unrelated package in this (contrived) file;
		// the real import path wins over the bare identifier text.
		imports := map[string]string{"http": "example.com/other/http"}
		assert.False(t, tn.matches(node, imports))
	})
}

// TestTypeNameMatches_ImportAliasResolution covers cases the path-tail
// fallback cannot handle correctly on its own: an explicit import alias, and
// two distinct import paths that share a last path segment.
func TestTypeNameMatches_ImportAliasResolution(t *testing.T) {
	t.Run("explicit alias resolves correctly", func(t *testing.T) {
		// import althttp "net/http"; func f(r *althttp.Request)
		node := &dst.StarExpr{
			X: &dst.SelectorExpr{X: &dst.Ident{Name: "althttp"}, Sel: &dst.Ident{Name: "Request"}},
		}
		tn, err := parseTypeName("*net/http.Request")
		require.NoError(t, err)

		imports := map[string]string{"althttp": "net/http"}
		assert.True(t, tn.matches(node, imports))

		// Without import context, the tail fallback can't know "althttp" means
		// net/http, so it correctly declines to match rather than guessing.
		assert.False(t, tn.matches(node, nil))
	})

	t.Run("colliding last path segments are disambiguated", func(t *testing.T) {
		// Both html/template and text/template default to the local name
		// "template"; only the file's actual import decides which one a
		// "template.Template" reference in that file means.
		node := &dst.SelectorExpr{X: &dst.Ident{Name: "template"}, Sel: &dst.Ident{Name: "Template"}}

		textTemplate, err := parseTypeName("text/template.Template")
		require.NoError(t, err)
		htmlTemplate, err := parseTypeName("html/template.Template")
		require.NoError(t, err)

		importsText := map[string]string{"template": "text/template"}
		assert.True(t, textTemplate.matches(node, importsText))
		assert.False(t, htmlTemplate.matches(node, importsText))

		importsHTML := map[string]string{"template": "html/template"}
		assert.False(t, textTemplate.matches(node, importsHTML))
		assert.True(t, htmlTemplate.matches(node, importsHTML))
	})
}

func TestImportAliasMap(t *testing.T) {
	t.Run("nil file returns nil", func(t *testing.T) {
		assert.Nil(t, ImportAliasMap(nil))
	})

	t.Run("resolves default and aliased imports, skips blank and dot imports", func(t *testing.T) {
		p := NewAstParser()
		file, err := p.ParseSource(`package main

import (
	"net/http"
	althttp "net/http"
	_ "unsafe"
	. "fmt"
)

func f(r *http.Request, r2 *althttp.Request) {}
`)
		require.NoError(t, err)

		imports := ImportAliasMap(file)
		assert.Len(t, imports, 2)
		assert.Equal(t, "net/http", imports["http"])
		assert.Equal(t, "net/http", imports["althttp"])
		assert.NotContains(t, imports, "_")
		assert.NotContains(t, imports, ".")
	})

	t.Run("module version suffix is not the package name", func(t *testing.T) {
		p := NewAstParser()
		file, err := p.ParseSource(`package main

import (
	"github.com/golang-jwt/jwt/v5"
	"gopkg.in/yaml.v3"
)

func f(t *jwt.Token, n *yaml.Node) {}
`)
		require.NoError(t, err)

		imports := ImportAliasMap(file)
		assert.Equal(t, "github.com/golang-jwt/jwt/v5", imports["jwt"])
		assert.Equal(t, "gopkg.in/yaml.v3", imports["yaml"])
	})

	t.Run("distinct modules sharing a version suffix do not collide", func(t *testing.T) {
		p := NewAstParser()
		file, err := p.ParseSource(`package main

import (
	"github.com/a/foo/v2"
	"github.com/b/bar/v2"
)

func f(x *foo.T, y *bar.T) {}
`)
		require.NoError(t, err)

		imports := ImportAliasMap(file)
		assert.Len(t, imports, 2)
		assert.Equal(t, "github.com/a/foo/v2", imports["foo"])
		assert.Equal(t, "github.com/b/bar/v2", imports["bar"])
	})

	t.Run("package name unrelated to import path is not resolved by convention", func(t *testing.T) {
		// github.com/redis/go-redis/v9 declares package "redis", not "go-redis".
		p := NewAstParser()
		file, err := p.ParseSource(`package main

import "github.com/redis/go-redis/v9"

func f(c *redis.Client) {}
`)
		require.NoError(t, err)

		imports := ImportAliasMap(file)
		assert.NotContains(t, imports, "redis")
		assert.Equal(t, "github.com/redis/go-redis/v9", imports["go-redis"])
	})

	t.Run("v-prefixed last segment that is not a version suffix is preserved", func(t *testing.T) {
		// "vault" starts with 'v' like a version suffix, but the rest isn't
		// digits, so it must be treated as the package name, not stripped.
		p := NewAstParser()
		file, err := p.ParseSource(`package main

import "github.com/hashicorp/vault"

func f(c *vault.Client) {}
`)
		require.NoError(t, err)

		imports := ImportAliasMap(file)
		assert.Equal(t, "github.com/hashicorp/vault", imports["vault"])
	})

	t.Run("import spec with a nil path is skipped", func(t *testing.T) {
		file := &dst.File{Imports: []*dst.ImportSpec{
			{Path: nil},
			{Path: &dst.BasicLit{Value: `"net/http"`}},
		}}

		imports := ImportAliasMap(file)
		assert.Len(t, imports, 1)
		assert.Equal(t, "net/http", imports["http"])
	})

	t.Run("import spec with an unquotable path is skipped", func(t *testing.T) {
		file := &dst.File{Imports: []*dst.ImportSpec{
			{Path: &dst.BasicLit{Value: "not-a-quoted-string"}},
			{Path: &dst.BasicLit{Value: `"net/http"`}},
		}}

		imports := ImportAliasMap(file)
		assert.Len(t, imports, 1)
		assert.Equal(t, "net/http", imports["http"])
	})

	t.Run("resolves imports from Decls when Imports slice is empty", func(t *testing.T) {
		file := &dst.File{
			Decls: []dst.Decl{
				&dst.GenDecl{
					Tok: token.IMPORT,
					Specs: []dst.Spec{
						&dst.ImportSpec{
							Path: &dst.BasicLit{Value: `"net/http"`},
						},
						&dst.ImportSpec{
							Name: &dst.Ident{Name: "althttp"},
							Path: &dst.BasicLit{Value: `"net/http"`},
						},
					},
				},
				&dst.GenDecl{
					Tok: token.VAR,
				},
			},
		}

		imports := ImportAliasMap(file)
		assert.Len(t, imports, 2)
		assert.Equal(t, "net/http", imports["http"])
		assert.Equal(t, "net/http", imports["althttp"])
	})

	t.Run("Imports takes precedence when both Imports and Decls contain specs", func(t *testing.T) {
		file := &dst.File{
			Imports: []*dst.ImportSpec{
				{Path: &dst.BasicLit{Value: `"net/http"`}},
			},
			Decls: []dst.Decl{
				&dst.GenDecl{
					Tok: token.IMPORT,
					Specs: []dst.Spec{
						&dst.ImportSpec{
							Name: &dst.Ident{Name: "ignored"},
							Path: &dst.BasicLit{Value: `"other/pkg"`},
						},
					},
				},
			},
		}

		imports := ImportAliasMap(file)
		assert.Len(t, imports, 1)
		assert.Equal(t, "net/http", imports["http"])
		assert.NotContains(t, imports, "ignored")
	})
}

func TestDefaultImportAlias(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "net/http", want: "http"},
		{input: "fmt", want: "fmt"},
		{input: "github.com/golang-jwt/jwt/v5", want: "jwt"},
		{input: "github.com/example/pkg/v2", want: "pkg"},
		{input: "gopkg.in/yaml.v3", want: "yaml"},
		{input: "gopkg.in/go-playground/validator.v9", want: "validator"},
		{input: "github.com/hashicorp/vault", want: "vault"},
		{input: "github.com/redis/go-redis/v9", want: "go-redis"},
		{input: "example.com/foo/v10", want: "foo"},
		{input: "yaml.v2", want: "yaml"},
		{input: "v2", want: "v2"},
		{input: "./v2", want: ""},
		{input: "/v2", want: ""},
		{input: "", want: ""},
		{input: ".", want: ""},
		{input: "/", want: ""},
		{input: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := defaultImportAlias(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func mustContains(t *testing.T, fields *dst.FieldList, typeStr string) bool {
	t.Helper()
	ok, err := fieldListContainsType(fields, typeStr, nil)
	require.NoError(t, err)
	return ok
}

func TestFieldListContainsType(t *testing.T) {
	fields := &dst.FieldList{
		List: []*dst.Field{
			{Type: &dst.Ident{Name: "string"}},
			{
				Type: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "context"},
					Sel: &dst.Ident{Name: "Context"},
				},
			},
			{Type: &dst.Ident{Name: "error"}},
			{Type: &dst.ArrayType{Elt: &dst.Ident{Name: "byte"}}},
			{Type: &dst.MapType{Key: &dst.Ident{Name: "string"}, Value: &dst.Ident{Name: "string"}}},
		},
	}

	assert.True(t, mustContains(t, fields, "string"))
	assert.True(t, mustContains(t, fields, "context.Context"))
	assert.True(t, mustContains(t, fields, "error"))
	assert.False(t, mustContains(t, fields, "int"))
	assert.False(t, mustContains(t, fields, "io.Reader"))
	assert.False(t, mustContains(t, nil, "error"))
	assert.False(t, mustContains(t, &dst.FieldList{}, "error"))

	_, err := fieldListContainsType(fields, "[]invalid", nil)
	assert.Error(t, err)
}

func TestMatchesTypeName(t *testing.T) {
	ctxType := &dst.SelectorExpr{
		X:   &dst.Ident{Name: "context"},
		Sel: &dst.Ident{Name: "Context"},
	}

	matched, err := MatchesTypeName(ctxType, "context.Context", nil)
	require.NoError(t, err)
	assert.True(t, matched)

	matched, err = MatchesTypeName(ctxType, "io.Reader", nil)
	require.NoError(t, err)
	assert.False(t, matched)

	_, err = MatchesTypeName(ctxType, "[]invalid", nil)
	assert.Error(t, err)
}

func TestMatchesTypeName_UnsupportedNodeDoesNotMatch(t *testing.T) {
	sliceType := &dst.ArrayType{Elt: &dst.Ident{Name: "byte"}}

	matched, err := MatchesTypeName(sliceType, "context.Context", nil)
	require.NoError(t, err)
	assert.False(t, matched)
}
