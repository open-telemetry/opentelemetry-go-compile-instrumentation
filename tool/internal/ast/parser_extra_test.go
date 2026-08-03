// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSnippet(t *testing.T) {
	p := NewAstParser()

	t.Run("parses statements", func(t *testing.T) {
		stmts, err := p.ParseSnippet("x := 1; y := x + 2")
		require.NoError(t, err)
		assert.Len(t, stmts, 2)
	})

	t.Run("rejects empty source", func(t *testing.T) {
		_, err := p.ParseSnippet("")
		require.Error(t, err)
	})

	t.Run("rejects invalid source", func(t *testing.T) {
		_, err := p.ParseSnippet("this is not go")
		require.Error(t, err)
	})
}

func TestFindPosition(t *testing.T) {
	p := NewAstParser()
	file, err := p.ParseSource("package main\n\nfunc Foo() {}\n")
	require.NoError(t, err)

	t.Run("known node has a valid position", func(t *testing.T) {
		fn := FindFuncDeclWithoutRecv(file, "Foo")
		require.NotNil(t, fn)
		pos := p.FindPosition(fn)
		assert.Positive(t, pos.Line)
	})

	t.Run("unknown node returns invalid position", func(t *testing.T) {
		// A node the decorator never saw maps to no AST node.
		pos := p.FindPosition(Ident("orphan"))
		assert.Equal(t, -1, pos.Line)
		assert.Equal(t, -1, pos.Column)
	})
}

func TestWriteFile(t *testing.T) {
	p := NewAstParser()
	file, err := p.ParseSource("package main\n\nfunc Foo() {}\n")
	require.NoError(t, err)

	out := filepath.Join(t.TempDir(), "out.go")
	require.NoError(t, WriteFile(out, file))

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(data), "package main")
	assert.Contains(t, string(data), "func Foo()")
}

func TestWriteFileReturnsErrorForBadPath(t *testing.T) {
	p := NewAstParser()
	file, err := p.ParseSource("package main\n")
	require.NoError(t, err)

	// A path inside a nonexistent directory cannot be created.
	bad := filepath.Join(t.TempDir(), "missing-dir", "out.go")
	require.Error(t, WriteFile(bad, file))
}

func TestWriteFileAtomic(t *testing.T) {
	p := NewAstParser()
	file, err := p.ParseSource("package main\n\nfunc Bar() {}\n")
	require.NoError(t, err)

	out := filepath.Join(t.TempDir(), "atomic.go")
	require.NoError(t, WriteFileAtomic(out, file))

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(data), "func Bar()")
}

func TestFindFuncsByDirective(t *testing.T) {
	const src = `package p

//otelc:span
func Traced() {}

//otelc:span
func AlsoTraced() {}

// regular comment
func Plain() {}

//otelc:other
func Other() {}
`
	p := NewAstParser()
	file, err := p.ParseSource(src)
	require.NoError(t, err)

	funcs := FindFuncsByDirective(file, "otelc:span")
	names := make([]string, 0, len(funcs))
	for _, fn := range funcs {
		names = append(names, fn.Name.Name)
	}
	assert.ElementsMatch(t, []string{"Traced", "AlsoTraced"}, names)

	// A directive that matches nothing returns no functions.
	assert.Empty(t, FindFuncsByDirective(file, "otelc:none"))
}
