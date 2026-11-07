// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"errors"
	goast "go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/dave/dst"
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

	t.Run("parses snippet ending in a line comment", func(t *testing.T) {
		stmts, err := p.ParseSnippet("x := 1 // trailing comment")
		require.NoError(t, err)
		assert.Len(t, stmts, 1)
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
		assert.Equal(t, token.Position{}, pos)
		assert.False(t, pos.IsValid())
	})
}

func TestOriginalPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orig.go")
	src := "package main\n\nfunc Foo() {\n\tbar(1)\n}\n"
	require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	p := NewAstParser()
	file, err := p.Parse(path, parser.ParseComments)
	require.NoError(t, err)
	fn := FindFuncDeclWithoutRecv(file, "Foo")
	require.NotNil(t, fn)
	call := fn.Body.List[0]

	t.Run("node of the parsed file", func(t *testing.T) {
		pos, ok := p.OriginalPosition(call)
		require.True(t, ok)
		assert.Equal(t, "orig.go", pos.Filename)
		assert.Equal(t, 4, pos.Line)
		assert.Equal(t, 2, pos.Column)
	})

	t.Run("closing brace of a block", func(t *testing.T) {
		pos, ok := p.Rbrace(fn.Body)
		require.True(t, ok)
		assert.Equal(t, 5, pos.Line)
		assert.Equal(t, 1, pos.Column)
	})

	t.Run("node that is not in a map", func(t *testing.T) {
		_, ok := p.OriginalPosition(Ident("orphan"))
		assert.False(t, ok)
		_, ok = p.Rbrace(&dst.BlockStmt{})
		assert.False(t, ok)
	})

	t.Run("snippet of the same parser", func(t *testing.T) {
		// The snippet has its own file in the same file set.
		stmts, snippetErr := p.ParseSnippet("baz()")
		require.NoError(t, snippetErr)
		_, ok := p.OriginalPosition(stmts[0])
		assert.False(t, ok)
	})

	t.Run("source of the same parser that Parse did not read", func(t *testing.T) {
		// ParseSource uses the same decorator, so the node is in the map. But
		// its position is in a file that Parse did not read.
		other, srcErr := p.ParseSource("package main\n\nfunc Other() {}\n")
		require.NoError(t, srcErr)
		otherFn := FindFuncDeclWithoutRecv(other, "Other")
		require.NotNil(t, otherFn)
		_, ok := p.OriginalPosition(otherFn)
		assert.False(t, ok)
		_, ok = p.Rbrace(otherFn.Body)
		assert.False(t, ok)
	})

	t.Run("node without a position", func(t *testing.T) {
		node := Ident("noPos")
		p.dec.Ast.Nodes[node] = &goast.Ident{Name: "noPos"}
		_, ok := p.OriginalPosition(node)
		assert.False(t, ok)
	})

	t.Run("source with a line directive", func(t *testing.T) {
		lineFile := filepath.Join(t.TempDir(), "gen.go")
		lineSrc := "package main\n\n//line other.go:100\nfunc Foo() {\n\tbar(1)\n}\n"
		require.NoError(t, os.WriteFile(lineFile, []byte(lineSrc), 0o600))
		lineParser := NewAstParser()
		lineRoot, parseErr := lineParser.Parse(lineFile, parser.ParseComments)
		require.NoError(t, parseErr)
		pos, ok := lineParser.OriginalPosition(FindFuncDeclWithoutRecv(lineRoot, "Foo").Body.List[0])
		require.True(t, ok)
		assert.Equal(t, "other.go", pos.Filename)
		assert.Equal(t, 101, pos.Line)
	})
}

// findCallExpr returns the first *dst.CallExpr for a call to funcName in root.
func findCallExpr(t *testing.T, root dst.Node, funcName string) *dst.CallExpr {
	t.Helper()
	var found *dst.CallExpr
	dst.Inspect(root, func(n dst.Node) bool {
		if found != nil {
			return false
		}
		call, ok := n.(*dst.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*dst.Ident)
		if !ok || ident.Name != funcName {
			return true
		}
		found = call
		return false
	})
	require.NotNil(t, found, "no call to %s found", funcName)
	return found
}

func TestParsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "orig.go")
	require.NoError(t, os.WriteFile(path, []byte("package main\n"), 0o600))
	p := NewAstParser()
	file, err := p.Parse(path, parser.ParseComments)
	require.NoError(t, err)
	assert.True(t, p.Parsed(file))

	// The same file, read by another parser.
	assert.False(t, NewAstParser().Parsed(file))
	// A file that ParseSource parsed.
	source, err := p.ParseSource("package main\n")
	require.NoError(t, err)
	assert.False(t, p.Parsed(source))
	// A file that has only the package clause.
	clause, err := p.Parse(path, parser.PackageClauseOnly)
	require.NoError(t, err)
	assert.False(t, p.Parsed(clause))
}

func TestPropagatePositions(t *testing.T) {
	p := NewAstParser()
	file, err := p.ParseSource("package main\n\nfunc Foo() {\n\tbar(1)\n}\n")
	require.NoError(t, err)

	orig := findCallExpr(t, file, "bar")
	origPos := p.FindPosition(orig)
	require.True(t, origPos.IsValid(), "orig call must have a known source position")

	cloned, ok := dst.Clone(orig).(*dst.CallExpr)
	require.True(t, ok)
	clonedPos := p.FindPosition(cloned)
	require.False(t, clonedPos.IsValid(), "a freshly cloned node starts out unmapped")

	p.PropagatePositions(orig, cloned)

	assert.Equal(t, origPos, p.FindPosition(cloned), "cloned node must inherit orig's position")
	// Nested nodes (e.g. the argument) must be propagated too.
	origArg := orig.Args[0]
	clonedArg := cloned.Args[0]
	assert.Equal(t, p.FindPosition(origArg), p.FindPosition(clonedArg))
}

func TestPropagatePositions_ShapeMismatchIsNoop(t *testing.T) {
	p := NewAstParser()
	file, err := p.ParseSource("package main\n\nfunc Foo() {\n\tbar(1)\n}\n")
	require.NoError(t, err)

	orig := findCallExpr(t, file, "bar")

	// cloned has a different node count than orig (an extra argument), so
	// PropagatePositions must bail out without mapping any position.
	cloned := &dst.CallExpr{
		Fun: dst.NewIdent("bar"),
		Args: []dst.Expr{
			&dst.BasicLit{Kind: token.INT, Value: "1"},
			&dst.BasicLit{Kind: token.INT, Value: "2"},
		},
	}

	p.PropagatePositions(orig, cloned)

	pos := p.FindPosition(cloned)
	assert.False(t, pos.IsValid(),
		"a shape mismatch between orig and cloned must leave the clone's positions unmapped")
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

func TestWriteFileAtomic_WriteError(t *testing.T) {
	p := NewAstParser()
	file, err := p.ParseSource("package main\n\nfunc Bar() {}\n")
	require.NoError(t, err)

	badPath := filepath.Join(t.TempDir(), "missing-dir", "atomic.go")
	require.Error(t, WriteFileAtomic(badPath, file))
}

// unprintableFile returns an AST that the restorer cannot turn back into
// source: go/format re-parses files with grouped imports, and a function
// named "1bad" does not parse.
func unprintableFile(t *testing.T) *dst.File {
	t.Helper()

	p := NewAstParser()
	file, err := p.ParseSource("package main\n\nimport (\n\t\"fmt\"\n)\n\nfunc Foo() { fmt.Println() }\n")
	require.NoError(t, err)

	FindFuncDeclWithoutRecv(file, "Foo").Name = dst.NewIdent("1bad")
	return file
}

func TestRenderFile_RestoreError(t *testing.T) {
	data, err := RenderFile(unprintableFile(t))
	require.ErrorContains(t, err, "failed to restore AST")
	assert.Nil(t, data)
}

func TestWriteFileAtomic_RestoreError(t *testing.T) {
	out := filepath.Join(t.TempDir(), "atomic.go")

	err := WriteFileAtomic(out, unprintableFile(t))
	require.ErrorContains(t, err, "failed to restore AST")
	require.ErrorContains(t, err, out)
	assert.NoFileExists(t, out)
}

func TestParseAst(t *testing.T) {
	_, err := ParseFile("parser_test.go")
	require.NoError(t, err)
}

func TestParsePackageName(t *testing.T) {
	name, err := ParsePackageName("parser_test.go")
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
	f, err := ParseFile("parser_test.go")
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
	f, err := ParseFile("parser_test.go")
	require.NoError(t, err)

	err = WriteFile(filepath.Join(t.TempDir(), "nonexistent", "out.go"), f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create file")
}

type mockWriteCloser struct {
	writeErr     error
	closeErr     error
	panicOnWrite bool
	closed       bool
}

func (m *mockWriteCloser) Write(p []byte) (int, error) {
	if m.panicOnWrite {
		panic("simulated write panic")
	}
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(p), nil
}

func (m *mockWriteCloser) Close() error {
	m.closed = true
	return m.closeErr
}

func TestWriteFile_WriteError(t *testing.T) {
	f, err := ParseFile("parser_test.go")
	require.NoError(t, err)

	mock := &mockWriteCloser{writeErr: errors.New("disk write error")}
	err = writeFile(mock, "out.go", f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write to file")
}

func TestWriteFile_CloseError(t *testing.T) {
	f, err := ParseFile("parser_test.go")
	require.NoError(t, err)

	mock := &mockWriteCloser{closeErr: errors.New("flush close error")}
	err = writeFile(mock, "out.go", f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to close file")
}

func TestWriteFile_PanicSafety(t *testing.T) {
	f, err := ParseFile("parser_test.go")
	require.NoError(t, err)

	mock := &mockWriteCloser{panicOnWrite: true}
	require.Panics(t, func() { _ = writeFile(mock, "out.go", f) })
	assert.True(t, mock.closed, "the file must be closed even when writing panics")
}

func TestRenderFile(t *testing.T) {
	p := NewAstParser()
	file, err := p.ParseSource("package main\n\nfunc Bar() {}\n")
	require.NoError(t, err)

	buf, err := RenderFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(buf), "func Bar()")
}
