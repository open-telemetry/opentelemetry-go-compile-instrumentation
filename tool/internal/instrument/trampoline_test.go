// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"errors"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/ast"
)

func TestBaseTypeName(t *testing.T) {
	tests := []struct {
		name     string
		typeSrc  string
		expected string
	}{
		{
			name:     "simple ident",
			typeSrc:  "int",
			expected: "int",
		},
		{
			name:     "pointer type",
			typeSrc:  "*string",
			expected: "string",
		},
		{
			name:     "double pointer",
			typeSrc:  "**float64",
			expected: "float64",
		},
		{
			name:     "package qualified type",
			typeSrc:  "pkg.Type",
			expected: "Type",
		},
		{
			name:     "pointer to package qualified type",
			typeSrc:  "*pkg.Type",
			expected: "Type",
		},
		{
			name:     "interface type",
			typeSrc:  "interface{}",
			expected: "interface{}",
		},
		{
			name:     "array type",
			typeSrc:  "[]int",
			expected: "int",
		},
		{
			name:     "nested array type",
			typeSrc:  "[][]string",
			expected: "string",
		},
		{
			name:     "array of pointer type",
			typeSrc:  "[]*int",
			expected: "int",
		},
		{
			name:     "array of package qualified type",
			typeSrc:  "[]pkg.Type",
			expected: "Type",
		},
		{
			name:     "ellipsis type",
			typeSrc:  "...int",
			expected: "int",
		},
		{
			name:     "ellipsis of pointer type",
			typeSrc:  "...*string",
			expected: "string",
		},
		{
			name:     "ellipsis of package qualified type",
			typeSrc:  "...pkg.Type",
			expected: "Type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse a function with the type as a parameter
			src := "package main\nfunc f(p " + tt.typeSrc + ") {}"
			parser := ast.NewAstParser()
			file, err := parser.ParseSource(src)
			require.NoError(t, err)

			funcDecl, ok := file.Decls[0].(*dst.FuncDecl)
			require.True(t, ok)
			require.NotNil(t, funcDecl.Type.Params)
			require.Len(t, funcDecl.Type.Params.List, 1)

			typeExpr := funcDecl.Type.Params.List[0].Type
			result := baseTypeName(typeExpr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCheckHookDecl(t *testing.T) {
	tests := []struct {
		name        string
		trampSrc    string
		hookSrc     string
		before      bool
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid before hook - pointer types match value types",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string, param1 *int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 string, p2 int) {}`,
			before: true,
		},
		{
			name: "valid after hook - pointer types match value types",
			trampSrc: `
package main
func OtelAfterTrampoline(hookContext *HookContext, ret0 *float32, ret1 *error) {}`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1After(ctx hook.HookContext, r1 float32, r2 error) {}`,
			before: false,
		},
		{
			name: "invalid - missing HookContext in hook",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
func H1Before(p1 string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "expected 2 params, got 1",
		},
		{
			name: "invalid - type mismatch",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string, param1 *int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 string, p2 string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trampFunc := parseFunc(t, tt.trampSrc)
			hookFunc := parseFunc(t, tt.hookSrc)

			ip := &InstrumentPhase{}
			if tt.before {
				ip.beforeTrampFunc = trampFunc
			} else {
				ip.afterTrampFunc = trampFunc
			}

			err := ip.checkHookDecl(hookFunc, tt.before)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// genHookContext mirrors the HookContextImpl the trampoline generates for a
// concrete *int param and *error return (see setValue/getValue): Set writes
// through the stored pointer, using the zero value when val is nil. The real
// type is synthesized per target function and can't be imported, so we stand it
// in here to run the nil path that the golden suite only compiles, never runs.
type genHookContext struct {
	params     []any
	returnVals []any
}

func (c *genHookContext) SetParam(idx int, val any) {
	if idx == 0 {
		if val == nil {
			*(c.params[0].(*int)) = 0
		} else {
			*(c.params[0].(*int)) = val.(int)
		}
	}
}

func (c *genHookContext) GetParam(idx int) any {
	if idx == 0 {
		return *(c.params[0].(*int))
	}
	return nil
}

func (c *genHookContext) SetReturnVal(idx int, val any) {
	if idx == 0 {
		if val == nil {
			*(c.returnVals[0].(*error)) = nil
		} else {
			*(c.returnVals[0].(*error)) = val.(error)
		}
	}
}

func (c *genHookContext) GetReturnVal(idx int) any {
	if idx == 0 {
		return *(c.returnVals[0].(*error))
	}
	return nil
}

// TestHookContextSetNilWritesZeroValue is the runtime guard for #726: before the
// fix, Set*(idx, nil) replaced the slot with nil, so the next Get* panicked on
// the type assertion and the underlying value never changed. A nil val must now
// write the concrete type's zero value through the stored pointer.
func TestHookContextSetNilWritesZeroValue(t *testing.T) {
	param := 42
	retVal := errors.New("boom")
	c := &genHookContext{
		params:     []any{&param},
		returnVals: []any{&retVal},
	}

	require.NotPanics(t, func() { c.SetParam(0, nil) })
	assert.Equal(t, 0, c.GetParam(0))
	assert.Equal(t, 0, param) // written through the pointer, not just the slot

	require.NotPanics(t, func() { c.SetReturnVal(0, nil) })
	assert.Nil(t, c.GetReturnVal(0))
	require.NoError(t, retVal) // the error itself was zeroed to nil

	c.SetParam(0, 7)
	assert.Equal(t, 7, c.GetParam(0))
}

func TestImplTemplate_ParseAndMaterialize(t *testing.T) {
	p := ast.NewAstParser()
	astRoot, err := p.ParseSource(templateImpl)
	require.NoError(t, err)
	require.NotNil(t, astRoot)

	// Verify that GetKeyData, SetKeyData, and HasKeyData methods are present in templateImpl
	foundGetKeyData := false
	foundSetKeyData := false
	foundHasKeyData := false

	for _, decl := range astRoot.Decls {
		if funcDecl, ok := decl.(*dst.FuncDecl); ok {
			switch funcDecl.Name.Name {
			case "GetKeyData":
				foundGetKeyData = true
			case "SetKeyData":
				foundSetKeyData = true
			case "HasKeyData":
				foundHasKeyData = true
			}
		}
	}

	require.True(t, foundGetKeyData, "GetKeyData must be present in impl.tmpl")
	require.True(t, foundSetKeyData, "SetKeyData must be present in impl.tmpl")
	require.True(t, foundHasKeyData, "HasKeyData must be present in impl.tmpl")
}

// typeParamsT builds a type-parameter list with a single parameter named "T".
func typeParamsT() *dst.FieldList {
	return &dst.FieldList{
		List: []*dst.Field{{Names: []*dst.Ident{dst.NewIdent("T")}}},
	}
}

func TestIsTypeParameter(t *testing.T) {
	tp := typeParamsT()
	assert.True(t, isTypeParameter(dst.NewIdent("T"), tp))
	assert.False(t, isTypeParameter(dst.NewIdent("string"), tp))
	assert.False(t, isTypeParameter(dst.NewIdent("T"), nil))
	// Non-identifier expressions are never type parameters.
	assert.False(t, isTypeParameter(&dst.StarExpr{X: dst.NewIdent("T")}, tp))
}

func TestReplaceTypeParamsWithAny(t *testing.T) {
	tp := typeParamsT()

	t.Run("bare type parameter becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(dst.NewIdent("T"), tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("pointer to type parameter", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.StarExpr{X: dst.NewIdent("T")}, tp)
		star, ok := got.(*dst.StarExpr)
		require.True(t, ok)
		assert.IsType(t, &dst.InterfaceType{}, star.X)
	})

	t.Run("slice of type parameter", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.ArrayType{Elt: dst.NewIdent("T")}, tp)
		arr, ok := got.(*dst.ArrayType)
		require.True(t, ok)
		assert.IsType(t, &dst.InterfaceType{}, arr.Elt)
	})

	t.Run("map with type parameter key and value", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.MapType{Key: dst.NewIdent("T"), Value: dst.NewIdent("T")}, tp)
		m, ok := got.(*dst.MapType)
		require.True(t, ok)
		assert.IsType(t, &dst.InterfaceType{}, m.Key)
		assert.IsType(t, &dst.InterfaceType{}, m.Value)
	})

	t.Run("channel of type parameter", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.ChanType{Dir: dst.SEND, Value: dst.NewIdent("T")}, tp)
		ch, ok := got.(*dst.ChanType)
		require.True(t, ok)
		assert.Equal(t, dst.SEND, ch.Dir)
		assert.IsType(t, &dst.InterfaceType{}, ch.Value)
	})

	t.Run("generic index expression becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.IndexExpr{X: dst.NewIdent("GenStruct"), Index: dst.NewIdent("T")}, tp)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("generic index list expression becomes interface{}", func(t *testing.T) {
		got := replaceTypeParamsWithAny(
			&dst.IndexListExpr{X: dst.NewIdent("GenStruct"), Indices: []dst.Expr{dst.NewIdent("T"), dst.NewIdent("U")}},
			tp,
		)
		assert.IsType(t, &dst.InterfaceType{}, got)
	})

	t.Run("variadic type parameter preserves ellipsis", func(t *testing.T) {
		got := replaceTypeParamsWithAny(&dst.Ellipsis{Elt: dst.NewIdent("T")}, tp)
		ell, ok := got.(*dst.Ellipsis)
		require.True(t, ok)
		assert.IsType(t, &dst.InterfaceType{}, ell.Elt)
	})

	t.Run("func type processes params and results", func(t *testing.T) {
		fn := &dst.FuncType{
			Params: &dst.FieldList{List: []*dst.Field{
				{Names: []*dst.Ident{dst.NewIdent("x")}, Type: dst.NewIdent("T")},
			}},
			Results: &dst.FieldList{List: []*dst.Field{
				{Type: dst.NewIdent("T")},
			}},
		}
		got := replaceTypeParamsWithAny(fn, tp)
		newFn, ok := got.(*dst.FuncType)
		require.True(t, ok)
		require.Len(t, newFn.Params.List, 1)
		require.Len(t, newFn.Results.List, 1)
		// The named parameter keeps its name but its type becomes interface{}.
		require.Len(t, newFn.Params.List[0].Names, 1)
		assert.Equal(t, "x", newFn.Params.List[0].Names[0].Name)
		assert.IsType(t, &dst.InterfaceType{}, newFn.Params.List[0].Type)
		assert.IsType(t, &dst.InterfaceType{}, newFn.Results.List[0].Type)
	})

	t.Run("non-type-param identifier is returned unchanged", func(t *testing.T) {
		in := dst.NewIdent("string")
		got := replaceTypeParamsWithAny(in, tp)
		assert.Same(t, in, got)
	})

	t.Run("selector expression is returned unchanged", func(t *testing.T) {
		in := &dst.SelectorExpr{X: dst.NewIdent("pkg"), Sel: dst.NewIdent("Type")}
		got := replaceTypeParamsWithAny(in, tp)
		assert.Same(t, in, got)
	})
}

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
