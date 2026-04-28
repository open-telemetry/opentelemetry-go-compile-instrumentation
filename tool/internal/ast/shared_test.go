// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture source used across all shared_test cases
const sharedTestSource = `package main

var GlobalVar = "original"

const MaxRetries = 3

type MyStruct struct{ x int }

func TopLevel(a, b int) int { return a + b }

func (s *MyStruct) Method() {}
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
	decls := ListFuncDecls(file)
	require.Len(t, decls, 2)
	names := []string{decls[0].Name.Name, decls[1].Name.Name}
	assert.Contains(t, names, "TopLevel")
	assert.Contains(t, names, "Method")
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

func TestFindVarDecl(t *testing.T) {
	file := parseSharedFixture(t)

	t.Run("finds existing var", func(t *testing.T) {
		genDecl, spec := FindVarDecl(file, "GlobalVar")
		require.NotNil(t, genDecl)
		require.NotNil(t, spec)
		assert.Equal(t, "GlobalVar", spec.Names[0].Name)
	})

	t.Run("does not find const as var", func(t *testing.T) {
		genDecl, spec := FindVarDecl(file, "MaxRetries")
		assert.Nil(t, genDecl)
		assert.Nil(t, spec)
	})

	t.Run("not found returns nil pair", func(t *testing.T) {
		genDecl, spec := FindVarDecl(file, "Unknown")
		assert.Nil(t, genDecl)
		assert.Nil(t, spec)
	})
}

func TestFindConstDecl(t *testing.T) {
	file := parseSharedFixture(t)

	t.Run("finds existing const", func(t *testing.T) {
		genDecl, spec := FindConstDecl(file, "MaxRetries")
		require.NotNil(t, genDecl)
		require.NotNil(t, spec)
		assert.Equal(t, "MaxRetries", spec.Names[0].Name)
	})

	t.Run("does not find var as const", func(t *testing.T) {
		genDecl, spec := FindConstDecl(file, "GlobalVar")
		assert.Nil(t, genDecl)
		assert.Nil(t, spec)
	})

	t.Run("not found returns nil pair", func(t *testing.T) {
		genDecl, spec := FindConstDecl(file, "Unknown")
		assert.Nil(t, genDecl)
		assert.Nil(t, spec)
	})
}

func TestFindTypeDecl(t *testing.T) {
	file := parseSharedFixture(t)

	t.Run("finds existing type", func(t *testing.T) {
		decl := FindTypeDecl(file, "MyStruct")
		require.NotNil(t, decl)
	})

	t.Run("not found returns nil", func(t *testing.T) {
		decl := FindTypeDecl(file, "NoSuchType")
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
