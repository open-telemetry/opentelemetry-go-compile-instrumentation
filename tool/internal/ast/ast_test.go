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

func TestParseAst(t *testing.T) {
	_, err := ParseFile("ast_test.go")
	require.NoError(t, err)
}

func TestParseFileFast(t *testing.T) {
	f, err := ParseFileFast("ast_test.go")
	require.NoError(t, err)
	require.NotNil(t, f)
	assert.Equal(t, "ast", f.Name.Name)
}

func TestParseFileOnlyPackage(t *testing.T) {
	f, err := ParseFileOnlyPackage("ast_test.go")
	require.NoError(t, err)
	require.NotNil(t, f)
	assert.Equal(t, "ast", f.Name.Name)
}

func TestParseSnippet(t *testing.T) {
	stmts, err := NewAstParser().ParseSnippet(`x := 1`)
	require.NoError(t, err)
	require.Len(t, stmts, 1)
}

func TestParseSnippet_Empty(t *testing.T) {
	_, err := NewAstParser().ParseSnippet(``)
	require.Error(t, err)
}

func TestParseSource(t *testing.T) {
	src := "package main\n\nfunc main() {}\n"
	f, err := NewAstParser().ParseSource(src)
	require.NoError(t, err)
	require.NotNil(t, f)
	assert.Equal(t, "main", f.Name.Name)
}

func TestParseSource_Empty(t *testing.T) {
	_, err := NewAstParser().ParseSource(``)
	require.Error(t, err)
}

func TestWriteFileAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.go")

	f, err := ParseFile("ast_test.go")
	require.NoError(t, err)

	err = WriteFileAtomic(outPath, f)
	require.NoError(t, err)

	_, statErr := os.Stat(outPath)
	require.NoError(t, statErr, "output file should exist after WriteFileAtomic")
}

func TestWriteFileAtomic_Error(t *testing.T) {
	// Invalid path (directory that doesn't exist) should cause an error.
	f, err := ParseFile("ast_test.go")
	require.NoError(t, err)

	err = WriteFileAtomic("/nonexistent-dir-xyz/out.go", f)
	require.Error(t, err)
}

func TestFindPosition(t *testing.T) {
	p := NewAstParser()
	f, err := p.Parse("ast_test.go", 0)
	require.NoError(t, err)
	require.NotNil(t, f)
	// The file node itself may not be in the position map; just verify no panic.
	pos := p.FindPosition(f)
	_ = pos
}

