// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package instrument

import (
	"bytes"
	"context"
	"fmt"
	"go/parser"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
)

// fileMarker stands for the absolute path of the original file in the expected
// output of the tests below.
const fileMarker = "FILE"

// dirMarker stands for the directory of the original file.
const dirMarker = "DIR"

// instrumentSource writes src to a file, parses it, calls edit, runs the line
// directive pass, and returns the printed code. The code has fileMarker in
// place of the path of the file.
func instrumentSource(t *testing.T, src string, edit func(root *dst.File)) string {
	t.Helper()
	return instrumentSourceAt(t, filepath.Join(t.TempDir(), "orig.go"), src, edit)
}

// instrumentSourceAt is like instrumentSource, but the file is at path file.
// The directory of file must exist.
func instrumentSourceAt(t *testing.T, file, src string, edit func(root *dst.File)) string {
	t.Helper()
	require.NoError(t, os.WriteFile(file, []byte(src), 0o600))
	p := ast.NewAstParser()
	root, err := p.Parse(file, parser.ParseComments)
	require.NoError(t, err)
	edit(root)
	markLineDirectives(p, root, file)
	out, err := ast.PrintFile(root)
	require.NoError(t, err)
	result := strings.ReplaceAll(string(out), file, fileMarker)
	return strings.ReplaceAll(result, filepath.Dir(file), dirMarker)
}

// snippet parses code like a raw rule does.
func snippet(t *testing.T, code string) []dst.Stmt {
	t.Helper()
	stmts, err := ast.NewAstParser().ParseSnippet(code)
	require.NoError(t, err)
	return stmts
}

// findFunc returns the declaration of the function name.
func findFunc(t *testing.T, root *dst.File, name string) *dst.FuncDecl {
	t.Helper()
	for _, decl := range root.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	require.Failf(t, "function not found", "name: %s", name)
	return nil
}

// prepend adds code at the start of the body.
func prepend(t *testing.T, body *dst.BlockStmt, code string) {
	t.Helper()
	body.List = append(snippet(t, code), body.List...)
}

// insertStmts adds code in the list, before the statement at index.
func insertStmts(t *testing.T, list []dst.Stmt, index int, code string) []dst.Stmt {
	t.Helper()
	result := append([]dst.Stmt{}, list[:index]...)
	result = append(result, snippet(t, code)...)
	return append(result, list[index:]...)
}

func TestMarkLineDirectives_StartOfFunction(t *testing.T) {
	src := `package main

func f() {
	a()
	b()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		prepend(t, findFunc(t, root, "f").Body, "gen1()\ngen2()")
	})
	want := `package main

func f() {
	/*line <generated>:1*/ gen1()
	gen2()
	/*line FILE:4:1*/ a()
	b()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_EmptyFunctionBody(t *testing.T) {
	src := `package main

func f() {}

func g() {
}

func h() {}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		prepend(t, findFunc(t, root, "f").Body, "gen()")
		prepend(t, findFunc(t, root, "g").Body, "gen()")
	})
	want := `package main

func f() {
	/*line <generated>:1*/ gen() /*line FILE:2*/
}

func g() {
	/*line <generated>:1*/ gen() /*line FILE:5*/
}

func h() {}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_PatternBeforeAndAfter(t *testing.T) {
	src := `package main

func f() {
	a()
	b()
	c()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		// After the last statement, then before the second statement.
		body.List = append(body.List, snippet(t, "genEnd()")...)
		body.List = insertStmts(t, body.List, 1, "genMiddle()")
	})
	want := `package main

func f() {
	a()
	/*line <generated>:1*/ genMiddle()
	/*line FILE:5:1*/ b()
	c()
	/*line <generated>:1*/ genEnd() /*line FILE:6*/
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_MultipleRunsInOneBlock(t *testing.T) {
	src := `package main

func f() {
	a()
	b()
	c()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		body.List = insertStmts(t, body.List, 2, "gen3()")
		body.List = insertStmts(t, body.List, 1, "gen2a()\ngen2b()")
		body.List = insertStmts(t, body.List, 0, "gen1()")
	})
	want := `package main

func f() {
	/*line <generated>:1*/ gen1()
	/*line FILE:4:1*/ a()
	/*line <generated>:1*/ gen2a()
	gen2b()
	/*line FILE:5:1*/ b()
	/*line <generated>:1*/ gen3()
	/*line FILE:6:1*/ c()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_NestedBlocks(t *testing.T) {
	src := `package main

func f(ch chan int, x any) {
	if ch != nil {
		a()
	}
	for i := 0; i < 3; i++ {
		b()
	}
	switch {
	case true:
		c()
	}
	select {
	case <-ch:
		d()
	}
	func() {
		e()
	}()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		// Insert at the start of each nested block.
		for _, stmt := range body.List {
			switch s := stmt.(type) {
			case *dst.IfStmt:
				prepend(t, s.Body, "genIf()")
			case *dst.ForStmt:
				prepend(t, s.Body, "genFor()")
			case *dst.SwitchStmt:
				clause := s.Body.List[0].(*dst.CaseClause)
				clause.Body = append(snippet(t, "genCase()"), clause.Body...)
			case *dst.SelectStmt:
				clause := s.Body.List[0].(*dst.CommClause)
				clause.Body = append(snippet(t, "genComm()"), clause.Body...)
			case *dst.ExprStmt:
				lit := s.X.(*dst.CallExpr).Fun.(*dst.FuncLit)
				prepend(t, lit.Body, "genLit()")
			}
		}
	})
	want := `package main

func f(ch chan int, x any) {
	if ch != nil {
		/*line <generated>:1*/ genIf()
		/*line FILE:5:2*/ a()
	}
	for i := 0; i < 3; i++ {
		/*line <generated>:1*/ genFor()
		/*line FILE:8:2*/ b()
	}
	switch {
	case true:
		/*line <generated>:1*/ genCase()
		/*line FILE:12:2*/ c()
	}
	select {
	case <-ch:
		/*line <generated>:1*/ genComm()
		/*line FILE:16:2*/ d()
	}
	func() {
		/*line <generated>:1*/ genLit()
		/*line FILE:19:2*/ e()
	}()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_EndOfNestedBlocks(t *testing.T) {
	src := `package main

func f(ch chan int) {
	if ch != nil {
		a()
	} else {
		b()
	}
	for i := 0; i < 3; i++ {
		c()
	}
	g := func() {
		d()
	}
	g()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		ifStmt := body.List[0].(*dst.IfStmt)
		ifStmt.Body.List = append(ifStmt.Body.List, snippet(t, "genIf()")...)
		elseBlock := ifStmt.Else.(*dst.BlockStmt)
		elseBlock.List = append(elseBlock.List, snippet(t, "genElse()")...)
		forStmt := body.List[1].(*dst.ForStmt)
		forStmt.Body.List = append(forStmt.Body.List, snippet(t, "genFor()")...)
		lit := body.List[2].(*dst.AssignStmt).Rhs[0].(*dst.FuncLit)
		lit.Body.List = append(lit.Body.List, snippet(t, "genLit()")...)
	})
	want := `package main

func f(ch chan int) {
	if ch != nil {
		a()
		/*line <generated>:1*/ genIf() /*line FILE:5*/
	} else {
		b()
		/*line <generated>:1*/ genElse() /*line FILE:7*/
	}
	for i := 0; i < 3; i++ {
		c()
		/*line <generated>:1*/ genFor() /*line FILE:10*/
	}
	g := func() {
		d()
		/*line <generated>:1*/ genLit() /*line FILE:13*/
	}
	g()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_SwitchClausesShareState(t *testing.T) {
	src := `package main

func f(x int) {
	switch x {
	case 1:
		a()
	case 2:
		b()
	default:
		c()
	}
	d()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		sw := body.List[0].(*dst.SwitchStmt)
		// The first clause ends with generated code: the next case keyword
		// must be restored. The last clause too: the closing brace must be.
		first := sw.Body.List[0].(*dst.CaseClause)
		first.Body = append(first.Body, snippet(t, "genFirst()")...)
		last := sw.Body.List[2].(*dst.CaseClause)
		last.Body = append(last.Body, snippet(t, "genLast()")...)
	})
	want := `package main

func f(x int) {
	switch x {
	case 1:
		a()
		/*line <generated>:1*/ genFirst()
	/*line FILE:6*/
	case 2:
		b()
	default:
		c()
		/*line <generated>:1*/ genLast() /*line FILE:10*/
	}
	d()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_TopLevelDeclarations(t *testing.T) {
	src := `package main

import "fmt"

// F prints.
func F() {
	fmt.Println()
}

func G() {}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		extra, err := ast.NewAstParser().
			ParseSource("package main\n\nimport _ \"unsafe\"\n\ntype T int\n\nfunc H() {}\n")
		require.NoError(t, err)
		// A generated import before the original code, and generated
		// declarations after it.
		root.Decls = append([]dst.Decl{extra.Decls[0]}, root.Decls...)
		root.Decls = append(root.Decls, extra.Decls[1], extra.Decls[2])
	})
	want := `package main

//line <generated>:1
import _ "unsafe"

//line FILE:3
import "fmt"

// F prints.
func F() {
	fmt.Println()
}

func G() {}

//line <generated>:1
type T int

func H() {}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_TopLevelRestoreIsAfterDocComment(t *testing.T) {
	src := `package main

// F is documented.
//
//go:noinline
func F() {}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		extra, err := ast.NewAstParser().ParseSource("package main\n\nimport _ \"unsafe\"\n")
		require.NoError(t, err)
		root.Decls = append([]dst.Decl{extra.Decls[0]}, root.Decls...)
	})
	want := `package main

//line <generated>:1
import _ "unsafe"

// F is documented.
//
//go:noinline
//line FILE:6
func F() {}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_ExistingDirectiveIsNotDuplicated(t *testing.T) {
	src := `package main

func F() {}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		extra, err := ast.NewAstParser().ParseSource("package main\n\n//line <generated>:1\ntype T int\n")
		require.NoError(t, err)
		root.Decls = append(root.Decls, extra.Decls[0])
	})
	want := `package main

func F() {}

//line <generated>:1
type T int
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_ImportSpec(t *testing.T) {
	src := `package main

import (
	"fmt"
	"os"
)

func F() {
	fmt.Println(os.Args)
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		importDecl := root.Decls[0].(*dst.GenDecl)
		spec := &dst.ImportSpec{Name: dst.NewIdent("_"), Path: &dst.BasicLit{Kind: 9, Value: `"unsafe"`}}
		importDecl.Specs = append([]dst.Spec{importDecl.Specs[0], spec}, importDecl.Specs[1:]...)
	})
	// The printer sorts the specs, so the declaration as a whole counts as
	// generated, and the next declaration is restored.
	want := `package main

import (
	"fmt"
	"os"
	_ "unsafe"
)

//line FILE:8
func F() {
	fmt.Println(os.Args)
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_GeneratedBlockIsNotEntered(t *testing.T) {
	src := `package main

func f() {
	a()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		// A generated statement with a block that has statements in it.
		prepend(t, body, "if cond() {\n\tgen1()\n\tgen2()\n}")
	})
	want := `package main

func f() {
	/*line <generated>:1*/ if cond() {
		gen1()
		gen2()
	}
	/*line FILE:4:1*/ a()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_ColumnOneHasOnlyALine(t *testing.T) {
	// The statements are not indented.
	src := "package main\n\nfunc f() {\na()\n\tb()\n}\n"
	got := instrumentSource(t, src, func(root *dst.File) {
		prepend(t, findFunc(t, root, "f").Body, "gen()")
	})
	want := `package main

func f() {
	/*line <generated>:1*/ gen()
	/*line FILE:4*/ a()
	b()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_BlankLinesDoNotDisplaceDirective(t *testing.T) {
	src := `package main

func f() {
	a()

	b()

	c()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		body.List = insertStmts(t, body.List, 1, "gen()")
	})
	// The printer puts a comment on a line of its own when a blank line is
	// before it, and then the directive would apply to the newline. So the
	// pass removes the blank line before a directive.
	want := `package main

func f() {
	a()
	/*line <generated>:1*/ gen()
	/*line FILE:6:1*/ b()

	c()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_KeepsExistingComments(t *testing.T) {
	src := `package main

func f() {
	a()
	// Comment about b.
	b()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		body.List = insertStmts(t, body.List, 1, "gen()")
	})
	// The printer puts the directive on a line of its own, after the comment.
	// The newline after the directive is on the line before b.
	want := `package main

func f() {
	a()
	/*line <generated>:1*/ gen()
	// Comment about b.
	/*line FILE:5*/
	b()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_FollowsExistingLineDirective(t *testing.T) {
	// A cgo file has a line directive that maps the code to another file. The
	// restore directive must use the mapped position, and the name is
	// relative to the directory of the file.
	src := `package main

//line other.go:100
func f() {
	a()
	b()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		prepend(t, findFunc(t, root, "f").Body, "gen()")
	})
	want := `package main

//line other.go:100
func f() {
	/*line <generated>:1*/ gen()
	/*line DIR/other.go:101*/ a()
	b()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_NothingGenerated(t *testing.T) {
	src := `package main

func f() {
	a()
}
`
	got := instrumentSource(t, src, func(*dst.File) {})
	assert.Equal(t, src, got)
}

func TestMarkLineDirectives_LabeledStatement(t *testing.T) {
	src := `package main

func f() {
	a()
loop:
	for {
		b()
		goto loop
	}
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		body.List = insertStmts(t, body.List, 1, "gen()")
	})
	// The printer puts the label and the statement on two lines. Both need a
	// directive.
	want := `package main

func f() {
	a()
	/*line <generated>:1*/ gen()
	/*line FILE:5*/ loop:
	/*line FILE:6:1*/ for {
		b()
		goto loop
	}
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_UnsafeFileNameIsNotWritten(t *testing.T) {
	// A "//line" comment in the source can give any file name. A name with
	// "*/" ends the comment that the pass writes, and the rest is code.
	src := `package main

func f() {
	a()
//line /tmp/x/*/;println("INJECTED");/*orig.go:5:1
	b()
	c()
}

//line /tmp/y/*/;println("INJECTED");/*orig.go:20
func g() {
	d()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		f := findFunc(t, root, "f").Body
		// A generated statement before b makes the pass restore b.
		f.List = insertStmts(t, f.List, 1, "gen1()")
		// A generated last statement makes the pass restore the closing
		// brace of f.
		f.List = append(f.List, snippet(t, "gen4()")...)
		prepend(t, findFunc(t, root, "g").Body, "gen2()")
		// A generated declaration before g makes the pass restore g.
		gen, err := ast.NewAstParser().ParseSource("package p\n\nfunc gen3() {}\n")
		require.NoError(t, err)
		root.Decls = append(root.Decls[:1], append([]dst.Decl{gen.Decls[0]}, root.Decls[1:]...)...)
	})
	// The comments of the user are the only place for the name.
	assert.NotContains(t, got, "/*line /tmp")
	assert.Equal(t, 2, strings.Count(got, "INJECTED"), got)
	for line := range strings.SplitSeq(got, "\n") {
		if strings.Contains(line, "INJECTED") {
			assert.True(t, strings.HasPrefix(strings.TrimSpace(line), "//line /tmp/"), line)
		}
	}
	// The injected code is still marked.
	assert.Contains(t, got, "/*line <generated>:1*/ gen1()")
	// gen2 and gen4 are in the same run as gen1, because the restores were
	// skipped. They do not need a directive.
	assert.Contains(t, got, "\tgen4()\n")
	assert.Contains(t, got, "\tgen2()\n")
	assert.Equal(t, 1, strings.Count(got, "<generated>"), got)
}

func TestMarkLineDirectives_SafeDirectiveFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
		want bool
	}{
		{"absolute path", "/tmp/dir/orig.go", true},
		{"space", "/tmp/my dir/orig.go", true},
		{"unicode", "/tmp/dir/\u00e9\u4e16.go", true},
		{"comment end", "/tmp/x/*/y.go", false},
		{"line feed", "/tmp/x\ny.go", false},
		{"carriage return", "/tmp/x\ry.go", false},
		{"byte order mark", "/tmp/\uFEFFx/y.go", false},
		{"byte order mark at start", "\uFEFF/tmp/y.go", false},
		{"NUL", "/tmp/x\x00y.go", false},
		{"invalid UTF-8", "/tmp/x\xffy.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, safeDirectiveFile(tc.file))
		})
	}
}

func TestMarkLineDirectives_ByteOrderMarkInPathIsNotWritten(t *testing.T) {
	// The Go scanner rejects a byte order mark in a comment. If the pass
	// wrote the path, ast.WriteFile would fail.
	dir := filepath.Join(t.TempDir(), "\uFEFFdir")
	require.NoError(t, os.Mkdir(dir, 0o700))
	src := `package main

func f() {
	a()
	b()
	c()
}

func g() {
	d()
}
`
	file := filepath.Join(dir, "orig.go")
	require.NoError(t, os.WriteFile(file, []byte(src), 0o600))
	p := ast.NewAstParser()
	root, err := p.Parse(file, parser.ParseComments)
	require.NoError(t, err)
	f := findFunc(t, root, "f").Body
	// A restore of b, a restore of the closing brace of f, and a restore of g.
	f.List = insertStmts(t, f.List, 1, "gen1()")
	f.List = append(f.List, snippet(t, "gen2()")...)
	gen, err := ast.NewAstParser().ParseSource("package p\n\nfunc gen3() {}\n")
	require.NoError(t, err)
	root.Decls = append(root.Decls[:1], append([]dst.Decl{gen.Decls[0]}, root.Decls[1:]...)...)
	markLineDirectives(p, root, file)

	out := filepath.Join(t.TempDir(), "out.go")
	require.NoError(t, ast.WriteFile(out, root))
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "\uFEFF")
	assert.NotContains(t, string(got), "orig.go")
	assert.Contains(t, string(got), "/*line <generated>:1*/ gen1()")
}

func TestMarkLineDirectives_TopLevelRestoreAfterSkippedRestore(t *testing.T) {
	// The restores of g and of the closing brace of f are skipped, because
	// their file name is unsafe. h has a safe file name, and it must be
	// restored.
	src := `package main

func f() {
	a()
}

//line /tmp/y/*/;println("INJECTED");/*orig.go:20
func g() {
	d()
}

//line safe.go:90
func h() {}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		gen, err := ast.NewAstParser().ParseSource("package p\n\nfunc gen3() {}\n")
		require.NoError(t, err)
		root.Decls = append(root.Decls[:1], append([]dst.Decl{gen.Decls[0]}, root.Decls[1:]...)...)
	})
	assert.Equal(t, 1, strings.Count(got, "INJECTED"), got)
	assert.Contains(t, got, "//line DIR/safe.go:90\nfunc h() {}")

	// The closing brace of f is not restored either.
	src = `package main

func f() {
	a()
//line /tmp/x/*/;println("INJECTED");/*orig.go:5
}

//line safe.go:90
func h() {}
`
	got = instrumentSource(t, src, func(root *dst.File) {
		f := findFunc(t, root, "f").Body
		f.List = append(f.List, snippet(t, "gen()")...)
	})
	assert.Equal(t, 1, strings.Count(got, "INJECTED"), got)
	assert.Contains(t, got, "//line DIR/safe.go:90\nfunc h() {}")
}

func TestMarkLineDirectives_ClonedNodeIsGenerated(t *testing.T) {
	src := `package main

func f() {
	a()
	b()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		// A clone has no position in the original file.
		clone := dst.Clone(body.List[0]).(dst.Stmt)
		body.List = append([]dst.Stmt{clone}, body.List...)
	})
	want := `package main

func f() {
	/*line <generated>:1*/ a()
	/*line FILE:4:1*/ a()
	b()
}
`
	assert.Equal(t, want, got)
}

// --- Tests that compile and run the printed code ---

const runProgram = `package main

import (
	"fmt"
	"runtime"
)

func where() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}

func target() {
	fmt.Println("A", where())
	fmt.Println("B", where())
	if len(fmt.Sprint()) == 0 {
		fmt.Println("C", where())
	}
	switch {
	case true:
		fmt.Println("D", where())
	case false:
		fmt.Println("E", where())
	}
	fmt.Println("F", where())
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	fmt.Println("G", where())
}

func main() {
	target()
}
`

// goTool runs the go command in dir and returns its output.
func goTool(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "go", args...)
	cmd.Dir = dir
	// GOWORK=off: the test module must not join a go.work of the developer.
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GO111MODULE=on", "GOWORK=off")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// writeModule writes a module with one file in a new temporary directory.
func writeModule(t *testing.T, name, src string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n\ngo 1.21\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
	return dir
}

func TestMarkLineDirectives_CompilerHonoursDirectives(t *testing.T) {
	origDir := writeModule(t, "orig.go", runProgram)
	origFile := filepath.Join(origDir, "orig.go")

	p := ast.NewAstParser()
	root, err := p.Parse(origFile, parser.ParseComments)
	require.NoError(t, err)
	body := findFunc(t, root, "target").Body

	// Injected statements print their own id and their position.
	where := func(id string) string {
		return fmt.Sprintf(`fmt.Println(%q, where())`, id)
	}
	// Original statements: 0 A, 1 B, 2 if, 3 switch, 4 F, 5 defer, 6 G.
	ifStmt := body.List[2].(*dst.IfStmt)
	ifStmt.Body.List = append(ifStmt.Body.List, snippet(t, where("gen-if-end"))...)
	clause := body.List[3].(*dst.SwitchStmt).Body.List[0].(*dst.CaseClause)
	clause.Body = append(clause.Body, snippet(t, where("gen-case-end"))...)
	body.List = insertStmts(t, body.List, 4, where("gen-middle"))
	body.List = insertStmts(t, body.List, 0, where("gen-start")+"\n"+where("gen-start-2"))
	// This is the last statement. The deferred function in the program shows
	// where the closing brace of the function is.
	body.List = append(body.List, snippet(t, where("gen-end"))...)
	markLineDirectives(p, root, origFile)

	instDir := writeModule(t, "inst.go", "")
	require.NoError(t, ast.WriteFile(filepath.Join(instDir, "inst.go"), root))

	out, err := goTool(t, instDir, "run", "inst.go")
	require.NoError(t, err, out)

	pos := func(line int) string { return fmt.Sprintf("%s:%d", origFile, line) }
	lines := strings.Split(strings.TrimSpace(out), "\n")
	got := make(map[string]string)
	for _, line := range lines {
		fields := strings.Fields(line)
		got[fields[0]] = strings.Join(fields[1:], " ")
	}
	lineOf := func(marker string) int {
		for i, l := range strings.Split(runProgram, "\n") {
			if strings.Contains(l, marker) {
				return i + 1
			}
		}
		require.Failf(t, "marker not found", "marker: %s", marker)
		return 0
	}
	// Original statements keep their line in the original file.
	assert.Equal(t, pos(lineOf(`"A"`)), got["A"])
	assert.Equal(t, pos(lineOf(`"B"`)), got["B"])
	assert.Equal(t, pos(lineOf(`"C"`)), got["C"])
	assert.Equal(t, pos(lineOf(`"D"`)), got["D"])
	assert.Equal(t, pos(lineOf(`"F"`)), got["F"])
	assert.Equal(t, pos(lineOf(`"G"`)), got["G"])
	// Injected statements are in the "<generated>" file.
	for _, id := range []string{"gen-start", "gen-start-2", "gen-if-end", "gen-case-end", "gen-middle", "gen-end"} {
		// The compiler puts the directory of the file before the name.
		assert.Regexp(t, `/<generated>:\d+$`, got[id], id)
	}
	// The deferred function is called from the closing brace of target.
	assert.Equal(t, pos(lineOf(`"G"`)+1), got["closing"], "line of the closing brace")
}

func TestMarkLineDirectives_CompilerReportsOriginalColumns(t *testing.T) {
	// Each function has a statement with a type error. The compiler must
	// report the same position as for the original file.
	src := "package main\n\nfunc f() {\n\tvar a int = \"a\"\n\t_ = a\n}\n\n" +
		"func g() {\nvar b int = \"b\"\n_ = b\n}\n\nfunc main() {\n\tf()\n\tg()\n}\n"
	origDir := writeModule(t, "orig.go", src)
	origFile := filepath.Join(origDir, "orig.go")
	origOut, err := goTool(t, origDir, "build", "-gcflags=-e", "-o", os.DevNull, "orig.go")
	require.Error(t, err)

	p := ast.NewAstParser()
	root, err := p.Parse(origFile, parser.ParseComments)
	require.NoError(t, err)
	prepend(t, findFunc(t, root, "f").Body, "println(1)")
	prepend(t, findFunc(t, root, "g").Body, "println(2)")
	markLineDirectives(p, root, origFile)
	instDir := writeModule(t, "inst.go", "")
	require.NoError(t, ast.WriteFile(filepath.Join(instDir, "inst.go"), root))
	instOut, err := goTool(t, instDir, "build", "-gcflags=-e", "-o", os.DevNull, "inst.go")
	require.Error(t, err)

	position := regexp.MustCompile(`orig\.go:(\d+)(?::(\d+))?: cannot use`)
	want := position.FindAllStringSubmatch(origOut, -1)
	got := position.FindAllStringSubmatch(instOut, -1)
	require.Len(t, want, 2, origOut)
	require.Len(t, got, 2, instOut)
	// The statement in f is indented: the line and the column are the same.
	assert.Equal(t, "4", got[0][1])
	assert.Equal(t, "14", got[0][2])
	assert.Equal(t, want[0][1:], got[0][1:])
	// The statement in g is at column 1. The directive has no column, so the
	// compiler gives only the line.
	assert.Equal(t, "9", got[1][1])
	assert.Equal(t, want[1][1], got[1][1])
	assert.Empty(t, got[1][2])
}

// runInjected parses src as the file orig.go of a module, calls edit, runs the
// line directive pass, and runs the printed program. It returns the path of
// the original file and the output of the program.
// injectedRun is the result of runInjected.
type injectedRun struct {
	// origFile is the path of the original file.
	origFile string
	// output is the output of the program.
	output string
}

func runInjected(t *testing.T, src string, edit func(root *dst.File)) injectedRun {
	t.Helper()
	origDir := writeModule(t, "orig.go", src)
	origFile := filepath.Join(origDir, "orig.go")
	p := ast.NewAstParser()
	root, err := p.Parse(origFile, parser.ParseComments)
	require.NoError(t, err)
	edit(root)
	markLineDirectives(p, root, origFile)
	instDir := writeModule(t, "inst.go", "")
	instFile := filepath.Join(instDir, "inst.go")
	require.NoError(t, ast.WriteFile(instFile, root))
	out, err := goTool(t, instDir, "run", "inst.go")
	instrumented, readErr := os.ReadFile(instFile)
	require.NoError(t, readErr)
	require.NoError(t, err, "%s\n%s", out, instrumented)
	return injectedRun{origFile: origFile, output: out}
}

// positions reads the lines "<id> <file:line>" of the output.
func positions(out string) map[string]string {
	got := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 {
			got[fields[0]] = fields[1]
		}
	}
	return got
}

// lineOfMarker returns the line number of the first line of src with marker.
func lineOfMarker(t *testing.T, src, marker string) int {
	t.Helper()
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, marker) {
			return i + 1
		}
	}
	require.Failf(t, "marker not found", "marker: %s", marker)
	return 0
}

const controlFlowProgram = `package main

import (
	"fmt"
	"runtime"
)

func where() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}

func target() {
	fmt.Println("A", where())
loop:
	for i := 0; i < 2; i++ {
		fmt.Println("B", where())
		if i > 5 {
			goto done
		}
		continue loop
	}
done:
	fmt.Println("C", where())
	switch x := len(fmt.Sprint()); x {
	case 0:
		fmt.Println("D", where())
		fallthrough
	case 1:
		fmt.Println("E", where())
	default:
		fmt.Println("F", where())
	}
	fmt.Println("G", where())
}

func main() {
	target()
}
`

func TestMarkLineDirectives_CompilerHonoursLabelsGotoAndFallthrough(t *testing.T) {
	where := func(id string) string {
		return fmt.Sprintf(`fmt.Println(%q, where())`, id)
	}
	run := runInjected(t, controlFlowProgram, func(root *dst.File) {
		body := findFunc(t, root, "target").Body
		// Original statements: 0 A, 1 loop, 2 done, 3 switch, 4 G.
		sw := body.List[3].(*dst.SwitchStmt)
		first := sw.Body.List[0].(*dst.CaseClause)
		second := sw.Body.List[1].(*dst.CaseClause)
		last := sw.Body.List[2].(*dst.CaseClause)
		first.Body = insertStmts(t, first.Body, 1, where("gen-before-fallthrough"))
		second.Body = append(snippet(t, where("gen-case-start")), second.Body...)
		last.Body = append(last.Body, snippet(t, where("gen-default-end"))...)
		body.List = insertStmts(t, body.List, 4, where("gen-before-g"))
		body.List = insertStmts(t, body.List, 2, where("gen-before-done"))
		body.List = insertStmts(t, body.List, 1, where("gen-before-loop"))
		body.List = insertStmts(t, body.List, 0, where("gen-start"))
	})
	got := positions(run.output)
	for id, marker := range map[string]string{
		"A": `"A"`, "B": `"B"`, "C": `"C"`, "D": `"D"`, "E": `"E"`, "G": `"G"`,
	} {
		want := fmt.Sprintf("%s:%d", run.origFile, lineOfMarker(t, controlFlowProgram, marker))
		assert.Equal(t, want, got[id], id)
	}
	// "F" and "gen-default-end" do not run: case 0 falls through to case 1.
	for _, id := range []string{
		"gen-start", "gen-before-fallthrough", "gen-case-start", "gen-before-g", "gen-before-done", "gen-before-loop",
	} {
		assert.Regexp(t, `/<generated>:\d+$`, got[id], id)
	}
}

func TestMarkLineDirectives_CompilerHonoursLabelAfterGeneratedStatement(t *testing.T) {
	// The source has the label and its statement on one line. The printer
	// puts them on two lines. The statement and the statements after it must
	// report their original lines.
	src := `package main

import (
	"fmt"
	"runtime"
)

func where() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}

func main() {
	fmt.Println("A", where())
retry: fmt.Println("B", where())
	fmt.Println("C", where())
	if len(fmt.Sprint()) > 0 {
		goto retry
	}
}
`
	run := runInjected(t, src, func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		body.List = insertStmts(t, body.List, 1, `fmt.Println("gen", where())`)
	})
	got := positions(run.output)
	for _, id := range []string{"B", "C"} {
		want := fmt.Sprintf("%s:%d", run.origFile, lineOfMarker(t, src, `"`+id+`"`))
		assert.Equal(t, want, got[id], id)
	}
	assert.Regexp(t, `/<generated>:\d+$`, got["gen"])
}

func TestMarkLineDirectives_CompilerIgnoresUnsafeFileName(t *testing.T) {
	// The source has a file name that would close the comment and add code.
	// The code must not run.
	src := "package main\n\nfunc main() {\n\tprintln(\"start\")\n" +
		"//line /tmp/x/*/;println(\"INJECTED\");/*orig.go:5:1\n" +
		"\tprintln(\"end\")\n}\n"
	run := runInjected(t, src, func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		body.List = insertStmts(t, body.List, 1, `println("gen")`)
	})
	assert.Contains(t, run.output, "start")
	assert.Contains(t, run.output, "gen")
	assert.Contains(t, run.output, "end")
	assert.NotContains(t, run.output, "INJECTED")
}

func TestMarkLineDirectives_CompilerRestoresAfterSkippedRestore(t *testing.T) {
	// The restore of C is skipped, because its file name is unsafe. D has a
	// safe file name, and it must be restored.
	src := `package main

import (
	"fmt"
	"runtime"
)

func where() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}

func main() {
	fmt.Println("A", where())
	fmt.Println("B", where())
//line /tmp/x/*/;println("INJECTED");/*orig.go:30:1
	fmt.Println("C", where())
//line safe.go:77:1
	fmt.Println("D", where())
	fmt.Println("E", where())
}
`
	run := runInjected(t, src, func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		body.List = insertStmts(t, body.List, 2, `fmt.Println("gen", where())`)
	})
	got := positions(run.output)
	safe := filepath.Join(filepath.Dir(run.origFile), "safe.go")
	assert.Equal(t, safe+":77", got["D"])
	assert.Equal(t, safe+":78", got["E"])
	assert.Regexp(t, `/<generated>:\d+$`, got["gen"])
	assert.NotContains(t, run.output, "INJECTED")
}

func TestMarkLineDirectives_CompilerRestoresAfterSkippedBraceRestore(t *testing.T) {
	// The name of the closing brace of the if statement is unsafe, so its
	// restore is skipped. The statement after the if statement has a safe file
	// name, and it must be restored.
	src := `package main

import (
	"fmt"
	"runtime"
)

func where() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}

func main() {
	fmt.Println("A", where())
	if len(fmt.Sprint()) == 0 {
		fmt.Println("B", where())
//line /tmp/x/*/;println("INJECTED");/*orig.go:30:1
	}
//line safe.go:80:1
	fmt.Println("D", where())
	fmt.Println("E", where())
}
`
	run := runInjected(t, src, func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		ifStmt := body.List[1].(*dst.IfStmt)
		ifStmt.Body.List = append(ifStmt.Body.List, snippet(t, `fmt.Println("gen", where())`)...)
	})
	got := positions(run.output)
	safe := filepath.Join(filepath.Dir(run.origFile), "safe.go")
	assert.Equal(t, safe+":80", got["D"])
	assert.Equal(t, safe+":81", got["E"])
	assert.Regexp(t, `/<generated>:\d+$`, got["gen"])
	assert.NotContains(t, run.output, "INJECTED")
}

// unsafeDirective is a "//line" comment with a file name that would close a
// block comment and add code. The pass must not write this name.
const unsafeDirective = `//line /tmp/x/*/;println("INJECTED");/*orig.go:30:1`

// preludeProgram is the start of the programs below. It has the function
// where, which gives the position of its caller.
const preludeProgram = `package main

import (
	"fmt"
	"runtime"
)

func where() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}

func call(f func()) int {
	f()
	return 1
}

`

// printWhere returns the code of a statement that prints id and its position.
func printWhere(id string) string {
	return fmt.Sprintf(`fmt.Println(%q, where())`, id)
}

func TestMarkLineDirectives_CompilerThreadsStateThroughSiblingBlocks(t *testing.T) {
	// The closing brace of each if body has an unsafe file name, so its
	// restore is skipped. The state must follow the code in source order, and
	// not be the "or" of the state of the blocks.
	src := preludeProgram + `func f1(b bool) {
	if b {
		fmt.Println("B1", where())
` + unsafeDirective + `
	} else {
//line safe.go:77:1
		fmt.Println("D1", where())
	}
	fmt.Println("E1", where())
}

func f2(b bool) {
	if b {
		fmt.Println("B2", where())
` + unsafeDirective + `
	} else {
//line safe.go:90:1
		fmt.Println("D2", where())
	}
	fmt.Println("E2", where())
}

func main() {
	f1(true)
	f1(false)
	f2(true)
	f2(false)
}
`
	run := runInjected(t, src, func(root *dst.File) {
		// f1: the if body ends in an injected statement, so the brace is not
		// restored. The else block starts with an injected statement, and then
		// has an original statement with a safe name. It ends in original
		// code, so an injected statement after the if is the start of a run.
		f1 := findFunc(t, root, "f1").Body
		ifStmt := f1.List[0].(*dst.IfStmt)
		ifStmt.Body.List = append(ifStmt.Body.List, snippet(t, printWhere("gen-if1"))...)
		elseBlock := ifStmt.Else.(*dst.BlockStmt)
		elseBlock.List = insertStmts(t, elseBlock.List, 0, printWhere("gen-else1"))
		f1.List = insertStmts(t, f1.List, 1, printWhere("gen-after1"))
		// f2: the else block has only original statements. The brace of the if
		// body was not restored, so the first statement of the else block must
		// be restored.
		f2 := findFunc(t, root, "f2").Body
		ifStmt = f2.List[0].(*dst.IfStmt)
		ifStmt.Body.List = append(ifStmt.Body.List, snippet(t, printWhere("gen-if2"))...)
		f2.List = insertStmts(t, f2.List, 1, printWhere("gen-after2"))
	})
	got := positions(run.output)
	safe := filepath.Join(filepath.Dir(run.origFile), "safe.go")
	assert.Equal(t, safe+":77", got["D1"])
	assert.Equal(t, safe+":79", got["E1"])
	assert.Equal(t, safe+":90", got["D2"])
	assert.Equal(t, safe+":92", got["E2"])
	for _, id := range []string{"gen-if1", "gen-else1", "gen-after1", "gen-if2", "gen-after2"} {
		assert.Regexp(t, `/<generated>:\d+$`, got[id], id)
	}
	assert.NotContains(t, run.output, "INJECTED")
}

func TestMarkLineDirectives_CompilerThreadsStateFromCaseExpression(t *testing.T) {
	// The function literal in the case expression ends in an injected
	// statement, and the name of its closing brace is unsafe. The statement in
	// the clause has a safe name, and it must be restored.
	src := preludeProgram + `func main() {
	x := 1
	switch x {
	case call(func() {
		fmt.Println("L", where())
` + unsafeDirective + `
	}):
//line safe.go:80:1
		fmt.Println("D", where())
		fmt.Println("E", where())
	}
}
`
	run := runInjected(t, src, func(root *dst.File) {
		sw := findFunc(t, root, "main").Body.List[1].(*dst.SwitchStmt)
		lit := sw.Body.List[0].(*dst.CaseClause).List[0].(*dst.CallExpr).Args[0].(*dst.FuncLit)
		lit.Body.List = append(lit.Body.List, snippet(t, printWhere("gen"))...)
	})
	got := positions(run.output)
	safe := filepath.Join(filepath.Dir(run.origFile), "safe.go")
	assert.Equal(t, safe+":80", got["D"])
	assert.Equal(t, safe+":81", got["E"])
	assert.Regexp(t, `/<generated>:\d+$`, got["gen"])
	assert.NotContains(t, run.output, "INJECTED")
}

func TestMarkLineDirectives_CompilerThreadsStateFromCommunication(t *testing.T) {
	// Like the test above, but the function literal is in the communication of
	// a select clause.
	src := preludeProgram + `func main() {
	ch := make(chan int, 1)
	select {
	case ch <- call(func() {
		fmt.Println("L", where())
` + unsafeDirective + `
	}):
//line safe.go:80:1
		fmt.Println("D", where())
		fmt.Println("E", where())
	}
}
`
	run := runInjected(t, src, func(root *dst.File) {
		sel := findFunc(t, root, "main").Body.List[1].(*dst.SelectStmt)
		send := sel.Body.List[0].(*dst.CommClause).Comm.(*dst.SendStmt)
		lit := send.Value.(*dst.CallExpr).Args[0].(*dst.FuncLit)
		lit.Body.List = append(lit.Body.List, snippet(t, printWhere("gen"))...)
	})
	got := positions(run.output)
	safe := filepath.Join(filepath.Dir(run.origFile), "safe.go")
	assert.Equal(t, safe+":80", got["D"])
	assert.Equal(t, safe+":81", got["E"])
	assert.Regexp(t, `/<generated>:\d+$`, got["gen"])
	assert.NotContains(t, run.output, "INJECTED")
}

func TestMarkLineDirectives_Directive(t *testing.T) {
	tests := []struct {
		name      string
		file      string
		line, col int
		want      string
		ok        bool
	}{
		{name: "line", file: "/a/b.go", line: 7, want: "/a/b.go:7", ok: true},
		{name: "line and column", file: "/a/b.go", line: 7, col: 3, want: "/a/b.go:7:3", ok: true},
		{name: "line zero", file: "/a/b.go", line: 0},
		{name: "line zero with column", file: "/a/b.go", line: 0, col: 1},
		{name: "negative column", file: "/a/b.go", line: 1, col: -1},
		{name: "largest line", file: "/a/b.go", line: 1 << 30, want: "/a/b.go:1073741824", ok: true},
		{name: "line too large", file: "/a/b.go", line: 1<<30 + 1},
		{name: "largest column", file: "/a/b.go", line: 1, col: 1 << 30, want: "/a/b.go:1:1073741824", ok: true},
		{name: "column too large", file: "/a/b.go", line: 1, col: 1<<30 + 1},
		{name: "unsafe name", file: "/a/*/b.go", line: 1, col: 1},
		{name: "name ends in a number", file: "/a/b:0", line: 7},
		{name: "name ends in a number, with column", file: "/a/b:0", line: 7, col: 1, want: "/a/b:0:7:1", ok: true},
		{name: "name has a number inside", file: "/a/b:0/c.go", line: 7, want: "/a/b:0/c.go:7", ok: true},
		{name: "name ends in a colon", file: "/a/b:", line: 7, want: "/a/b::7", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := directive(tt.file, tt.line, tt.col)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// genDecl returns a generated top-level declaration.
func genDecl(t *testing.T) dst.Decl {
	t.Helper()
	gen, err := ast.NewAstParser().ParseSource("package p\n\nfunc gen() {}\n")
	require.NoError(t, err)
	return gen.Decls[0]
}

func TestMarkLineDirectives_CompilerNameEndsInNumber(t *testing.T) {
	// A name that ends in ":0" is valid in the source, because the directive
	// has a line and a column. A directive with only a line would be read as
	// line 0 and column 12 by Go, and Go rejects it.
	const name = "/tmp/foo:0"
	t.Run("top-level declaration", func(t *testing.T) {
		// g has no comment before it, so only the pass can restore it.
		src := preludeProgram + `//line ` + name + `:12:1
func h() {}

func g() {
	fmt.Println("G", where())
}

func main() {
	g()
}
`
		edit := func(root *dst.File) {
			root.Decls = append(root.Decls[:4], append([]dst.Decl{genDecl(t)}, root.Decls[4:]...)...)
		}
		run := runInjected(t, src, edit)
		// The pass restores g with a column, and not with a line only.
		assert.Contains(t, instrumentSource(t, src, edit), "\n//line "+name+":14:1\nfunc g() {")
		// h is at line 12 and g is at line 14. The first statement of g is at
		// line 15.
		assert.Equal(t, name+":15", positions(run.output)["G"])
	})
	t.Run("statement", func(t *testing.T) {
		// S1 is at column 1 and has no comment before it. A directive for it
		// can have only a line, and it is not valid: the pass does not restore
		// S1. S2 has a column, and it is restored.
		src := preludeProgram + `func main() {
//line ` + name + `:12:1
fmt.Println("S0", where())
fmt.Println("S1", where())
	fmt.Println("S2", where())
	fmt.Println("S3", where())
}
`
		run := runInjected(t, src, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			body.List = insertStmts(t, body.List, 1, printWhere("gen"))
		})
		got := positions(run.output)
		assert.Regexp(t, `/<generated>:\d+$`, got["gen"])
		assert.Regexp(t, `/<generated>:\d+$`, got["S1"])
		assert.Equal(t, name+":14", got["S2"])
		assert.Equal(t, name+":15", got["S3"])
	})
	t.Run("closing brace", func(t *testing.T) {
		src := preludeProgram + `func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	fmt.Println("S", where())
//line ` + name + `:50:1
}
`
		run := runInjected(t, src, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			body.List = append(body.List, snippet(t, printWhere("gen"))...)
		})
		got := positions(run.output)
		assert.Regexp(t, `/<generated>:\d+$`, got["gen"])
		assert.Equal(t, name+":50", got["closing"])
	})
	t.Run("own-line directive", func(t *testing.T) {
		// A statement with a comment before it, and a clause of a switch,
		// have a directive on a line of its own.
		src := preludeProgram + `func main() {
	x := 1
	switch x {
	case 1:
		fmt.Println("C1", where())
//line ` + name + `:50:1
	case 2:
		fmt.Println("C2", where())
	}
	fmt.Println("S", where())
//line ` + name + `:60:1
	// comment
	fmt.Println("T", where())
}
`
		run := runInjected(t, src, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			clause := body.List[1].(*dst.SwitchStmt).Body.List[0].(*dst.CaseClause)
			clause.Body = append(clause.Body, snippet(t, printWhere("gen1"))...)
			body.List = insertStmts(t, body.List, 3, printWhere("gen2"))
		})
		got := positions(run.output)
		assert.Regexp(t, `/<generated>:\d+$`, got["gen1"])
		assert.Regexp(t, `/<generated>:\d+$`, got["gen2"])
		assert.Equal(t, name+":61", got["T"])
	})
}

func TestMarkLineDirectives_CompilerLineOneIsNotRestoredAsLineZero(t *testing.T) {
	// A directive for line 1 would need the line before it, which is line 0.
	// Go rejects line 0. The pass does not write the directive, and a later
	// node is restored.
	const file = "/tmp/foo.go"
	t.Run("closing brace", func(t *testing.T) {
		src := preludeProgram + `func main() {
	fmt.Println("S", where())
//line ` + file + `:1:1
}
`
		run := runInjected(t, src, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			body.List = append(body.List, snippet(t, printWhere("gen"))...)
		})
		assert.Regexp(t, `/<generated>:\d+$`, positions(run.output)["gen"])
	})
	t.Run("statement with a comment", func(t *testing.T) {
		src := preludeProgram + `func main() {
//line ` + file + `:1:1
	/* comment */ fmt.Println("S1", where())
	fmt.Println("S2", where())
}
`
		run := runInjected(t, src, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			body.List = insertStmts(t, body.List, 0, printWhere("gen"))
		})
		got := positions(run.output)
		assert.Regexp(t, `/<generated>:\d+$`, got["gen"])
		assert.Regexp(t, `/<generated>:\d+$`, got["S1"])
		assert.Equal(t, file+":2", got["S2"])
	})
	t.Run("clause of a switch", func(t *testing.T) {
		src := preludeProgram + `func main() {
	for _, x := range []int{1, 2} {
		switch x {
		case 1:
			fmt.Println("C1", where())
//line ` + file + `:1:1
		case 2:
			fmt.Println("C2", where())
		}
	}
}
`
		run := runInjected(t, src, func(root *dst.File) {
			loop := findFunc(t, root, "main").Body.List[0].(*dst.RangeStmt)
			clause := loop.Body.List[0].(*dst.SwitchStmt).Body.List[0].(*dst.CaseClause)
			clause.Body = append(clause.Body, snippet(t, printWhere("gen"))...)
		})
		got := positions(run.output)
		assert.Regexp(t, `/<generated>:\d+$`, got["gen"])
		assert.Equal(t, file+":2", got["C2"])
	})
}
