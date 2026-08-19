// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/ast"
)

// parseReceiverType returns the file and the receiver type expression of a
// method declared on the given receiver source, e.g. "*GenStruct[T]". The
// synthetic file has no matching type declaration, so constraint recovery
// attempted against it necessarily falls back to any.
func parseReceiverType(t *testing.T, recvSrc string) (*dst.File, dst.Expr) {
	t.Helper()

	src := "package main\nfunc (r " + recvSrc + ") m() {}"
	return parseReceiverSource(t, src)
}

// parseReceiverTypeWithDecl returns the file and the receiver type expression
// of a method declared on recvSrc, where typeDecl is the source of the
// generic type's own declaration (e.g. "type GenStruct[T comparable] struct{}"),
// placed ahead of the method in the same file so constraint recovery can find
// it. imports, if non-empty, is a complete import declaration inserted before
// typeDecl.
func parseReceiverTypeWithDecl(t *testing.T, imports, typeDecl, recvSrc string) (*dst.File, dst.Expr) {
	t.Helper()

	src := "package main\n" + imports + "\n" + typeDecl + "\nfunc (r " + recvSrc + ") m() {}"
	return parseReceiverSource(t, src)
}

func parseReceiverSource(t *testing.T, src string) (*dst.File, dst.Expr) {
	t.Helper()

	parser := ast.NewAstParser()
	file, err := parser.ParseSource(src)
	require.NoError(t, err)

	var funcDecl *dst.FuncDecl
	for _, decl := range file.Decls {
		if fd, ok := decl.(*dst.FuncDecl); ok {
			funcDecl = fd
			break
		}
	}
	require.NotNil(t, funcDecl, "source must declare exactly one func")
	require.NotNil(t, funcDecl.Recv)
	require.Len(t, funcDecl.Recv.List, 1)

	return file, funcDecl.Recv.List[0].Type
}

// typeParamNames lists the parameter names in a field list, so a test can
// assert on the names without reaching into the dst structure each time.
func typeParamNames(t *testing.T, fields *dst.FieldList) []string {
	t.Helper()
	if fields == nil {
		return nil
	}

	names := make([]string, 0, len(fields.List))
	for _, field := range fields.List {
		require.Len(t, field.Names, 1, "each type parameter should carry exactly one name")
		names = append(names, field.Names[0].Name)
	}
	return names
}

func TestExtractReceiverTypeParams(t *testing.T) {
	tests := []struct {
		name     string
		recvSrc  string
		expected []string
	}{
		{
			name:     "value receiver without type parameters",
			recvSrc:  "GenStruct",
			expected: nil,
		},
		{
			name:     "pointer receiver without type parameters",
			recvSrc:  "*GenStruct",
			expected: nil,
		},
		{
			name:     "value receiver with one type parameter",
			recvSrc:  "GenStruct[T]",
			expected: []string{"T"},
		},
		{
			name:     "pointer receiver with one type parameter",
			recvSrc:  "*GenStruct[T]",
			expected: []string{"T"},
		},
		{
			name:     "value receiver with two type parameters",
			recvSrc:  "GenStruct[T, U]",
			expected: []string{"T", "U"},
		},
		{
			name:     "pointer receiver with two type parameters",
			recvSrc:  "*GenStruct[T, U]",
			expected: []string{"T", "U"},
		},
		{
			name:     "pointer receiver with three type parameters",
			recvSrc:  "*GenStruct[T, U, V]",
			expected: []string{"T", "U", "V"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, recvType := parseReceiverType(t, tt.recvSrc)

			params := extractReceiverTypeParams(file, recvType)

			if tt.expected == nil {
				assert.Nil(t, params, "a receiver without type parameters should produce no field list")
				return
			}

			require.NotNil(t, params)
			assert.Equal(t, tt.expected, typeParamNames(t, params))
		})
	}
}

// TestExtractReceiverTypeParamsConstraint_NoMatchingDecl documents the
// fallback: when the receiver's generic type declaration can't be found in
// file (here, because the synthetic source doesn't declare it at all), the
// constraint widens to any rather than failing.
func TestExtractReceiverTypeParamsConstraint_NoMatchingDecl(t *testing.T) {
	file, recvType := parseReceiverType(t, "*GenStruct[T, U]")

	params := extractReceiverTypeParams(file, recvType)
	require.NotNil(t, params)
	require.Len(t, params.List, 2)

	for _, field := range params.List {
		constraint, ok := field.Type.(*dst.Ident)
		require.True(t, ok, "expected the constraint to be a plain identifier")
		assert.Equal(t, "any", constraint.Name)
	}
}

// TestExtractReceiverTypeParamsConstraint_Recovered covers recovering the
// real constraint from the receiver's own type declaration when it's present
// in the same file: a built-in constraint, a package-qualified one, and a
// receiver that renames its type parameter relative to the declaration
// (constraints are matched positionally, not by name).
func TestExtractReceiverTypeParamsConstraint_Recovered(t *testing.T) {
	tests := []struct {
		name         string
		imports      string
		typeDecl     string
		recvSrc      string
		wantNames    []string
		wantIdent    string // for a plain identifier constraint, e.g. "comparable"
		wantSelector string // for a package-qualified constraint, e.g. "constraints.Ordered"
	}{
		{
			name:      "built-in constraint",
			typeDecl:  "type GenStruct[T comparable] struct{}",
			recvSrc:   "GenStruct[T]",
			wantNames: []string{"T"},
			wantIdent: "comparable",
		},
		{
			name:      "renamed type parameter still resolves by position",
			typeDecl:  "type GenStruct[T comparable] struct{}",
			recvSrc:   "GenStruct[U]",
			wantNames: []string{"U"},
			wantIdent: "comparable",
		},
		{
			name:         "package-qualified constraint",
			imports:      `import "golang.org/x/exp/constraints"`,
			typeDecl:     "type GenStruct[T constraints.Ordered] struct{}",
			recvSrc:      "GenStruct[T]",
			wantNames:    []string{"T"},
			wantSelector: "constraints.Ordered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, recvType := parseReceiverTypeWithDecl(t, tt.imports, tt.typeDecl, tt.recvSrc)

			params := extractReceiverTypeParams(file, recvType)
			require.NotNil(t, params)
			require.Len(t, params.List, 1)
			assert.Equal(t, tt.wantNames, typeParamNames(t, params))

			switch {
			case tt.wantIdent != "":
				constraint, ok := params.List[0].Type.(*dst.Ident)
				require.True(t, ok, "expected the constraint to be a plain identifier")
				assert.Equal(t, tt.wantIdent, constraint.Name)
			case tt.wantSelector != "":
				sel, ok := params.List[0].Type.(*dst.SelectorExpr)
				require.True(t, ok, "expected the constraint to be a package-qualified selector")
				pkgIdent, ok := sel.X.(*dst.Ident)
				require.True(t, ok)
				assert.Equal(t, tt.wantSelector, pkgIdent.Name+"."+sel.Sel.Name)
			}
		})
	}
}

// TestExtractReceiverTypeParamsConstraint_MultipleParams covers positional
// matching against a declaration with several type parameters and mixed
// constraints, including one field whose two names share a single constraint.
func TestExtractReceiverTypeParamsConstraint_MultipleParams(t *testing.T) {
	file, recvType := parseReceiverTypeWithDecl(t, "",
		"type GenStruct[T, U comparable, V any] struct{}",
		"GenStruct[T, U, V]")

	params := extractReceiverTypeParams(file, recvType)
	require.NotNil(t, params)
	require.Len(t, params.List, 3)
	assert.Equal(t, []string{"T", "U", "V"}, typeParamNames(t, params))

	for i, want := range []string{"comparable", "comparable", "any"} {
		constraint, ok := params.List[i].Type.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, want, constraint.Name)
	}
}

// TestReceiverBaseTypeName_NonIdent covers the defensive fallback directly: a
// non-identifier base expression, which no valid Go receiver form actually
// produces, returns "" rather than panicking.
func TestReceiverBaseTypeName_NonIdent(t *testing.T) {
	nonIdent := &dst.SelectorExpr{X: ast.Ident("pkg"), Sel: ast.Ident("GenStruct")}

	assert.Empty(t, receiverBaseTypeName(nonIdent))
}

// TestFindGenericTypeDecl_NilFileOrEmptyName covers both guard conditions in
// findGenericTypeDecl directly: a nil file and an empty type name should each
// short-circuit to nil without walking file.Decls.
func TestFindGenericTypeDecl_NilFileOrEmptyName(t *testing.T) {
	file, _ := parseReceiverTypeWithDecl(t, "", "type GenStruct[T comparable] struct{}", "GenStruct[T]")

	assert.Nil(t, findGenericTypeDecl(nil, "GenStruct"))
	assert.Nil(t, findGenericTypeDecl(file, ""))
}

// TestReceiverConstraintAt_EmptyNamesField covers the defensive n=1 fallback
// in the position-walking loop. A type parameter field with no names doesn't
// occur in valid Go, but the loop should still advance past it by one
// position instead of looping forever or reading out of range.
func TestReceiverConstraintAt_EmptyNamesField(t *testing.T) {
	original := &dst.FieldList{
		List: []*dst.Field{
			{Names: nil, Type: ast.Ident("comparable")},
			{Names: []*dst.Ident{ast.Ident("T")}, Type: ast.Ident("any")},
		},
	}

	constraint := receiverConstraintAt(original, 1)

	ident, ok := constraint.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "any", ident.Name)
}

// TestExtractReceiverTypeParamsNestedPointer covers the recursive path. A
// doubly-indirected receiver is not valid Go, but the function recurses
// through StarExpr without limit and callers reach it from expressions that
// have not been validated yet.
func TestExtractReceiverTypeParamsNestedPointer(t *testing.T) {
	file, inner := parseReceiverType(t, "*GenStruct[T]")
	nested := &dst.StarExpr{X: inner}

	params := extractReceiverTypeParams(file, nested)

	require.NotNil(t, params)
	assert.Equal(t, []string{"T"}, typeParamNames(t, params))
}
