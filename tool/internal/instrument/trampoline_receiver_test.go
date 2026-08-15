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

// parseReceiverType returns the receiver type expression of a method declared
// on the given receiver source, e.g. "*GenStruct[T]".
func parseReceiverType(t *testing.T, recvSrc string) dst.Expr {
	t.Helper()

	src := "package main\nfunc (r " + recvSrc + ") m() {}"
	parser := ast.NewAstParser()
	file, err := parser.ParseSource(src)
	require.NoError(t, err)

	funcDecl, ok := file.Decls[0].(*dst.FuncDecl)
	require.True(t, ok)
	require.NotNil(t, funcDecl.Recv)
	require.Len(t, funcDecl.Recv.List, 1)

	return funcDecl.Recv.List[0].Type
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
			recvType := parseReceiverType(t, tt.recvSrc)

			params := extractReceiverTypeParams(recvType)

			if tt.expected == nil {
				assert.Nil(t, params, "a receiver without type parameters should produce no field list")
				return
			}

			require.NotNil(t, params)
			assert.Equal(t, tt.expected, typeParamNames(t, params))
		})
	}
}

// TestExtractReceiverTypeParamsConstraint records that every extracted
// parameter is given the "any" constraint. The trampoline only needs the
// parameter to exist and accept whatever the original receiver accepted, so
// the real constraint is deliberately widened rather than carried across.
func TestExtractReceiverTypeParamsConstraint(t *testing.T) {
	recvType := parseReceiverType(t, "*GenStruct[T, U]")

	params := extractReceiverTypeParams(recvType)
	require.NotNil(t, params)
	require.Len(t, params.List, 2)

	for _, field := range params.List {
		constraint, ok := field.Type.(*dst.Ident)
		require.True(t, ok, "expected the constraint to be a plain identifier")
		assert.Equal(t, "any", constraint.Name)
	}
}

// TestExtractReceiverTypeParamsNestedPointer covers the recursive path. A
// doubly-indirected receiver is not valid Go, but the function recurses
// through StarExpr without limit and callers reach it from expressions that
// have not been validated yet.
func TestExtractReceiverTypeParamsNestedPointer(t *testing.T) {
	inner := parseReceiverType(t, "*GenStruct[T]")
	nested := &dst.StarExpr{X: inner}

	params := extractReceiverTypeParams(nested)

	require.NotNil(t, params)
	assert.Equal(t, []string{"T"}, typeParamNames(t, params))
}
