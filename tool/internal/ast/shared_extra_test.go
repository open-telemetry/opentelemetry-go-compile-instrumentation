// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestFindFuncDeclRawRule(t *testing.T) {
	file := parseSharedFixture(t)

	raw := &rule.InstRawRule{Func: "TopLevel"}
	fn, ok, err := FindFuncDecl(file, raw)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotNil(t, fn)
	assert.Equal(t, "TopLevel", fn.Name.Name)
}

func TestFindFuncDeclRawRuleMissing(t *testing.T) {
	file := parseSharedFixture(t)

	raw := &rule.InstRawRule{Func: "Missing"}
	fn, ok, err := FindFuncDecl(file, raw)
	require.NoError(t, err)
	require.False(t, ok)
	assert.Nil(t, fn)
}

func TestFindFuncDeclFilterDef(t *testing.T) {
	file := parseSharedFixture(t)

	filter := &rule.FilterDef{HasFunc: "TopLevel"}
	fn, ok, err := FindFuncDecl(file, filter)
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
	}, nil)
	require.Error(t, err)

	_, err = funcDeclMatchesFilters(decl, &rule.InstFuncRule{
		SignatureContains: &rule.FuncSignature{Args: []string{"[]invalid"}},
	}, nil)
	require.Error(t, err)

	_, err = funcDeclMatchesFilters(decl, &rule.InstFuncRule{
		SignatureContains: &rule.FuncSignature{Returns: []string{"[]invalid"}},
	}, nil)
	require.Error(t, err)
}

func TestFuncDeclMatchesFilters_LastResultNoResults(t *testing.T) {
	decl := makeFuncDecl(nil, nil)

	ok, err := funcDeclMatchesFilters(decl, &rule.InstFuncRule{LastResult: "error"}, nil)
	require.NoError(t, err)
	assert.False(t, ok)
}
