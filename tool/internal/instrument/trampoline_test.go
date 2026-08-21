// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
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
			name:     "non-empty interface type",
			typeSrc:  "interface{ Read(p []byte) (n int, err error) }",
			expected: "interface{Read([]byte) (int, error)}",
		},
		{
			name:     "interface type with embedded interface",
			typeSrc:  "interface{ pkg.Reader }",
			expected: "interface{Reader}",
		},
		{
			name:     "empty struct type",
			typeSrc:  "struct{}",
			expected: "struct{}",
		},
		{
			name:     "non-empty struct type",
			typeSrc:  "struct{ Name string }",
			expected: "struct{Name string}",
		},
		{
			name:     "struct type with embedded field",
			typeSrc:  "struct{ pkg.Base }",
			expected: "struct{Base}",
		},
		{
			name:     "fixed-size array type with literal length",
			typeSrc:  "[5]int",
			expected: "[5]int",
		},
		{
			name:     "fixed-size array type with named length",
			typeSrc:  "[N]int",
			expected: "[N]int",
		},
		{
			name:     "func type with no params or results",
			typeSrc:  "func()",
			expected: "func()",
		},
		{
			name:     "func type with unnamed params and multiple results",
			typeSrc:  "func(int, string) (bool, error)",
			expected: "func(int, string) (bool, error)",
		},
		{
			name:     "func type with a single result",
			typeSrc:  "func(int) string",
			expected: "func(int) string",
		},
		{
			name:     "generic type with one type argument",
			typeSrc:  "Foo[int]",
			expected: "Foo[int]",
		},
		{
			name:     "generic type with multiple type arguments",
			typeSrc:  "Foo[int, string]",
			expected: "Foo[int, string]",
		},
		{
			name:     "array type",
			typeSrc:  "[]int",
			expected: "[]int",
		},
		{
			name:     "nested array type",
			typeSrc:  "[][]string",
			expected: "[][]string",
		},
		{
			name:     "array of pointer type",
			typeSrc:  "[]*int",
			expected: "[]int",
		},
		{
			name:     "array of package qualified type",
			typeSrc:  "[]pkg.Type",
			expected: "[]Type",
		},
		{
			name:     "ellipsis type",
			typeSrc:  "...int",
			expected: "...int",
		},
		{
			name:     "ellipsis of pointer type",
			typeSrc:  "...*string",
			expected: "...string",
		},
		{
			name:     "ellipsis of package qualified type",
			typeSrc:  "...pkg.Type",
			expected: "...Type",
		},
		{
			name:     "map type",
			typeSrc:  "map[string]int",
			expected: "map[string]int",
		},
		{
			name:     "map of package qualified types",
			typeSrc:  "map[pkg.Key]*pkg.Val",
			expected: "map[Key]Val",
		},
		{
			name:     "channel type",
			typeSrc:  "chan bool",
			expected: "chan bool",
		},
		{
			name:     "receive channel type",
			typeSrc:  "<-chan int",
			expected: "<-chan int",
		},
		{
			name:     "send channel type",
			typeSrc:  "chan<- string",
			expected: "chan<- string",
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

func TestBaseTypeName_NilExpr(t *testing.T) {
	assert.Empty(t, baseTypeName(nil))
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
			name: "valid before hook - slice and variadic compatibility",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *[]string, param1 *[]int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 []string, p2 ...int) {}`,
			before: true,
		},
		{
			name: "valid before hook - map and chan types",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *map[string]int, param1 *chan bool) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 map[string]int, p2 chan bool) {}`,
			before: true,
		},
		{
			name: "valid before hook - any/interface{} wildcard",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 any) {}`,
			before: true,
		},
		{
			name: "valid before hook - slice of any matching variadic interface{}",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *[]any) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 ...interface{}) {}`,
			before: true,
		},
		{
			name: "valid before hook - slice of concrete type matching variadic interface{}",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *[]SpanEndOption) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 ...interface{}) {}`,
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
			name: "invalid - before hook first param is not HookContext",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
func H1Before(notCtx int, p1 string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "hook func first param must be HookContext, got int",
		},
		{
			name: "invalid - after hook param count mismatch",
			trampSrc: `
package main
func OtelAfterTrampoline(hookContext *HookContext, ret0 *string) {}`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1After(ctx hook.HookContext) {}`,
			before:      false,
			expectError: true,
			errorMsg:    "expected 2 params, got 1",
		},
		{
			name: "invalid - after hook type mismatch",
			trampSrc: `
package main
func OtelAfterTrampoline(hookContext *HookContext, ret0 *string) {}`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1After(ctx hook.HookContext, r1 int) {}`,
			before:      false,
			expectError: true,
			errorMsg:    "type mismatch, expected string, got int",
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
			name: "invalid - type mismatch basic",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string, param1 *int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 string, p2 string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch, expected int, got string",
		},
		{
			name: "invalid - scalar vs slice mismatch",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 []string) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch, expected string, got []string",
		},
		{
			name: "invalid - map vs chan mismatch",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *map[string]int) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 chan bool) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch, expected map[string]int, got chan bool",
		},
		{
			name: "invalid - slice element type mismatch",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *[]string) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 []int) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch, expected []string, got []int",
		},
		{
			// Regression: baseTypeName used to normalize every non-empty
			// interface to the literal placeholder "interface{...}", so
			// structurally distinct interfaces collided and falsely matched.
			name: "invalid - distinct anonymous interfaces don't collide",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *interface{ Read(p []byte) (n int, err error) }) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 interface{ Write(p []byte) (n int, err error) }) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch",
		},
		{
			name: "valid before hook - matching anonymous interfaces",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *interface{ Read(p []byte) (n int, err error) }) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 interface{ Read(p []byte) (n int, err error) }) {}`,
			before: true,
		},
		{
			// Same class of bug as the anonymous interface case above, but for
			// anonymous struct types.
			name: "invalid - distinct anonymous structs don't collide",
			trampSrc: `
package main
func OtelBeforeTrampoline(param0 *struct{ Name string }) (hookContext *HookContext, skipCall bool) { return nil, false }`,
			hookSrc: `
package testdata
import "go.opentelemetry.io/otelc/pkg/hook"
func H1Before(ctx hook.HookContext, p1 struct{ Age int }) {}`,
			before:      true,
			expectError: true,
			errorMsg:    "type mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trampFunc := parseFunc(t, tt.trampSrc)
			hookFunc := parseFunc(t, tt.hookSrc)

			ip := &instrumentPhase{}
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

			ip := newTestPhase()
			ip.target = file
			params, err := ip.extractReceiverTypeParams(t.Context(), recvType)
			require.NoError(t, err)

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

	ip := newTestPhase()
	ip.target = file
	params, err := ip.extractReceiverTypeParams(t.Context(), recvType)
	require.NoError(t, err)
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

			ip := newTestPhase()
			ip.target = file
			params, err := ip.extractReceiverTypeParams(t.Context(), recvType)
			require.NoError(t, err)
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

	ip := newTestPhase()
	ip.target = file
	params, err := ip.extractReceiverTypeParams(t.Context(), recvType)
	require.NoError(t, err)
	require.NotNil(t, params)
	require.Len(t, params.List, 3)
	assert.Equal(t, []string{"T", "U", "V"}, typeParamNames(t, params))

	for i, want := range []string{"comparable", "comparable", "any"} {
		constraint, ok := params.List[i].Type.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, want, constraint.Name)
	}
}

// TestResolveGenericTypeDecl_CrossFile covers the whole-package fallback: the
// generic type's declaration lives in a different file of the same package
// than the method being instrumented, so the same-file lookup misses and the
// sibling-file search must find it instead.
func TestResolveGenericTypeDecl_CrossFile(t *testing.T) {
	methodFile, _ := parseReceiverType(t, "*GenStruct[T]")

	parser := ast.NewAstParser()
	declFile, err := parser.ParseSource("package main\n\ntype GenStruct[T comparable] struct{}\n")
	require.NoError(t, err)

	ip := newTestPhase()
	ip.target = methodFile
	ip.siblingASTs = map[string]*dst.File{"decl.go": declFile}

	params, found := ip.resolveGenericTypeDecl("GenStruct")

	require.NotNil(t, params)
	assert.Same(t, declFile, found, "resolveGenericTypeDecl must report which file the declaration came from")
	require.Len(t, params.List, 1)
	constraint, ok := params.List[0].Type.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "comparable", constraint.Name)
}

// TestResolveGenericTypeDecl_SameFileFastPathSkipsSiblingLoad confirms the
// same-file hit never touches sibling discovery at all: ip.siblingASTs stays
// nil, so a package with many files pays no parsing cost for a method whose
// generic type is declared right there in the same file (the common case).
func TestResolveGenericTypeDecl_SameFileFastPathSkipsSiblingLoad(t *testing.T) {
	file, _ := parseReceiverTypeWithDecl(t, "", "type GenStruct[T comparable] struct{}", "GenStruct[T]")

	ip := newTestPhase()
	ip.target = file
	// No compileArgs/targetPath configured, so if loadSiblingASTs were called
	// it would have nothing to work with anyway -- but the point of this test
	// is that it must not even be invoked in the first place.

	params, found := ip.resolveGenericTypeDecl("GenStruct")

	require.NotNil(t, params)
	assert.Same(t, file, found)
	assert.Nil(t, ip.siblingASTs, "same-file hit must not trigger sibling loading at all")
}

// TestResolveGenericTypeDecl_CachesSiblingASTsAcrossCalls proves the sibling
// cache is genuinely reused, not rebuilt on every lookup, by loading it once
// from real files on disk and then removing the sibling file before the
// second call: if resolveGenericTypeDecl reloaded from disk instead of
// reusing ip.siblingASTs, the second call would find nothing and return nil.
func TestResolveGenericTypeDecl_CachesSiblingASTsAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.go")
	sibling := filepath.Join(dir, "sibling.go")
	require.NoError(t, os.WriteFile(target, []byte("package main\nfunc (g *GenStruct[T]) M() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(sibling, []byte("package main\ntype GenStruct[T comparable] struct{}\n"), 0o644))

	absTarget, err := filepath.Abs(target)
	require.NoError(t, err)

	targetFile, err := ast.ParseFileFast(target)
	require.NoError(t, err)

	ip := newTestPhase()
	ip.target = targetFile
	ip.targetPath = absTarget
	ip.compileArgs = []string{target, sibling}

	require.Nil(t, ip.siblingASTs, "precondition: cache must start empty so the first call performs a real load")
	params1, found1 := ip.resolveGenericTypeDecl("GenStruct")
	require.NotNil(t, params1)
	require.NotNil(t, found1)
	require.NotNil(t, ip.siblingASTs, "first call should have populated the cache")

	// Remove the sibling file. A second call that (incorrectly) reloads from
	// disk would find nothing; a second call that reuses the cache still
	// succeeds.
	require.NoError(t, os.Remove(sibling))

	params2, found2 := ip.resolveGenericTypeDecl("GenStruct")
	assert.NotNil(t, params2, "second call should still succeed by reusing the cached sibling ASTs")
	assert.NotNil(t, found2)
}

// TestResolveGenericTypeDecl_FindsDeclarationInEarlierProcessedFile is a
// regression test for a real bug: instrument() processes a package's files in
// one shared *instrumentPhase, calling parseFile (which updates ip.target and
// ip.targetPath) once per file. If the sibling cache were built excluding
// whichever file happened to be "current" at the moment of the first
// cross-file lookup, that exclusion would stick for the rest of the package's
// instrumentation -- a type declared in an earlier-processed file would
// become permanently unfindable from every later file, silently degrading to
// any exactly like the bug this package exists to fix. This reproduces that
// exact sequence: a lookup miss while processing the file that will later
// hold the real declaration, followed by a lookup for it from a different
// file.
func TestResolveGenericTypeDecl_FindsDeclarationInEarlierProcessedFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	require.NoError(t, os.WriteFile(a,
		[]byte("package main\ntype Box[T comparable] struct{}\nfunc (o *Other[T]) X() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("package main\nfunc (g *Box[T]) M() {}\n"), 0o644))

	ip := newTestPhase()
	ip.compileArgs = []string{a, b}

	// Process a.go first, exactly like instrument()'s per-file loop.
	_, err := ip.parseFile(a)
	require.NoError(t, err)
	// A cross-file miss here (looking up a type that doesn't exist anywhere)
	// is what used to permanently seed the cache without a.go in it.
	_, found := ip.resolveGenericTypeDecl("Other")
	assert.Nil(t, found)

	// Now process b.go, whose method's receiver type is declared back in a.go.
	_, err = ip.parseFile(b)
	require.NoError(t, err)

	params, declFile := ip.resolveGenericTypeDecl("Box")
	require.NotNil(t, params, "Box[T comparable], declared in a.go, must still be found while processing b.go")
	require.NotNil(t, declFile)
	constraint, ok := params.List[0].Type.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "comparable", constraint.Name)
}

// TestResolveGenericTypeDecl_NotFoundAnywhere locks in the graceful
// whole-package fallback: when the type isn't declared in ip.target or in any
// sibling file, resolveGenericTypeDecl must return (nil, nil) rather than an
// error, so callers fall back to any exactly as they did before cross-file
// search existed.
func TestResolveGenericTypeDecl_NotFoundAnywhere(t *testing.T) {
	methodFile, _ := parseReceiverType(t, "*GenStruct[T]")

	parser := ast.NewAstParser()
	unrelated, err := parser.ParseSource("package main\n\ntype SomethingElse[T any] struct{}\n")
	require.NoError(t, err)

	ip := newTestPhase()
	ip.target = methodFile
	ip.siblingASTs = map[string]*dst.File{"other.go": unrelated}

	params, found := ip.resolveGenericTypeDecl("GenStruct")

	assert.Nil(t, params)
	assert.Nil(t, found)
}

// TestExtractReceiverTypeParamsConstraint_CrossFileRecovered exercises the
// full extractReceiverTypeParams path (not just resolveGenericTypeDecl
// directly) against a cross-file declaration, confirming the recovered
// constraint is the real one, not any.
func TestExtractReceiverTypeParamsConstraint_CrossFileRecovered(t *testing.T) {
	methodFile, recvType := parseReceiverType(t, "*GenStruct[T]")

	parser := ast.NewAstParser()
	declFile, err := parser.ParseSource("package main\n\ntype GenStruct[T comparable] struct{}\n")
	require.NoError(t, err)

	ip := newTestPhase()
	ip.target = methodFile
	ip.siblingASTs = map[string]*dst.File{"decl.go": declFile}

	params, err := ip.extractReceiverTypeParams(t.Context(), recvType)
	require.NoError(t, err)
	require.NotNil(t, params)
	require.Len(t, params.List, 1)

	constraint, ok := params.List[0].Type.(*dst.Ident)
	require.True(t, ok, "expected the constraint to be a plain identifier")
	assert.Equal(t, "comparable", constraint.Name)
}

// funcDeclFromSource parses src and returns its enclosing file plus the first
// top-level func declaration found in it.
func funcDeclFromSource(t *testing.T, src string) (*dst.File, *dst.FuncDecl) {
	t.Helper()
	parser := ast.NewAstParser()
	file, err := parser.ParseSource(src)
	require.NoError(t, err)
	for _, decl := range file.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok {
			return file, fn
		}
	}
	require.Fail(t, "no function declaration found in source")
	return nil, nil
}

// TestFindTargetGenericType covers every combination findTargetGenericType
// assembles a trampoline's type-parameter list from: no generics at all,
// receiver-only generics, method-own generics with no receiver, and the
// combined case (a generic receiver whose method also declares its own type
// parameter, e.g. func (c *Type1[K]) Target[V any]() V) -- the one branch of
// this function that had no test coverage of any kind before this change.
func TestFindTargetGenericType(t *testing.T) {
	t.Run("no receiver, no method type params", func(t *testing.T) {
		file, fn := funcDeclFromSource(t, "package main\nfunc Plain(a int) {}")
		ip := newTestPhase()
		ip.target = file

		params, err := ip.findTargetGenericType(t.Context(), fn)
		require.NoError(t, err)
		assert.Nil(t, params)
	})

	t.Run("receiver-only generics", func(t *testing.T) {
		file, fn := funcDeclFromSource(t,
			"package main\ntype GenStruct[T comparable] struct{}\nfunc (g *GenStruct[T]) M() {}")
		ip := newTestPhase()
		ip.target = file

		params, err := ip.findTargetGenericType(t.Context(), fn)
		require.NoError(t, err)
		require.NotNil(t, params)
		assert.Equal(t, []string{"T"}, typeParamNames(t, params))
	})

	t.Run("method-own type params only, no receiver", func(t *testing.T) {
		file, fn := funcDeclFromSource(t, "package main\nfunc Generic[V any](v V) V { return v }")
		ip := newTestPhase()
		ip.target = file

		params, err := ip.findTargetGenericType(t.Context(), fn)
		require.NoError(t, err)
		require.NotNil(t, params)
		assert.Equal(t, []string{"V"}, typeParamNames(t, params))
	})

	t.Run("combined: receiver generics plus the method's own type params", func(t *testing.T) {
		file, fn := funcDeclFromSource(t,
			"package main\n"+
				"type Type1[K comparable] struct{}\n"+
				"func (c *Type1[K]) Target(v K) {}")
		// Give Target its own type parameter in addition to the receiver's,
		// mirroring func (c *Type1[K]) Target[V any]() V from the doc comment --
		// built directly since the parser's receiver-vs-method type param
		// split is what's under test, not the source syntax for declaring both.
		fn.Type.TypeParams = &dst.FieldList{List: []*dst.Field{{
			Names: []*dst.Ident{ast.Ident("V")},
			Type:  ast.Ident("any"),
		}}}
		ip := newTestPhase()
		ip.target = file

		params, err := ip.findTargetGenericType(t.Context(), fn)
		require.NoError(t, err)
		require.NotNil(t, params)
		// Receiver's own type params come first, then the method's, per the
		// combining logic in findTargetGenericType.
		require.Len(t, params.List, 2)
		assert.Equal(t, []string{"K", "V"}, typeParamNames(t, params))
		kConstraint, ok := params.List[0].Type.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, "comparable", kConstraint.Name,
			"receiver constraint must still be recovered in the combined case")
		vConstraint, ok := params.List[1].Type.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, "any", vConstraint.Name)
	})

	t.Run("propagates an error from constraint import injection instead of swallowing it", func(t *testing.T) {
		methodFile, fn := funcDeclFromSource(t, "package main\nfunc (g *GenStruct[T]) M() {}")
		declFile, err := ast.NewAstParser().ParseSource(
			"package main\nimport \"fmt\"\ntype GenStruct[T fmt.Stringer] struct{}")
		require.NoError(t, err)

		ip := newTestPhase()
		// The target file already imports a different path under the alias
		// "fmt", conflicting with what the cross-file constraint needs.
		ip.target = fileWithImport("fmt", "some/other/package")
		ip.target.Decls = append(ip.target.Decls, methodFile.Decls...)
		ip.siblingASTs = map[string]*dst.File{"decl.go": declFile}

		_, err = ip.findTargetGenericType(t.Context(), fn)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "import conflict")
	})
}

// TestBuildTrampSignature covers the one place findTargetGenericType's result
// is actually consumed for codegen: the trampoline function's own
// Type.TypeParams, plus the dereferencing every parameter field goes through
// regardless of genericity.
func TestBuildTrampSignature(t *testing.T) {
	t.Run("generic receiver: type params attached, params dereferenced", func(t *testing.T) {
		file, fn := funcDeclFromSource(t,
			"package main\ntype GenStruct[T comparable] struct{}\nfunc (g *GenStruct[T]) M(p1 T) {}")
		ip := newTestPhase()
		ip.target = file
		ip.targetFunc = fn
		ip.beforeTrampFunc = &dst.FuncDecl{Type: &dst.FuncType{}}

		genericTypes, err := ip.findTargetGenericType(t.Context(), fn)
		require.NoError(t, err)

		ip.buildTrampSignature(genericTypes, trampolineBefore)

		require.NotNil(t, ip.beforeTrampFunc.Type.TypeParams)
		assert.Equal(t, []string{"T"}, typeParamNames(t, ip.beforeTrampFunc.Type.TypeParams))
		constraint, ok := ip.beforeTrampFunc.Type.TypeParams.List[0].Type.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, "comparable", constraint.Name)
		// The assigned TypeParams must be an independent clone, not the same
		// node genericTypes points at -- buildTrampSignature is called once per
		// trampoline (Before and After), and each needs to own its own AST
		// subtree rather than share one.
		assert.NotSame(t, genericTypes, ip.beforeTrampFunc.Type.TypeParams)

		// Every param field (receiver included) must come out pointer-wrapped so
		// the trampoline can address/modify the target function's own values.
		for _, field := range ip.beforeTrampFunc.Type.Params.List {
			_, isPointer := field.Type.(*dst.StarExpr)
			assert.True(t, isPointer, "field %v should be dereferenced to a pointer type", field.Names)
		}
	})

	t.Run("non-generic: no type params attached", func(t *testing.T) {
		file, fn := funcDeclFromSource(t, "package main\nfunc Plain(a int) {}")
		ip := newTestPhase()
		ip.target = file
		ip.targetFunc = fn
		ip.afterTrampFunc = &dst.FuncDecl{Type: &dst.FuncType{}}

		genericTypes, err := ip.findTargetGenericType(t.Context(), fn)
		require.NoError(t, err)
		require.Nil(t, genericTypes)

		ip.buildTrampSignature(genericTypes, trampolineAfter)
		assert.Nil(t, ip.afterTrampFunc.Type.TypeParams)
	})
}

// TestRewriteHookContextMethods_GenericPanicsAndErrorPropagation covers the
// generic-vs-non-generic branch directly (rather than only through golden
// fixtures) and, more importantly, that an error surfaced while resolving the
// target's generic type (e.g. a conflicting import needed by a cross-file
// constraint. genericTypes is resolved directly via findTargetGenericType
// rather than by this function -- see createTrampoline's doc comment for why
// -- so this only covers the generic-vs-non-generic branch, not error
// propagation; TestCreateTrampoline_PropagatesGenericTypeResolutionError
// covers the error path at the level where it actually now surfaces.
func TestRewriteHookContextMethods_GenericPanics(t *testing.T) {
	newMaterializedPhase := func(t *testing.T) *instrumentPhase {
		t.Helper()
		ip := newTestPhase()
		ip.target = &dst.File{}
		require.NoError(t, ip.materializeTemplate())
		return ip
	}

	findMethod := func(t *testing.T, ip *instrumentPhase, name string) *dst.FuncDecl {
		t.Helper()
		for _, decl := range ip.hookCtxMethods {
			if decl.Name.Name == name {
				return decl
			}
		}
		require.Failf(t, "method not found", "%s not present in hookCtxMethods", name)
		return nil
	}

	t.Run("generic target: accessor methods become panics", func(t *testing.T) {
		ip := newMaterializedPhase(t)
		src, fn := funcDeclFromSource(t,
			"package main\ntype GenStruct[T comparable] struct{}\nfunc (g *GenStruct[T]) M() {}")
		ip.targetFunc = fn
		// The generic-receiver lookup needs its declaration reachable from
		// ip.target; materializeTemplate already populated ip.target with the
		// trampoline template's own decls, so append the source's decls too.
		ip.target.Decls = append(ip.target.Decls, src.Decls...)

		genericTypes, err := ip.findTargetGenericType(t.Context(), fn)
		require.NoError(t, err)
		require.NotNil(t, genericTypes)

		ip.rewriteHookContextMethods(genericTypes)

		getParam := findMethod(t, ip, trampolineGetParamName)
		require.Len(t, getParam.Body.List, 1)
		_, isPanic := getParam.Body.List[0].(*dst.ExprStmt)
		assert.True(t, isPanic, "GetParam body should have been replaced with a single panic statement")
	})

	t.Run("non-generic target: accessor methods keep their real bodies", func(t *testing.T) {
		ip := newMaterializedPhase(t)
		_, fn := funcDeclFromSource(t, "package main\nfunc Plain(a int) (int, error) { return a, nil }")
		ip.targetFunc = fn

		genericTypes, err := ip.findTargetGenericType(t.Context(), fn)
		require.NoError(t, err)
		require.Nil(t, genericTypes)

		ip.rewriteHookContextMethods(genericTypes)

		getParam := findMethod(t, ip, trampolineGetParamName)
		require.Greater(t, len(getParam.Body.List), 1,
			"a non-generic target must keep GetParam's real switch-based body, not a bare panic")
	})
}

// TestCreateTrampoline_PropagatesGenericTypeResolutionError proves the
// error-propagation contract this whole restructuring depends on: genericTypes
// is now resolved exactly once, at the top of createTrampoline, and every
// consumer (rewriteHookContextMethods, buildTrampSignature for both Before and
// After, buildHookSignature via callHookFunc) is handed the already-resolved
// value instead of re-deriving it. This confirms a resolution failure (e.g. a
// cross-file constraint's import conflicting with one already in ip.target)
// surfaces from createTrampoline itself, before any of those consumers run.
func TestCreateTrampoline_PropagatesGenericTypeResolutionError(t *testing.T) {
	ip := newTestPhase()
	ip.target = &dst.File{}
	methodFile, fn := funcDeclFromSource(t, "package main\nfunc (g *GenStruct[T]) M() {}")
	declFile, err := ast.NewAstParser().ParseSource(
		"package main\nimport \"fmt\"\ntype GenStruct[T fmt.Stringer] struct{}")
	require.NoError(t, err)

	// ip.target already carries a conflicting "fmt" alias from a prior rule
	// application on this file.
	ip.target.Decls = append(ip.target.Decls, fileWithImport("fmt", "some/other/package").Decls...)
	ip.target.Decls = append(ip.target.Decls, methodFile.Decls...)
	ip.siblingASTs = map[string]*dst.File{"decl.go": declFile}
	ip.targetFunc = fn

	err = ip.createTrampoline(t.Context(), &rule.InstFuncRule{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import conflict")
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

	ip := newTestPhase()
	ip.target = file
	params, err := ip.extractReceiverTypeParams(t.Context(), nested)
	require.NoError(t, err)

	require.NotNil(t, params)
	assert.Equal(t, []string{"T"}, typeParamNames(t, params))
}
