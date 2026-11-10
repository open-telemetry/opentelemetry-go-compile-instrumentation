// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// fixture source used across all shared_test cases
const sharedTestSource = `package main

var GlobalVar = "original"

const MaxRetries = 3

type MyStruct struct{ x int }

type GenSingle[T any] struct{ val T }

type GenMulti[T, U any] struct{ t T; u U }

func TopLevel(a, b int) int { return a + b }

func (s *MyStruct) Method() error { return nil }

func (g GenSingle[T]) SingleVal() T { return g.val }

func (g *GenSingle[T]) SinglePtr() T { return g.val }

func (g GenMulti[T, U]) MultiVal() (T, U) { return g.t, g.u }

func (g *GenMulti[T, U]) MultiPtr() (T, U) { return g.t, g.u }
`

func parseSharedFixture(t *testing.T) *dst.File {
	t.Helper()
	p := NewAstParser()
	file, err := p.ParseSource(sharedTestSource)
	require.NoError(t, err)
	return file
}

func TestListFuncDecls(t *testing.T) {
	file := parseSharedFixture(t)
	decls := listFuncDecls(file)
	require.Len(t, decls, 6)
	names := make([]string, 0, len(decls))
	for _, decl := range decls {
		names = append(names, decl.Name.Name)
	}
	assert.Contains(t, names, "TopLevel")
	assert.Contains(t, names, "Method")
	assert.Contains(t, names, "SingleVal")
	assert.Contains(t, names, "SinglePtr")
	assert.Contains(t, names, "MultiVal")
	assert.Contains(t, names, "MultiPtr")
}

func TestFindFuncDeclWithoutRecv(t *testing.T) {
	file := parseSharedFixture(t)

	t.Run("finds top-level func", func(t *testing.T) {
		fn := FindFuncDeclWithoutRecv(file, "TopLevel")
		require.NotNil(t, fn)
		assert.Equal(t, "TopLevel", fn.Name.Name)
	})

	t.Run("ignores method with same name", func(t *testing.T) {
		// "Method" has a receiver, so it should not be found
		fn := FindFuncDeclWithoutRecv(file, "Method")
		assert.Nil(t, fn)
	})

	t.Run("not found returns nil", func(t *testing.T) {
		fn := FindFuncDeclWithoutRecv(file, "NonExistent")
		assert.Nil(t, fn)
	})
}

func TestFindFuncDeclForRule(t *testing.T) {
	file := parseSharedFixture(t)

	t.Run("matches name receiver and signature filters", func(t *testing.T) {
		sig := rule.FuncSignature{Returns: []string{"error"}}
		r := &rule.InstFuncRule{
			Func:      "Method",
			Recv:      "*MyStruct",
			Signature: &sig,
		}

		fn, ok, err := FindFuncDecl(file, r, nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, fn)
		assert.Equal(t, "Method", fn.Name.Name)
	})

	t.Run("matches single param generic value receiver", func(t *testing.T) {
		r := &rule.InstFuncRule{
			Func: "SingleVal",
			Recv: "GenSingle",
		}
		fn, ok, err := FindFuncDecl(file, r, nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, fn)
		assert.Equal(t, "SingleVal", fn.Name.Name)
	})

	t.Run("matches single param generic pointer receiver", func(t *testing.T) {
		r := &rule.InstFuncRule{
			Func: "SinglePtr",
			Recv: "*GenSingle",
		}
		fn, ok, err := FindFuncDecl(file, r, nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, fn)
		assert.Equal(t, "SinglePtr", fn.Name.Name)
	})

	t.Run("matches multi param generic value receiver", func(t *testing.T) {
		r := &rule.InstFuncRule{
			Func: "MultiVal",
			Recv: "GenMulti",
		}
		fn, ok, err := FindFuncDecl(file, r, nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, fn)
		assert.Equal(t, "MultiVal", fn.Name.Name)
	})

	t.Run("matches multi param generic pointer receiver", func(t *testing.T) {
		r := &rule.InstFuncRule{
			Func: "MultiPtr",
			Recv: "*GenMulti",
		}
		fn, ok, err := FindFuncDecl(file, r, nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, fn)
		assert.Equal(t, "MultiPtr", fn.Name.Name)
	})

	t.Run("matches InstRawRule", func(t *testing.T) {
		r := &rule.InstRawRule{
			Func: "Method",
			Recv: "*MyStruct",
		}
		fn, ok, err := FindFuncDecl(file, r, nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, fn)
		assert.Equal(t, "Method", fn.Name.Name)
	})

	t.Run("matches FilterDef", func(t *testing.T) {
		r := &rule.FilterDef{
			HasFunc: "Method",
			HasRecv: "*MyStruct",
		}
		fn, ok, err := FindFuncDecl(file, r, nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotNil(t, fn)
		assert.Equal(t, "Method", fn.Name.Name)
	})

	t.Run("returns nil when signature filters do not match", func(t *testing.T) {
		sig := rule.FuncSignature{Args: []string{"string"}}
		r := &rule.InstFuncRule{
			Func:      "TopLevel",
			Signature: &sig,
		}

		fn, ok, err := FindFuncDecl(file, r, nil)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Nil(t, fn)
	})

	t.Run("returns error for invalid filter type", func(t *testing.T) {
		r := &rule.InstFuncRule{
			Func:  "TopLevel",
			Param: "[]invalid",
		}

		fn, ok, err := FindFuncDecl(file, r, nil)
		require.Error(t, err)
		assert.False(t, ok)
		assert.Nil(t, fn)
	})
}

// TestFindFuncDeclForRule_QualifiedParamType is an end-to-end regression test
// for the bug where a where.param/where.result filter naming a type from a
// multi-segment import path (e.g. "*net/http.Request", as opposed to a
// single-segment stdlib package like "io" or "context") never matched,
// because the matcher compared the bare package identifier written at the
// call site ("http") against the filter's full import path ("net/http")
// instead of resolving it via the file's own imports. This silently broke any
// rule targeting a handler-shaped function such as
// func(w http.ResponseWriter, r *http.Request).
func TestFindFuncDeclForRule_QualifiedParamType(t *testing.T) {
	p := NewAstParser()
	file, err := p.ParseSource(`package main

import "net/http"

func handleRoot(w http.ResponseWriter, r *http.Request) {}
`)
	require.NoError(t, err)

	r := &rule.InstFuncRule{
		Func:  "handleRoot",
		Param: "*net/http.Request",
	}

	fn, ok, err := FindFuncDecl(file, r, nil)
	require.NoError(t, err)
	require.True(t, ok, `where.param: "*net/http.Request" should match a *http.Request parameter`)
	require.NotNil(t, fn)
	assert.Equal(t, "handleRoot", fn.Name.Name)
}

func TestFindVarDecl(t *testing.T) {
	file := parseSharedFixture(t)

	t.Run("finds existing var", func(t *testing.T) {
		genDecl, spec := findVarDecl(file, "GlobalVar")
		require.NotNil(t, genDecl)
		require.NotNil(t, spec)
		assert.Equal(t, "GlobalVar", spec.Names[0].Name)
	})

	t.Run("does not find const as var", func(t *testing.T) {
		genDecl, spec := findVarDecl(file, "MaxRetries")
		assert.Nil(t, genDecl)
		assert.Nil(t, spec)
	})

	t.Run("not found returns nil pair", func(t *testing.T) {
		genDecl, spec := findVarDecl(file, "Unknown")
		assert.Nil(t, genDecl)
		assert.Nil(t, spec)
	})
}

func TestFindConstDecl(t *testing.T) {
	file := parseSharedFixture(t)

	t.Run("finds existing const", func(t *testing.T) {
		genDecl, spec := findConstDecl(file, "MaxRetries")
		require.NotNil(t, genDecl)
		require.NotNil(t, spec)
		assert.Equal(t, "MaxRetries", spec.Names[0].Name)
	})

	t.Run("does not find var as const", func(t *testing.T) {
		genDecl, spec := findConstDecl(file, "GlobalVar")
		assert.Nil(t, genDecl)
		assert.Nil(t, spec)
	})

	t.Run("not found returns nil pair", func(t *testing.T) {
		genDecl, spec := findConstDecl(file, "Unknown")
		assert.Nil(t, genDecl)
		assert.Nil(t, spec)
	})
}

func TestFindTypeDecl(t *testing.T) {
	file := parseSharedFixture(t)

	t.Run("finds existing type", func(t *testing.T) {
		decl := findTypeDecl(file, "MyStruct")
		require.NotNil(t, decl)
	})

	t.Run("not found returns nil", func(t *testing.T) {
		decl := findTypeDecl(file, "NoSuchType")
		assert.Nil(t, decl)
	})
}

func TestFindNamedDecl(t *testing.T) {
	file := parseSharedFixture(t)

	t.Run("kind func finds top-level function", func(t *testing.T) {
		node := FindNamedDecl(file, "TopLevel", "func")
		require.NotNil(t, node)
		fn, ok := node.(*dst.FuncDecl)
		require.True(t, ok)
		assert.Equal(t, "TopLevel", fn.Name.Name)
	})

	t.Run("kind var finds variable", func(t *testing.T) {
		node := FindNamedDecl(file, "GlobalVar", "var")
		require.NotNil(t, node)
		_, ok := node.(*dst.ValueSpec)
		assert.True(t, ok)
	})

	t.Run("kind const finds constant", func(t *testing.T) {
		node := FindNamedDecl(file, "MaxRetries", "const")
		require.NotNil(t, node)
		_, ok := node.(*dst.ValueSpec)
		assert.True(t, ok)
	})

	t.Run("kind type finds type declaration", func(t *testing.T) {
		node := FindNamedDecl(file, "MyStruct", "type")
		require.NotNil(t, node)
		_, ok := node.(*dst.GenDecl)
		assert.True(t, ok)
	})

	t.Run("empty kind matches first found (func)", func(t *testing.T) {
		node := FindNamedDecl(file, "TopLevel", "")
		require.NotNil(t, node)
	})

	t.Run("empty kind matches var when no func matches", func(t *testing.T) {
		node := FindNamedDecl(file, "GlobalVar", "")
		require.NotNil(t, node)
	})

	t.Run("empty kind matches const", func(t *testing.T) {
		node := FindNamedDecl(file, "MaxRetries", "")
		require.NotNil(t, node)
	})

	t.Run("empty kind matches type", func(t *testing.T) {
		node := FindNamedDecl(file, "MyStruct", "")
		require.NotNil(t, node)
	})

	t.Run("not found returns nil", func(t *testing.T) {
		node := FindNamedDecl(file, "NonExistent", "")
		assert.Nil(t, node)
	})

	t.Run("wrong kind returns nil", func(t *testing.T) {
		// GlobalVar is a var, not a const
		node := FindNamedDecl(file, "GlobalVar", "const")
		assert.Nil(t, node)
	})
}

func TestFindStructType(t *testing.T) {
	t.Run("finds a plain struct", func(t *testing.T) {
		assert.NotNil(t, FindStructType(parseSharedFixture(t), "MyStruct"))
	})

	t.Run("nil for interface, alias, or missing type", func(t *testing.T) {
		src, err := NewAstParser().ParseSource(`package main
type Iface interface{ M() }
type Alias = int
type Plain struct{ x int }
`)
		require.NoError(t, err)
		assert.Nil(t, FindStructType(src, "Iface"))
		assert.Nil(t, FindStructType(src, "Alias"))
		assert.Nil(t, FindStructType(src, "Nope"))
		assert.NotNil(t, FindStructType(src, "Plain"))
	})

	t.Run("resolves the named struct in a grouped type block", func(t *testing.T) {
		src, err := NewAstParser().ParseSource(`package main
type (
	First  = int
	Second struct{ a int }
)
`)
		require.NoError(t, err)
		assert.Nil(t, FindStructType(src, "First"))
		assert.NotNil(t, FindStructType(src, "Second"))
	})

	t.Run("resolves generic structs", func(t *testing.T) {
		src, err := NewAstParser().ParseSource(`package main
type Gen[T any] struct{ v T }
type GenMulti[K comparable, V any] struct{ m map[K]V }
`)
		require.NoError(t, err)
		assert.NotNil(t, FindStructType(src, "Gen"))
		assert.NotNil(t, FindStructType(src, "GenMulti"))
	})
}

// makeFuncDecl builds a minimal *dst.FuncDecl for testing.
func makeFuncDecl(params, results []*dst.Field) *dst.FuncDecl {
	ft := &dst.FuncType{}
	if len(params) > 0 {
		ft.Params = &dst.FieldList{List: params}
	}
	if len(results) > 0 {
		ft.Results = &dst.FieldList{List: results}
	}
	return &dst.FuncDecl{
		Name: dst.NewIdent("TestFunc"),
		Type: ft,
		Body: &dst.BlockStmt{},
	}
}

func field(typeExpr dst.Expr) *dst.Field {
	return &dst.Field{Type: typeExpr}
}

func ident(name string) *dst.Ident { return &dst.Ident{Name: name} }

func selector(pkg, name string) *dst.SelectorExpr {
	return &dst.SelectorExpr{X: ident(pkg), Sel: ident(name)}
}

// mustMatch matches decl against r with no import context (nil), i.e. as if
// decl's enclosing file were unavailable. Type filters resolve via the
// import-path-tail fallback in this mode; see TestFuncDeclMatchesFilters_ImportAliasResolution
// for matching with a real *dst.File's import declarations.
func mustMatch(t *testing.T, decl *dst.FuncDecl, r *rule.InstFuncRule) bool {
	t.Helper()
	ok, err := funcDeclMatchesFilters(decl, r, nil, nil)
	require.NoError(t, err)
	return ok
}

func TestFuncDeclMatchesFilters_NoFilters(t *testing.T) {
	decl := makeFuncDecl(
		[]*dst.Field{field(ident("string"))},
		[]*dst.Field{field(ident("error"))},
	)
	r := &rule.InstFuncRule{}
	assert.True(t, mustMatch(t, decl, r), "no filters should always match")
}

func TestFuncDeclMatchesFilters_ExactSignature(t *testing.T) {
	// func(string, int) (float32, error)
	decl := makeFuncDecl(
		[]*dst.Field{field(ident("string")), field(ident("int"))},
		[]*dst.Field{field(ident("float32")), field(ident("error"))},
	)

	tests := []struct {
		name string
		sig  rule.FuncSignature
		want bool
	}{
		{
			name: "exact match",
			sig:  rule.FuncSignature{Args: []string{"string", "int"}, Returns: []string{"float32", "error"}},
			want: true,
		},
		{
			name: "wrong arg type",
			sig:  rule.FuncSignature{Args: []string{"int", "string"}, Returns: []string{"float32", "error"}},
			want: false,
		},
		{
			name: "wrong arg count",
			sig:  rule.FuncSignature{Args: []string{"string"}, Returns: []string{"float32", "error"}},
			want: false,
		},
		{
			name: "wrong return type",
			sig:  rule.FuncSignature{Args: []string{"string", "int"}, Returns: []string{"error"}},
			want: false,
		},
		{
			name: "no args filter only checks returns",
			sig:  rule.FuncSignature{Returns: []string{"float32", "error"}},
			want: false, // sig.Args==nil means 0 expected, but decl has 2 params
		},
		{
			name: "args only, no returns check",
			sig:  rule.FuncSignature{Args: []string{"string", "int"}},
			want: false, // sig.Returns==nil means 0 expected, but decl has 2 results
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := tt.sig
			r := &rule.InstFuncRule{Signature: &sig}
			assert.Equal(t, tt.want, mustMatch(t, decl, r))
		})
	}
}

func TestFuncDeclMatchesFilters_SignatureContains(t *testing.T) {
	// func(context.Context, string) error
	decl := makeFuncDecl(
		[]*dst.Field{field(selector("context", "Context")), field(ident("string"))},
		[]*dst.Field{field(ident("error"))},
	)

	tests := []struct {
		name string
		sig  rule.FuncSignature
		want bool
	}{
		{
			name: "arg match triggers true",
			sig:  rule.FuncSignature{Args: []string{"context.Context"}},
			want: true,
		},
		{
			name: "return match triggers true",
			sig:  rule.FuncSignature{Returns: []string{"error"}},
			want: true,
		},
		{
			name: "no match",
			sig:  rule.FuncSignature{Args: []string{"int"}, Returns: []string{"bool"}},
			want: false,
		},
		{
			name: "second arg matches",
			sig:  rule.FuncSignature{Args: []string{"string"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := tt.sig
			r := &rule.InstFuncRule{SignatureContains: &sig}
			assert.Equal(t, tt.want, mustMatch(t, decl, r))
		})
	}
}

func TestFuncDeclMatchesFilters_Result(t *testing.T) {
	// func() (io.Reader, error)
	decl := makeFuncDecl(
		nil,
		[]*dst.Field{field(selector("io", "Reader")), field(ident("error"))},
	)

	assert.True(t, mustMatch(t, decl, &rule.InstFuncRule{Result: "error"}))
	assert.True(t, mustMatch(t, decl, &rule.InstFuncRule{Result: "io.Reader"}))
	assert.False(t, mustMatch(t, decl, &rule.InstFuncRule{Result: "io.Writer"}))
	assert.False(t, mustMatch(t, decl, &rule.InstFuncRule{Result: "string"}))
}

func TestFuncDeclMatchesFilters_LastResult(t *testing.T) {
	// func() (io.Reader, error)
	decl := makeFuncDecl(
		nil,
		[]*dst.Field{field(selector("io", "Reader")), field(ident("error"))},
	)

	// error is the final result
	assert.True(t, mustMatch(t, decl, &rule.InstFuncRule{LastResult: "error"}))
	// io.Reader is NOT the final result
	assert.False(t, mustMatch(t, decl, &rule.InstFuncRule{LastResult: "io.Reader"}))
}

func TestFuncDeclMatchesFilters_Param(t *testing.T) {
	// func(context.Context, string) error
	decl := makeFuncDecl(
		[]*dst.Field{field(selector("context", "Context")), field(ident("string"))},
		[]*dst.Field{field(ident("error"))},
	)

	assert.True(t, mustMatch(t, decl, &rule.InstFuncRule{Param: "context.Context"}))
	assert.True(t, mustMatch(t, decl, &rule.InstFuncRule{Param: "string"}))
	assert.False(t, mustMatch(t, decl, &rule.InstFuncRule{Param: "int"}))
}

// TestFuncDeclMatchesFilters_CompositeTypes locks the documented no-match
// behavior for composite parameter and result types: those nodes never
// satisfy a type-name filter.
// "Contains" applies to the parameter/result list, not to nested element
// types, so args: [string] does not match []string.
func TestFuncDeclMatchesFilters_CompositeTypes(t *testing.T) {
	chanDecl := makeFuncDecl(
		[]*dst.Field{field(&dst.ChanType{Value: ident("int")})},
		[]*dst.Field{field(ident("error"))},
	)
	readDecl := makeFuncDecl(
		[]*dst.Field{field(&dst.ArrayType{Elt: ident("byte")})},
		[]*dst.Field{field(ident("int")), field(ident("error"))},
	)
	mixedDecl := makeFuncDecl(
		[]*dst.Field{field(ident("string")), field(&dst.ArrayType{Elt: ident("byte")})},
		[]*dst.Field{field(ident("error"))},
	)
	mapDecl := makeFuncDecl(
		[]*dst.Field{field(&dst.MapType{Key: ident("string"), Value: ident("int")})},
		nil,
	)
	funcDecl := makeFuncDecl(
		[]*dst.Field{field(&dst.FuncType{
			Results: &dst.FieldList{List: []*dst.Field{field(ident("error"))}},
		})},
		nil,
	)
	variadicDecl := makeFuncDecl(
		[]*dst.Field{field(&dst.Ellipsis{Elt: ident("string")})},
		nil,
	)
	ptrSliceDecl := makeFuncDecl(
		[]*dst.Field{field(&dst.StarExpr{X: &dst.ArrayType{Elt: ident("byte")}})},
		[]*dst.Field{field(&dst.ArrayType{Elt: ident("error")})},
	)

	tests := []struct {
		name string
		decl *dst.FuncDecl
		rule *rule.InstFuncRule
		want bool
	}{
		{
			name: "signature_contains does not match chan element type",
			decl: chanDecl,
			rule: &rule.InstFuncRule{SignatureContains: &rule.FuncSignature{Args: []string{"int"}}},
			want: false,
		},
		{
			name: "signature_contains still matches a simple result beside a composite param",
			decl: chanDecl,
			rule: &rule.InstFuncRule{SignatureContains: &rule.FuncSignature{Returns: []string{"error"}}},
			want: true,
		},
		{
			name: "signature_contains does not match slice element type",
			decl: readDecl,
			rule: &rule.InstFuncRule{SignatureContains: &rule.FuncSignature{Args: []string{"byte"}}},
			want: false,
		},
		{
			name: "param does not match slice element type",
			decl: readDecl,
			rule: &rule.InstFuncRule{Param: "byte"},
			want: false,
		},
		{
			name: "last_result still matches when an earlier param is composite",
			decl: readDecl,
			rule: &rule.InstFuncRule{LastResult: "error"},
			want: true,
		},
		{
			name: "result still matches when an earlier param is composite",
			decl: readDecl,
			rule: &rule.InstFuncRule{Result: "int"},
			want: true,
		},
		{
			name: "exact signature does not treat a composite slot as its element type",
			decl: readDecl,
			rule: &rule.InstFuncRule{Signature: &rule.FuncSignature{
				Args:    []string{"byte"},
				Returns: []string{"int", "error"},
			}},
			want: false,
		},
		{
			name: "param matches a simple type in a mixed signature",
			decl: mixedDecl,
			rule: &rule.InstFuncRule{Param: "string"},
			want: true,
		},
		{
			name: "signature_contains matches a simple type in a mixed signature",
			decl: mixedDecl,
			rule: &rule.InstFuncRule{SignatureContains: &rule.FuncSignature{Args: []string{"string"}}},
			want: true,
		},
		{
			name: "signature_contains does not match map key or value",
			decl: mapDecl,
			rule: &rule.InstFuncRule{SignatureContains: &rule.FuncSignature{Args: []string{"string"}}},
			want: false,
		},
		{
			name: "param does not match a func type's result",
			decl: funcDecl,
			rule: &rule.InstFuncRule{Param: "error"},
			want: false,
		},
		{
			name: "param does not match a variadic element type",
			decl: variadicDecl,
			rule: &rule.InstFuncRule{Param: "string"},
			want: false,
		},
		{
			name: "param does not match pointer-to-slice element type",
			decl: ptrSliceDecl,
			rule: &rule.InstFuncRule{Param: "byte"},
			want: false,
		},
		{
			name: "result does not match a composite return's element type",
			decl: ptrSliceDecl,
			rule: &rule.InstFuncRule{Result: "error"},
			want: false,
		},
		{
			name: "last_result does not match a composite last return's element type",
			decl: ptrSliceDecl,
			rule: &rule.InstFuncRule{LastResult: "error"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mustMatch(t, tt.decl, tt.rule))
		})
	}
}

func TestFindFuncDeclCompositeSignatureFilters(t *testing.T) {
	file, err := NewAstParser().ParseSource(`package main

func Read(p []byte) (n int, err error) { return len(p), nil }
func Send(ch chan int) error { return nil }
func Log(msg string, extra []byte) {}
`)
	require.NoError(t, err)

	t.Run("composite-only param is no-match not fatal", func(t *testing.T) {
		fn, ok, findErr := FindFuncDecl(file, &rule.InstFuncRule{
			Func:              "Read",
			SignatureContains: &rule.FuncSignature{Args: []string{"byte"}},
		}, nil)
		require.NoError(t, findErr)
		assert.False(t, ok)
		assert.Nil(t, fn)
	})

	t.Run("simple type still matches beside a composite param", func(t *testing.T) {
		fn, ok, findErr := FindFuncDecl(file, &rule.InstFuncRule{
			Func:  "Log",
			Param: "string",
		}, nil)
		require.NoError(t, findErr)
		require.True(t, ok)
		require.NotNil(t, fn)
		assert.Equal(t, "Log", fn.Name.Name)
	})

	t.Run("chan param does not match element type", func(t *testing.T) {
		fn, ok, findErr := FindFuncDecl(file, &rule.InstFuncRule{
			Func:  "Send",
			Param: "int",
		}, nil)
		require.NoError(t, findErr)
		assert.False(t, ok)
		assert.Nil(t, fn)
	})
}

func TestFuncDeclMatchesFilters_CombinedFilters(t *testing.T) {
	// func(context.Context, string) (io.Reader, error)
	decl := makeFuncDecl(
		[]*dst.Field{field(selector("context", "Context")), field(ident("string"))},
		[]*dst.Field{field(selector("io", "Reader")), field(ident("error"))},
	)

	// All filters match → true
	sig := rule.FuncSignature{Args: []string{"context.Context", "string"}, Returns: []string{"io.Reader", "error"}}
	r := &rule.InstFuncRule{
		Signature:  &sig,
		Result:     "error",
		LastResult: "error",
		Param:      "context.Context",
	}
	assert.True(t, mustMatch(t, decl, r))

	// Signature matches but Param doesn't → false
	r2 := &rule.InstFuncRule{
		Signature: &sig,
		Param:     "int",
	}
	assert.False(t, mustMatch(t, decl, r2))
}

func TestFuncDeclMatchesFilters_NoParams(t *testing.T) {
	// func() error
	decl := makeFuncDecl(nil, []*dst.Field{field(ident("error"))})

	// Empty signature matches
	r := &rule.InstFuncRule{Signature: &rule.FuncSignature{Returns: []string{"error"}}}
	assert.True(t, mustMatch(t, decl, r))

	// Non-empty args don't match
	r2 := &rule.InstFuncRule{Signature: &rule.FuncSignature{Args: []string{"string"}, Returns: []string{"error"}}}
	assert.False(t, mustMatch(t, decl, r2))
}

func TestFuncDeclMatchesFilters_InvalidTypeReturnsError(t *testing.T) {
	decl := makeFuncDecl(
		[]*dst.Field{field(ident("string"))},
		[]*dst.Field{field(ident("error"))},
	)

	_, err := funcDeclMatchesFilters(decl, &rule.InstFuncRule{Result: "[]invalid"}, nil, nil)
	require.Error(t, err)

	_, err = funcDeclMatchesFilters(decl, &rule.InstFuncRule{LastResult: "[]invalid"}, nil, nil)
	require.Error(t, err)

	_, err = funcDeclMatchesFilters(decl, &rule.InstFuncRule{Param: "[]invalid"}, nil, nil)
	require.Error(t, err)
}

func TestFindFuncDeclRawRule(t *testing.T) {
	file := parseSharedFixture(t)

	raw := &rule.InstRawRule{Func: "TopLevel"}
	fn, ok, err := FindFuncDecl(file, raw, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, fn)
	assert.Equal(t, "TopLevel", fn.Name.Name)
}

// loadRawRuleFromYAML parses a structured inject_code rule the same way
// setup does (ParseFile + Rules). Tests that go through this path catch
// filters being hoisted by Normalize and then dropped by InstRawRule.
func loadRawRuleFromYAML(t *testing.T, yaml string) *rule.InstRawRule {
	t.Helper()
	doc, err := rule.ParseFile([]byte(yaml))
	require.NoError(t, err)
	rules, err := doc.Rules()
	require.NoError(t, err)
	require.Len(t, rules, 1)
	raw, ok := rules[0].(*rule.InstRawRule)
	require.True(t, ok, "expected InstRawRule, got %T", rules[0])
	return raw
}

func parseSource(t *testing.T, src string) *dst.File {
	t.Helper()
	file, err := NewAstParser().ParseSource(src)
	require.NoError(t, err)
	return file
}

// TestFindFuncDecl_InjectCodeHonorsSignatureFilters is the regression for
// issue #1607: signature / signature_contains / param / result / last_result
// on an inject_code rule must reject a same-named function whose types do
// not match, the same way they do on inject_hooks.
func TestFindFuncDecl_InjectCodeHonorsSignatureFilters(t *testing.T) {
	const processString = `package main

func Process(s string) {}
`

	const processCtx = `package main

import "context"

func Process(ctx context.Context) error { return nil }
`

	tests := []struct {
		name   string
		src    string
		yaml   string
		wantOK bool
	}{
		{
			name: "signature args mismatch is not a match",
			src:  processString,
			yaml: `
raw_with_signature:
  target: main
  where:
    func: Process
    signature:
      args: [context.Context]
  do:
    - inject_code:
        raw: 'println("injected")'
`,
			wantOK: false,
		},
		{
			name: "signature_contains args mismatch is not a match",
			src:  processString,
			yaml: `
raw_with_signature_contains:
  target: main
  where:
    func: Process
    signature_contains:
      args: [context.Context]
  do:
    - inject_code:
        raw: 'println("injected")'
`,
			wantOK: false,
		},
		{
			name: "param mismatch is not a match",
			src:  processString,
			yaml: `
raw_with_param:
  target: main
  where:
    func: Process
    param: context.Context
  do:
    - inject_code:
        raw: 'println("injected")'
`,
			wantOK: false,
		},
		{
			name: "result mismatch is not a match",
			src:  processString,
			yaml: `
raw_with_result:
  target: main
  where:
    func: Process
    result: error
  do:
    - inject_code:
        raw: 'println("injected")'
`,
			wantOK: false,
		},
		{
			name: "last_result mismatch is not a match",
			src:  processString,
			yaml: `
raw_with_last_result:
  target: main
  where:
    func: Process
    last_result: error
  do:
    - inject_code:
        raw: 'println("injected")'
`,
			wantOK: false,
		},
		{
			name: "matching signature still applies",
			src:  processCtx,
			yaml: `
raw_with_matching_signature:
  target: main
  where:
    func: Process
    signature:
      args: [context.Context]
      returns: [error]
    param: context.Context
    result: error
    last_result: error
  do:
    - inject_code:
        raw: 'println("injected")'
`,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := loadRawRuleFromYAML(t, tt.yaml)
			file := parseSource(t, tt.src)
			fn, ok, err := FindFuncDecl(file, raw, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				require.NotNil(t, fn)
				assert.Equal(t, "Process", fn.Name.Name)
			} else {
				assert.Nil(t, fn)
			}
		})
	}
}

func TestFindFuncDeclRawRuleMissing(t *testing.T) {
	file := parseSharedFixture(t)

	raw := &rule.InstRawRule{Func: "Missing"}
	fn, ok, err := FindFuncDecl(file, raw, nil)
	require.NoError(t, err)
	require.False(t, ok)
	assert.Nil(t, fn)
}

func TestFindFuncDeclFilterDef(t *testing.T) {
	file := parseSharedFixture(t)

	filter := &rule.FilterDef{HasFunc: "TopLevel"}
	fn, ok, err := FindFuncDecl(file, filter, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, fn)
	assert.Equal(t, "TopLevel", fn.Name.Name)
}

func TestFuncDeclMatchesFilters_SignatureParseError(t *testing.T) {
	decl := makeFuncDecl(
		[]*dst.Field{field(ident("string"))},
		[]*dst.Field{field(ident("error"))},
	)

	_, err := funcDeclMatchesFilters(decl, &rule.InstFuncRule{
		Signature: &rule.FuncSignature{Args: []string{"[]invalid"}},
	}, nil, nil)
	require.Error(t, err)

	_, err = funcDeclMatchesFilters(decl, &rule.InstFuncRule{
		SignatureContains: &rule.FuncSignature{Args: []string{"[]invalid"}},
	}, nil, nil)
	require.Error(t, err)

	_, err = funcDeclMatchesFilters(decl, &rule.InstFuncRule{
		SignatureContains: &rule.FuncSignature{Returns: []string{"[]invalid"}},
	}, nil, nil)
	require.Error(t, err)
}

func TestFuncDeclMatchesFilters_LastResultNoResults(t *testing.T) {
	decl := makeFuncDecl(nil, nil)

	ok, err := funcDeclMatchesFilters(decl, &rule.InstFuncRule{LastResult: "error"}, nil, nil)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestMakeAndIsUnusedIdent(t *testing.T) {
	id := Ident("foo")
	assert.False(t, IsUnusedIdent(id))

	got := MakeUnusedIdent(id)
	assert.Same(t, id, got)
	assert.Equal(t, IdentIgnore, got.Name)
	assert.True(t, IsUnusedIdent(got))
}

func TestIsStringLit(t *testing.T) {
	lit := StringLit("hello")
	assert.True(t, isStringLit(lit, "hello"))
	assert.False(t, isStringLit(lit, "world"))

	// A non-string-literal expression is never a string literal.
	assert.False(t, isStringLit(Ident("hello"), "hello"))

	// An integer literal has the wrong Kind.
	assert.False(t, isStringLit(IntLit(3), "3"))
}

func TestIsInterfaceType(t *testing.T) {
	assert.True(t, IsInterfaceType(InterfaceType()))
	// "any" is treated as interface{}.
	assert.True(t, IsInterfaceType(Ident("any")))
	// Other identifiers are not interface types.
	assert.False(t, IsInterfaceType(Ident("string")))
}

func TestIsEllipsis(t *testing.T) {
	assert.True(t, IsEllipsis(Ellipsis(Ident("int"))))
	assert.False(t, IsEllipsis(Ident("int")))
}

func TestStripGenericTypes(t *testing.T) {
	tests := []struct {
		name string
		expr dst.Expr
		want string
	}{
		{
			name: "value receiver",
			expr: Ident("MyStruct"),
			want: "MyStruct",
		},
		{
			name: "pointer receiver",
			expr: &dst.StarExpr{X: Ident("MyStruct")},
			want: "*MyStruct",
		},
		{
			name: "generic value receiver single param",
			expr: &dst.IndexExpr{X: Ident("GenStruct"), Index: Ident("T")},
			want: "GenStruct",
		},
		{
			name: "generic pointer receiver single param",
			expr: &dst.StarExpr{X: &dst.IndexExpr{X: Ident("GenStruct"), Index: Ident("T")}},
			want: "*GenStruct",
		},
		{
			name: "generic value receiver multiple params",
			expr: &dst.IndexListExpr{X: Ident("GenStruct"), Indices: []dst.Expr{Ident("T"), Ident("U")}},
			want: "GenStruct",
		},
		{
			name: "generic pointer receiver multiple params",
			expr: &dst.StarExpr{
				X: &dst.IndexListExpr{X: Ident("GenStruct"), Indices: []dst.Expr{Ident("T"), Ident("U")}},
			},
			want: "*GenStruct",
		},
		{
			name: "parenthesized value receiver",
			expr: &dst.ParenExpr{X: Ident("MyStruct")},
			want: "MyStruct",
		},
		{
			name: "parenthesized pointer receiver",
			expr: &dst.ParenExpr{X: &dst.StarExpr{X: Ident("MyStruct")}},
			want: "*MyStruct",
		},
		{
			name: "parenthesized generic receiver",
			expr: &dst.ParenExpr{X: &dst.IndexExpr{X: Ident("GenStruct"), Index: Ident("T")}},
			want: "GenStruct",
		},
		{
			name: "pointer to parenthesized receiver",
			expr: &dst.StarExpr{X: &dst.ParenExpr{X: Ident("MyStruct")}},
			want: "*MyStruct",
		},
		{
			name: "unrecognized expression yields empty",
			expr: &dst.SelectorExpr{X: Ident("pkg"), Sel: Ident("Type")},
			want: "",
		},
		{
			name: "pointer to unrecognized expression yields empty",
			expr: &dst.StarExpr{X: &dst.SelectorExpr{X: Ident("pkg"), Sel: Ident("Type")}},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripGenericTypes(tt.expr))
		})
	}
}

func TestRenderExpr(t *testing.T) {
	tests := []struct {
		name string
		expr dst.Expr
		want string
	}{
		{
			name: "identifier",
			expr: &dst.Ident{Name: "foo"},
			want: "foo",
		},
		{
			name: "call expression",
			expr: &dst.CallExpr{
				Fun:  &dst.Ident{Name: "f"},
				Args: []dst.Expr{&dst.Ident{Name: "a"}, &dst.Ident{Name: "b"}},
			},
			want: "f(a, b)",
		},
		{
			name: "binary expression",
			expr: &dst.BinaryExpr{
				X:  &dst.Ident{Name: "a"},
				Op: token.ADD,
				Y:  &dst.Ident{Name: "b"},
			},
			want: "a + b",
		},
		{
			name: "selector expression",
			expr: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "pkg"},
				Sel: &dst.Ident{Name: "Func"},
			},
			want: "pkg.Func",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, err := RenderExpr(tt.expr)

			require.NoError(t, err)
			assert.Equal(t, tt.want, text)
		})
	}
}

func TestRenderNode(t *testing.T) {
	node := &dst.Ident{Name: "a"}
	synthetic := &dst.File{
		Name: Ident("_"),
		Decls: []dst.Decl{
			&dst.FuncDecl{
				Name: Ident("_"),
				Type: &dst.FuncType{Params: &dst.FieldList{}},
				Body: &dst.BlockStmt{List: []dst.Stmt{&dst.ExprStmt{X: node}}},
			},
		},
	}
	restorer := decorator.NewRestorer()
	_, err := restorer.RestoreFile(synthetic)
	require.NoError(t, err)

	t.Run("known node renders its source text", func(t *testing.T) {
		text, txtErr := RenderNode(restorer, node)

		require.NoError(t, txtErr)
		assert.Equal(t, "a", text)
	})

	t.Run("node not present in the restorer errors", func(t *testing.T) {
		unrelated := &dst.Ident{Name: "unrelated"}

		_, txtErr := RenderNode(restorer, unrelated)

		require.Error(t, txtErr)
		assert.Contains(t, txtErr.Error(), "failed to locate restored node")
	})
}

// TestFindFuncDeclParenthesizedReceiver covers the reachable parenthesized
// receiver forms. These compile and appear in unformatted dependency source, and
// before the graceful-no-match change an unrecognized receiver ended the build
// via util.Unimplemented. They should now match the base type like the
// unparenthesized forms.
func TestFindFuncDeclParenthesizedReceiver(t *testing.T) {
	src := `package main

type T struct{}

func (r (T)) Value() {}

func (r (*T)) Pointer() {}
`
	file, err := NewAstParser().ParseSource(src)
	require.NoError(t, err)

	value, ok, err := FindFuncDecl(file, &rule.InstFuncRule{Func: "Value", Recv: "T"}, nil)
	require.NoError(t, err)
	require.True(t, ok, "parenthesized value receiver (r (T)) should match")
	assert.Equal(t, "Value", value.Name.Name)

	pointer, ok, err := FindFuncDecl(file, &rule.InstFuncRule{Func: "Pointer", Recv: "*T"}, nil)
	require.NoError(t, err)
	require.True(t, ok, "parenthesized pointer receiver (r (*T)) should match")
	assert.Equal(t, "Pointer", pointer.Name.Name)
}

// TestFindFuncDeclUnrecognizedReceiverNoMatch covers the graceful degradation: a
// receiver shape stripGenericTypes does not recognize returns no match rather
// than ending the build. A qualified receiver does not compile and so never
// reaches the tool from real source, but the guard must still not abort.
func TestFindFuncDeclUnrecognizedReceiverNoMatch(t *testing.T) {
	// The receiver has to be a shape stripGenericTypes does not recognise, so a
	// qualified receiver is parsed rather than taken from the fixture: the
	// fixture's receivers all strip cleanly and would never reach the guard.
	file, err := NewAstParser().ParseSource(`package main

func (r pkg.T) Method() error { return nil }
`)
	require.NoError(t, err)

	// Both the function name and the receiver base name are the ones this
	// declaration would match on if the shape were recognised, so returning nil
	// can only come from the guard rather than from a name mismatch.
	assert.Nil(t, findFuncDecl(file, "Method", "T"),
		"an unrecognized receiver should be a no match, not a build abort")
}
