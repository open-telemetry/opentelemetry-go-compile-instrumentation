// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"errors"
	"go/parser"
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

func TestParsePackageName(t *testing.T) {
	name, err := ParsePackageName("ast_test.go")
	require.NoError(t, err)
	assert.Equal(t, "ast", name)
}

func TestParsePackageName_FileNotFound(t *testing.T) {
	_, err := ParsePackageName("/nonexistent/path/file.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")
}

func TestParsePackageName_InvalidGoFile(t *testing.T) {
	// Create a temp file with invalid Go syntax to trigger ParseFile error.
	tmpDir := t.TempDir()
	badFile := filepath.Join(tmpDir, "bad.go")
	require.NoError(t, os.WriteFile(badFile, []byte("this is not valid go"), 0o600))
	_, err := ParsePackageName(badFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse file")
}

// BenchmarkAstParserPackageClauseOnly benchmarks AstParser.Parse in
// PackageClauseOnly mode, which skips DST decoration.
func BenchmarkAstParserPackageClauseOnly(b *testing.B) {
	for range b.N {
		_, err := NewAstParser().Parse("parser.go", parser.PackageClauseOnly)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParsePackageName(b *testing.B) {
	for range b.N {
		_, err := ParsePackageName("parser.go")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestWriteFile_Basic(t *testing.T) {
	f, err := ParseFile("ast_test.go")
	require.NoError(t, err)

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.go")
	err = WriteFile(outPath, f)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "package ast")
}

func TestWriteFile_CreateError(t *testing.T) {
	f, err := ParseFile("ast_test.go")
	require.NoError(t, err)

	err = WriteFile(filepath.Join(t.TempDir(), "nonexistent", "out.go"), f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create file")
}

type mockWriteCloser struct {
	writeErr error
	closeErr error
}

func (m *mockWriteCloser) Write(p []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(p), nil
}

func (m *mockWriteCloser) Close() error {
	return m.closeErr
}

func TestWriteFile_WriteError(t *testing.T) {
	f, err := ParseFile("ast_test.go")
	require.NoError(t, err)

	mock := &mockWriteCloser{writeErr: errors.New("disk write error")}
	err = writeFile(mock, "out.go", f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write to file")
}

func TestWriteFile_CloseError(t *testing.T) {
	f, err := ParseFile("ast_test.go")
	require.NoError(t, err)

	mock := &mockWriteCloser{closeErr: errors.New("flush close error")}
	err = writeFile(mock, "out.go", f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close file")
}
