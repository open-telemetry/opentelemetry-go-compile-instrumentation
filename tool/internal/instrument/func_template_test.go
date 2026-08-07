// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
)

func TestFuncTemplateData_FuncName(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	data := newFuncTemplateData(funcDecl, nil)

	assert.Equal(t, "Foo", data.FuncName())
}

func TestFuncTemplateData_FuncArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(ctx int, name string) {}")
	data := newFuncTemplateData(funcDecl, nil)

	v, err := data.FuncArgument(0)
	require.NoError(t, err)
	assert.Equal(t, "ctx", v)

	v, err = data.FuncArgument(1)
	require.NoError(t, err)
	assert.Equal(t, "name", v)
}

func TestFuncTemplateData_FuncArgumentExcludesReceiver(t *testing.T) {
	funcDecl := parseFunc(t, "package main\ntype T struct{}\nfunc (t T) Foo(x int) {}")
	data := newFuncTemplateData(funcDecl, nil)

	v, err := data.FuncArgument(0)
	require.NoError(t, err)
	assert.Equal(t, "x", v)
}

func TestFuncTemplateData_VariadicArgument(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a string, b ...int) {}")
	data := newFuncTemplateData(funcDecl, nil)

	v, err := data.FuncArgument(0)
	require.NoError(t, err)
	assert.Equal(t, "a", v)

	v, err = data.FuncArgument(1)
	require.NoError(t, err)
	assert.Equal(t, "b", v)

	assert.Equal(t, 2, data.FuncArgumentCount())
}

func TestFuncTemplateData_FuncReturn(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() (int, error) { return 0, nil }")
	data := newFuncTemplateData(funcDecl, nil)

	v, err := data.FuncReturn(0)
	require.NoError(t, err)
	assert.Equal(t, "_unnamedRetVal0", v)

	v, err = data.FuncReturn(1)
	require.NoError(t, err)
	assert.Equal(t, "_unnamedRetVal1", v)
}

func TestFuncTemplateData_Counts(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a, b int) (int, error) { return 0, nil }")
	data := newFuncTemplateData(funcDecl, nil)

	assert.Equal(t, 2, data.FuncArgumentCount())
	assert.Equal(t, 2, data.FuncReturnCount())
}

func TestFuncTemplateData_FuncArgumentOutOfRange(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")
	data := newFuncTemplateData(funcDecl, nil)

	_, err := data.FuncArgument(1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestFuncTemplateData_FuncReturnOutOfRange(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	data := newFuncTemplateData(funcDecl, nil)

	_, err := data.FuncReturn(0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestFuncTemplateData_FuncArgumentOfType(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nimport \"context\"\nfunc Foo(ctx context.Context, name string) {}")
	data := newFuncTemplateData(funcDecl, nil)

	v, err := data.FuncArgumentOfType("context.Context")
	require.NoError(t, err)
	assert.Equal(t, "ctx", v)

	v, err = data.FuncArgumentOfType("io.Reader")
	require.NoError(t, err)
	assert.Empty(t, v)
}

func TestFuncTemplateData_FuncArgumentOfTypeExcludesReceiver(t *testing.T) {
	funcDecl := parseFunc(t, "package main\ntype T struct{}\nfunc (t T) Foo(x int) {}")
	data := newFuncTemplateData(funcDecl, nil)

	v, err := data.FuncArgumentOfType("T")
	require.NoError(t, err)
	assert.Empty(t, v, "the receiver is not part of FuncArgumentOfType's search space")

	v, err = data.FuncArgumentOfType("int")
	require.NoError(t, err)
	assert.Equal(t, "x", v)
}

func TestFuncTemplateData_FuncArgumentOfTypeInvalidType(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo(a int) {}")
	data := newFuncTemplateData(funcDecl, nil)

	_, err := data.FuncArgumentOfType("[]invalid")
	require.Error(t, err)
}

func TestFuncTemplateData_FuncReturnOfType(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() (int, error) { return 0, nil }")
	data := newFuncTemplateData(funcDecl, nil)

	v, err := data.FuncReturnOfType("error")
	require.NoError(t, err)
	assert.Equal(t, "_unnamedRetVal1", v)

	v, err = data.FuncReturnOfType("string")
	require.NoError(t, err)
	assert.Empty(t, v)
}

func TestFuncTemplateData_DirectiveArgs(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	args := []ast.DirectiveArg{{Key: "span.name", Value: "my-op"}, {Key: "tag", Value: "v1"}}
	data := newFuncTemplateData(funcDecl, args)

	assert.Equal(t, args, data.DirectiveArgs())
	assert.Equal(t, "my-op", data.DirectiveArg("span.name"))
	assert.Equal(t, "v1", data.DirectiveArg("tag"))
	assert.Empty(t, data.DirectiveArg("missing"))
}

func TestFuncTemplateData_DirectiveArgsEmpty(t *testing.T) {
	funcDecl := parseFunc(t, "package main\nfunc Foo() {}")
	data := newFuncTemplateData(funcDecl, nil)

	assert.Empty(t, data.DirectiveArgs())
	assert.Empty(t, data.DirectiveArg("span.name"))
}
