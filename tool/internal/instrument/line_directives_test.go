// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package instrument

import (
	"bytes"
	"context"
	"fmt"
	goast "go/ast"
	"go/parser"
	"go/token"
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
	/*line /<generated>:1*/ gen1()
	gen2()
	/*line FILE:4*/ a()
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
	/*line /<generated>:1*/ gen() /*line FILE:2*/
}

func g() {
	/*line /<generated>:1*/ gen() /*line FILE:5*/
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
	/*line /<generated>:1*/ genMiddle()
	/*line FILE:5*/ b()
	c()
	/*line /<generated>:1*/ genEnd() /*line FILE:6*/
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
	/*line /<generated>:1*/ gen1()
	/*line FILE:4*/ a()
	/*line /<generated>:1*/ gen2a()
	gen2b()
	/*line FILE:5*/ b()
	/*line /<generated>:1*/ gen3()
	/*line FILE:6*/ c()
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
		/*line /<generated>:1*/ genIf()
		/*line FILE:5*/ a()
	}
	for i := 0; i < 3; i++ {
		/*line /<generated>:1*/ genFor()
		/*line FILE:8*/ b()
	}
	switch {
	case true:
		/*line /<generated>:1*/ genCase()
		/*line FILE:12*/ c()
	}
	select {
	case <-ch:
		/*line /<generated>:1*/ genComm()
		/*line FILE:16*/ d()
	}
	func() {
		/*line /<generated>:1*/ genLit()
		/*line FILE:19*/ e()
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
		/*line /<generated>:1*/ genIf() /*line FILE:5*/
	} else {
		b()
		/*line /<generated>:1*/ genElse() /*line FILE:7*/
	}
	for i := 0; i < 3; i++ {
		c()
		/*line /<generated>:1*/ genFor() /*line FILE:10*/
	}
	g := func() {
		d()
		/*line /<generated>:1*/ genLit() /*line FILE:13*/
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
		/*line /<generated>:1*/ genFirst()
	/*line FILE:6*/
	case 2:
		b()
	default:
		c()
		/*line /<generated>:1*/ genLast() /*line FILE:10*/
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

//line /<generated>:1
import _ "unsafe"

//line FILE:3
import "fmt"

// F prints.
func F() {
	fmt.Println()
}

func G() {}

//line /<generated>:1
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

//line /<generated>:1
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
		extra, err := ast.NewAstParser().ParseSource("package main\n\n//line /<generated>:1\ntype T int\n")
		require.NoError(t, err)
		root.Decls = append(root.Decls, extra.Decls[0])
	})
	want := `package main

func F() {}

//line /<generated>:1
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
	// The printer sorts the specs, so the lines after the declaration are off.
	// The state becomes stale, and the next declaration is restored.
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
	/*line /<generated>:1*/ if cond() {
		gen1()
		gen2()
	}
	/*line FILE:4*/ a()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_DirectiveHasOnlyALine(t *testing.T) {
	// The statements are not indented.
	src := "package main\n\nfunc f() {\na()\n\tb()\n}\n"
	got := instrumentSource(t, src, func(root *dst.File) {
		prepend(t, findFunc(t, root, "f").Body, "gen()")
	})
	want := `package main

func f() {
	/*line /<generated>:1*/ gen()
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
	/*line /<generated>:1*/ gen()
	/*line FILE:6*/ b()

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
	/*line /<generated>:1*/ gen()
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
	/*line /<generated>:1*/ gen()
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
	/*line /<generated>:1*/ gen()
	/*line FILE:5*/ loop:
	/*line FILE:6*/ for {
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
	// The injected code is still marked. After original code that the pass
	// does not restore, the state is stale, so each generated node gets its
	// own directive.
	assert.Contains(t, got, "/*line /<generated>:1*/ gen1()")
	assert.Contains(t, got, "/*line /<generated>:1*/ gen2()")
	assert.Contains(t, got, "//line /<generated>:1\nfunc gen3() {}")
	assert.Contains(t, got, "/*line /<generated>:1*/ gen4()")
	assert.Equal(t, 4, strings.Count(got, "<generated>"), got)
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
	assert.Contains(t, string(got), "/*line /<generated>:1*/ gen1()")
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
	/*line /<generated>:1*/ a()
	/*line FILE:4*/ a()
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
		assert.Regexp(t, generatedPosition, got[id], id)
	}
	// The deferred function is called from the closing brace of target.
	assert.Equal(t, pos(lineOf(`"G"`)+1), got["closing"], "line of the closing brace")
}

func TestMarkLineDirectives_CompilerReportsOriginalLines(t *testing.T) {
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
	// The lines are the same as in the original file. A directive has no
	// column, so the compiler does not give a column for the code after it.
	assert.Equal(t, "4", got[0][1])
	assert.Equal(t, want[0][1], got[0][1])
	assert.Empty(t, got[0][2])
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
		assert.Regexp(t, generatedPosition, got[id], id)
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
	assert.Regexp(t, generatedPosition, got["gen"])
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
	assert.Regexp(t, generatedPosition, got["gen"])
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
	assert.Regexp(t, generatedPosition, got["gen"])
	assert.NotContains(t, run.output, "INJECTED")
}

// unsafeDirective is a "//line" comment with a file name that would close a
// block comment and add code. The pass must not write this name.
const unsafeDirective = `//line /tmp/x/*/;println("INJECTED");/*orig.go:30:1`

// preludeProgram is the start of the programs below. It has the function
// where, which gives the position of its caller.
// generatedPosition matches the position that runtime.Caller gives for
// injected code. The name is absolute, so it is the same in all Go versions.
const generatedPosition = `^/<generated>:\d+$`

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
		assert.Regexp(t, generatedPosition, got[id], id)
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
	assert.Regexp(t, generatedPosition, got["gen"])
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
	assert.Regexp(t, generatedPosition, got["gen"])
	assert.NotContains(t, run.output, "INJECTED")
}

func TestMarkLineDirectives_Directive(t *testing.T) {
	tests := []struct {
		name string
		file string
		line int
		want string
		ok   bool
	}{
		{name: "line", file: "/a/b.go", line: 7, want: "/a/b.go:7", ok: true},
		{name: "line zero", file: "/a/b.go", line: 0},
		{name: "negative line", file: "/a/b.go", line: -1},
		{name: "largest line", file: "/a/b.go", line: 1 << 30, want: "/a/b.go:1073741824", ok: true},
		{name: "line too large", file: "/a/b.go", line: 1<<30 + 1},
		{name: "unsafe name", file: "/a/*/b.go", line: 1},
		{name: "name ends in a number", file: "/a/b:0", line: 7},
		{name: "name ends in a nonzero number", file: "/a/b:7", line: 7},
		{name: "name has a number inside", file: "/a/b:0/c.go", line: 7, want: "/a/b:0/c.go:7", ok: true},
		{name: "name ends in a colon", file: "/a/b:", line: 7, want: "/a/b::7", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := directive(tt.file, tt.line)
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
	// A name that ends in ":" and digits is valid in the source, because the
	// directive has a line and a column. Go reads a directive with only a line
	// as the end of the name, then a line and a column. With ":0", Go rejects
	// the directive. With ":7", Go accepts it, but the file and the line are
	// wrong. So the pass does not restore a node in such a file, and the code
	// stays in the "<generated>" file. A later node in a safe file is restored.
	for _, name := range []string{"/tmp/foo:0", "/tmp/foo:7"} {
		t.Run(name, func(t *testing.T) { compilerNameEndsInNumber(t, name) })
	}
}

func compilerNameEndsInNumber(t *testing.T, name string) {
	t.Helper()
	const ok = "/tmp/ok.go"
	t.Run("top-level declaration", func(t *testing.T) {
		src := preludeProgram + `//line ` + name + `:12:1
func h() {}

func g() {
	fmt.Println("G", where())
}

//line ` + ok + `:40:1
func k() {
	fmt.Println("K", where())
}

func main() {
	g()
	k()
}
`
		edit := func(root *dst.File) {
			root.Decls = append(root.Decls[:4], append([]dst.Decl{genDecl(t)}, root.Decls[4:]...)...)
		}
		run := runInjected(t, src, edit)
		got := positions(run.output)
		assert.Regexp(t, generatedPosition, got["G"])
		assert.Equal(t, ok+":41", got["K"])
		assert.Contains(t, instrumentSource(t, src, edit), "\n//line "+ok+":40\nfunc k() {")
	})
	t.Run("statement", func(t *testing.T) {
		src := preludeProgram + `func main() {
//line ` + name + `:12:1
fmt.Println("S0", where())
fmt.Println("S1", where())
//line ` + ok + `:50:1
	fmt.Println("S2", where())
	fmt.Println("S3", where())
}
`
		run := runInjected(t, src, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			body.List = insertStmts(t, body.List, 1, printWhere("gen"))
		})
		got := positions(run.output)
		assert.Regexp(t, generatedPosition, got["S1"])
		assert.Equal(t, ok+":50", got["S2"])
		assert.Equal(t, ok+":51", got["S3"])
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
		assert.Regexp(t, generatedPosition, got["closing"])
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
//line ` + ok + `:70:1
	fmt.Println("U", where())
}
`
		run := runInjected(t, src, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			clause := body.List[1].(*dst.SwitchStmt).Body.List[0].(*dst.CaseClause)
			clause.Body = append(clause.Body, snippet(t, printWhere("gen1"))...)
			body.List = insertStmts(t, body.List, 3, printWhere("gen2"))
		})
		got := positions(run.output)
		assert.Regexp(t, generatedPosition, got["gen1"])
		assert.Regexp(t, generatedPosition, got["gen2"])
		assert.Regexp(t, generatedPosition, got["T"])
		assert.Equal(t, ok+":70", got["U"])
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
		assert.Regexp(t, generatedPosition, positions(run.output)["gen"])
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
		assert.Regexp(t, generatedPosition, got["S1"])
		assert.Equal(t, file+":2", got["S2"])
	})
	t.Run("statement without injected code", func(t *testing.T) {
		// The state was known. The pass cannot restore S1, so the state
		// becomes stale, and the next statement S2, which has no comment, must
		// be restored. S2 is on line 2 of foo.go, like in the original program.
		src := preludeProgram + `func main() {
//line ` + file + `:1:1
	fmt.Println("S1", where())
	fmt.Println("S2", where())
}
`
		got := assertSamePositions(t, src, func(*dst.File) {}, "S2")
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
		assert.Equal(t, file+":2", got["C2"])
	})
}

// --- User "//line" comments in function bodies ---

// userLineSource is a program like the output of goyacc. The "//line"
// comments are at column 1 inside function bodies. other is a function that
// the test does not change.
const userLineSource = preludeProgram + `func target(x int) {
	switch x {
	case 1:
//line parser.y:42
		fmt.Println("A", where())
		fmt.Println("B", where())
	case 2:
//line parser.y:90
		fmt.Println("C", where())
	}
	fmt.Println("D", where())
}

func other(x int) {
	switch x {
	case 1:
//line other.y:10
		fmt.Println("O1", where())
		fmt.Println("O2", where())
	}
	fmt.Println("O3", where())
}

func main() {
	target(1)
	target(2)
	other(1)
}
`

// runOriginal runs src as the file orig.go of a module and returns the output
// of the program.
func runOriginal(t *testing.T, src string) string {
	t.Helper()
	dir := writeModule(t, "orig.go", src)
	out, err := goTool(t, dir, "run", "orig.go")
	require.NoError(t, err, out)
	return out
}

// assertSamePositions checks that the instrumented program prints the same
// position as the original program for each id in ids. The two programs are in
// different directories, so the check compares the base name of the file and
// the line. It returns the positions of the instrumented program.
func assertSamePositions(t *testing.T, src string, edit func(root *dst.File), ids ...string) map[string]string {
	t.Helper()
	want := positions(runOriginal(t, src))
	got := positions(runInjected(t, src, edit).output)
	base := func(pos string) string {
		file, line, _ := strings.Cut(filepath.Base(pos), ":")
		if file == "inst.go" {
			file = "orig.go"
		}
		return file + ":" + line
	}
	for _, id := range ids {
		require.NotEmpty(t, want[id], id)
		assert.Equal(t, base(want[id]), base(got[id]), id)
	}
	return got
}

func TestMarkLineDirectives_CompilerUserLineCommentInFunctionWithInjectedCode(t *testing.T) {
	// The printer indents the "//line" comment, so Go ignores it. The pass
	// must restore the position of the statement after it.
	got := assertSamePositions(t, userLineSource, func(root *dst.File) {
		body := findFunc(t, root, "target").Body
		clause := body.List[0].(*dst.SwitchStmt).Body.List[0].(*dst.CaseClause)
		clause.Body = insertStmts(t, clause.Body, 1, printWhere("gen-mid"))
		body.List = insertStmts(t, body.List, 0, printWhere("gen-start"))
		body.List = append(body.List, snippet(t, printWhere("gen-end"))...)
	}, "A", "C", "D", "O1", "O2", "O3")
	// Check that the test shows what it must: the position is in the user file.
	assert.Contains(t, got["A"], "parser.y:42")
	assert.Contains(t, got["C"], "parser.y:90")
	assert.Contains(t, got["O1"], "other.y:10")
	assert.Regexp(t, generatedPosition, got["gen-mid"])
	assert.Regexp(t, generatedPosition, got["gen-start"])
	assert.Regexp(t, generatedPosition, got["gen-end"])
}

func TestMarkLineDirectives_CompilerUserLineCommentInFunctionWithoutInjectedCode(t *testing.T) {
	// No rule changed other. Its "//line" comment must work, as the printer
	// indents the comment in every function of the file.
	got := assertSamePositions(t, userLineSource, func(root *dst.File) {
		prepend(t, findFunc(t, root, "target").Body, printWhere("gen"))
	}, "A", "B", "C", "D", "O1", "O2", "O3")
	assert.Contains(t, got["O1"], "other.y:10")
	assert.Contains(t, got["O2"], "other.y:11")
}

func TestMarkLineDirectives_CompilerUserLineCommentWithoutAnyChange(t *testing.T) {
	// The pass restores the position also when no node is generated.
	assertSamePositions(t, userLineSource, func(*dst.File) {},
		"A", "B", "C", "D", "O1", "O2", "O3")
}

func TestMarkLineDirectives_CompilerUserLineCommentAfterLastStatement(t *testing.T) {
	// The "//line" comment is in the End decorations of the last statement
	// of a block, or of a clause. The code after the block must be restored,
	// and so must the closing brace.
	const src = preludeProgram + `func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	x := len(fmt.Sprint())
	if x == 0 {
		fmt.Println("A", where())
//line z.y:50
	}
	fmt.Println("B", where())
	switch x {
	case 0:
		fmt.Println("C", where())
//line c.y:80
	case 1:
		fmt.Println("D", where())
	}
	fmt.Println("E", where())
	for range 1 {
		fmt.Println("F", where())
//line f.y:30
	}
//line g.y:100
	fmt.Println("G", where())
	fmt.Println("H", where())
	for range 1 {
		fmt.Println("I", where())
//line i.y:60
	}
}
`
	got := assertSamePositions(t, src, func(*dst.File) {},
		"A", "B", "C", "E", "F", "G", "H", "I", "closing")
	assert.Contains(t, got["B"], "z.y:51")
	assert.Contains(t, got["E"], "c.y:")
	assert.Contains(t, got["G"], "g.y:100")
	// The deferred function is called from the closing brace of main.
	assert.Contains(t, got["closing"], "i.y:61")
	t.Run("with injected code", func(t *testing.T) {
		assertSamePositions(t, src, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			ifStmt := body.List[2].(*dst.IfStmt)
			ifStmt.Body.List = append(ifStmt.Body.List, snippet(t, printWhere("gen"))...)
			body.List = insertStmts(t, body.List, 3, printWhere("gen2"))
			loop := body.List[len(body.List)-1].(*dst.RangeStmt)
			loop.Body.List = append(loop.Body.List, snippet(t, printWhere("gen3"))...)
		}, "B", "G", "H", "I", "closing")
	})
}

func TestMarkLineDirectives_CompilerUserLineCommentWithNameThatEndsInNumber(t *testing.T) {
	// The restore of S2 is skipped, because a directive with only a line
	// cannot give the name "/tmp/foo:7" to Go (see directive). S2 keeps the
	// position of S1, which is a line of the original file, and not
	// "<generated>". S3 has no comment, but the state is still stale, so its
	// restore is tried, and skipped. S4 is in a safe file, and it must be
	// restored.
	const ok = "/tmp/ok.go"
	for _, name := range []string{"/tmp/foo:0", "/tmp/foo:7"} {
		t.Run(name, func(t *testing.T) {
			src := preludeProgram + `func main() {
	fmt.Println("S1", where())
//line ` + name + `:50:1
	fmt.Println("S2", where())
	fmt.Println("S3", where())
//line ` + ok + `:70:1
	fmt.Println("S4", where())
	fmt.Println("S5", where())
}
`
			edit := func(root *dst.File) {
				prepend(t, findFunc(t, root, "main").Body, printWhere("gen"))
			}
			got := positions(runInjected(t, src, edit).output)
			for _, id := range []string{"S2", "S3"} {
				assert.NotContains(t, got[id], "/tmp/foo", id)
				assert.NotContains(t, got[id], "<generated>", id)
				assert.Contains(t, got[id], "orig.go:", id)
			}
			assert.Equal(t, ok+":70", got["S4"])
			assert.Equal(t, ok+":71", got["S5"])
			text := instrumentSource(t, src, edit)
			assert.NotContains(t, text, "/*line "+name)
			assert.Equal(t, 1, strings.Count(text, "<generated>"), text)
		})
	}
}

func TestMarkLineDirectives_CompilerUserLineCommentWithNameThatEndsInNumberNoInjectedCode(t *testing.T) {
	// The state is stale, not generated. The restore of S2 is skipped, and
	// the pass does not write "<generated>". S2 keeps the position that the
	// printer gives (orig.go), which is the line in the instrumented file.
	const ok = "/tmp/ok.go"
	src := preludeProgram + `func main() {
	fmt.Println("S1", where())
//line /tmp/foo:7:50:1
	fmt.Println("S2", where())
//line ` + ok + `:70:1
	fmt.Println("S3", where())
}
`
	edit := func(*dst.File) {}
	run := runInjected(t, src, edit)
	got := positions(run.output)
	assert.NotContains(t, got["S2"], "/tmp/foo")
	assert.NotContains(t, got["S2"], "<generated>")
	assert.Equal(t, ok+":70", got["S3"])
	assert.NotContains(t, instrumentSource(t, src, edit), "<generated>")
}

func TestMarkLineDirectives_UserLineCommentInFunctionBody(t *testing.T) {
	// The comment of the user stays. The restore directive follows it on a
	// line of its own. The line is the line before the node, as the newline
	// that follows the directive adds one.
	src := `package main

func f(x int) {
	switch x {
	case 1:
//line parser.y:42
		a()
		b()
//line next.y:10
	case 2:
		c() //line trailing.y:1
	}
	d()
	if x > 1 {
//line empty.y:20
	}
	e()
//line last.y:30
}

func g() {
	h()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		prepend(t, findFunc(t, root, "f").Body, "gen()")
	})
	want := `package main

func f(x int) {
	/*line /<generated>:1*/ gen()
	/*line FILE:4*/ switch x {
	case 1:
		//line parser.y:42
		/*line DIR/parser.y:41*/
		a()
		b()
	//line next.y:10
	/*line DIR/next.y:9*/
	case 2:
		c() //line trailing.y:1
	}
	d()
	if x > 1 {
		//line empty.y:20
		/*line DIR/empty.y:19*/
	}
	e()
	//line last.y:30
	/*line DIR/last.y:29*/
}

func g() {
	h()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_HasUserLineDirective(t *testing.T) {
	for _, tc := range []struct {
		name       string
		decs       dst.Decorations
		startsLine bool
		want       bool
	}{
		{"empty", nil, true, false},
		{"first of start", dst.Decorations{"//line a.y:1"}, true, true},
		{"first of end", dst.Decorations{"//line a.y:1"}, false, false},
		{"after newline", dst.Decorations{"\n", "//line a.y:1"}, false, true},
		{"after line comment", dst.Decorations{"// c", "//line a.y:1"}, false, true},
		{"after block comment", dst.Decorations{"/* c */", "//line a.y:1"}, false, false},
		{"block directive", dst.Decorations{"/*line a.y:1*/"}, true, false},
		{"directive of the pass", dst.Decorations{"/*line /<generated>:1*/"}, true, false},
		{"not a directive", dst.Decorations{"//lines are long"}, true, false},
		{"plain comment", dst.Decorations{"// line a.y:1"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hasUserLineDirective(tc.decs, tc.startsLine))
		})
	}
}

func TestMarkLineDirectives_UserBlockLineCommentIsNotChanged(t *testing.T) {
	// A "/*line*/" comment of the user is valid at any column, so the pass
	// does not write a directive for it.
	src := `package main

func f() {
	a()
	/*line block.y:5*/ b()
	c()
}
`
	got := instrumentSource(t, src, func(*dst.File) {})
	assert.Equal(t, src, got)
}

func TestMarkLineDirectives_CompilerUserLineCommentAfterLastClause(t *testing.T) {
	// The comment is in the End decorations of the last clause, after the
	// last statement of its body. It is the last thing before the closing
	// brace of the switch statement, so the directive for the brace must
	// follow it.
	const src = preludeProgram + `func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	switch len(fmt.Sprint()) {
	case 1:
		fmt.Println("A", where())
	default:
		fmt.Println("B", where())
//line s.y:40
	}
	for range 1 {
		fmt.Println("C", where())
		switch {
		case true:
			fmt.Println("D", where())
//line t.y:70
		}
	}
	fmt.Println("E", where())
}
`
	got := assertSamePositions(t, src, func(*dst.File) {}, "B", "C", "D", "E", "closing")
	assert.Contains(t, got["E"], "t.y:72")
	assert.Contains(t, got["closing"], "t.y:73")
}

func TestMarkLineDirectives_CompilerGeneratedNodeAfterSkippedUserLineRestore(t *testing.T) {
	// The restore of the closing brace is skipped, because the name of the
	// comment is unsafe. The state is stale. A generated statement after the
	// block must have a "<generated>" directive, as the position of the
	// statement is not known. B is in the file of the unsafe comment, so its
	// restore is skipped too. C is in a safe file, and it is restored.
	const src = preludeProgram + `func main() {
	if len(fmt.Sprint()) == 0 {
		fmt.Println("A", where())
` + unsafeDirective + `
	}
	fmt.Println("B", where())
//line safe.go:80:1
	fmt.Println("C", where())
	fmt.Println("D", where())
}
`
	edit := func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		body.List = insertStmts(t, body.List, 1, printWhere("gen"))
	}
	run := runInjected(t, src, edit)
	got := positions(run.output)
	assert.Regexp(t, generatedPosition, got["gen"])
	assert.Regexp(t, generatedPosition, got["B"])
	safe := filepath.Join(filepath.Dir(run.origFile), "safe.go")
	assert.Equal(t, safe+":80", got["C"])
	assert.Equal(t, safe+":81", got["D"])
	assert.NotContains(t, run.output, "INJECTED")
}

func TestMarkLineDirectives_CompilerUserLineCommentBetweenLabelAndStatement(t *testing.T) {
	// The comment is in the decorations of the statement of the label, and not
	// in those of the label. No generated node is before the label, so only
	// the comment makes the statement need a restore.
	const src = preludeProgram + `func main() {
	n := 0
loop:
//line labeled.y:20
	fmt.Println("A", where())
	fmt.Println("B", where())
	n++
	if n < 2 {
		goto loop
	}
outer:
inner:
//line nested.y:40
	for range 1 {
		fmt.Println("C", where())
		if n > 5 {
			goto outer
		}
		break inner
	}
	fmt.Println("D", where())
}
`
	got := assertSamePositions(t, src, func(*dst.File) {}, "A", "B", "C", "D")
	assert.Contains(t, got["A"], "labeled.y:20")
	assert.Contains(t, got["B"], "labeled.y:21")
	assert.Contains(t, got["C"], "nested.y:41")
	t.Run("with injected code", func(t *testing.T) {
		withGen := assertSamePositions(t, src, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			body.List = insertStmts(t, body.List, 1, printWhere("gen"))
		}, "A", "B", "C", "D")
		assert.Regexp(t, generatedPosition, withGen["gen"])
	})
}

func TestMarkLineDirectives_UserLineCommentBetweenLabelAndStatement(t *testing.T) {
	// The restore directive follows the comment of the user, before the
	// statement. The label has no directive, because its position is known.
	src := `package main

func f() {
loop:
//line labeled.y:20
	a()
	goto loop
}
`
	got := instrumentSource(t, src, func(*dst.File) {})
	want := `package main

func f() {
loop:
	//line labeled.y:20
	/*line DIR/labeled.y:19*/
	a()
	goto loop
}
`
	assert.Equal(t, want, got)
}

// rbraceOfIf parses the program text as the file name, and returns the
// position of the closing brace of the first "if" statement in main. A parser
// reads the directives of the file.
func rbraceOfIf(t *testing.T, name, text string) token.Position {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, text, parser.ParseComments)
	require.NoError(t, err, text)
	var pos token.Position
	found := false
	goast.Inspect(findGoFunc(file, "main"), func(n goast.Node) bool {
		if stmt, ok := n.(*goast.IfStmt); ok && !found {
			pos, found = fset.Position(stmt.Body.Rbrace), true
		}
		return !found
	})
	require.True(t, found)
	return pos
}

func findGoFunc(file *goast.File, name string) *goast.FuncDecl {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*goast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func TestMarkLineDirectives_CompilerUserLineCommentInEmptyBlock(t *testing.T) {
	// The comment is in the decorations of the opening brace of an empty
	// block. The closing brace of the block must have the position that it has
	// in the original program, and not only the code after the block.
	const src = preludeProgram + `func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	if true {
//line empty.y:50
	}
	fmt.Println("A", where())
	if len(fmt.Sprint()) == 0 {
//line /tmp/foo.go:1:1
	}
	fmt.Println("B", where())
}
`
	checkBrace := func(t *testing.T, edit func(root *dst.File)) {
		t.Helper()
		file := filepath.Join(t.TempDir(), "orig.go")
		text := instrumentSourceAt(t, file, src, edit)
		text = strings.ReplaceAll(strings.ReplaceAll(text, fileMarker, file), dirMarker, filepath.Dir(file))
		want := rbraceOfIf(t, file, src)
		got := rbraceOfIf(t, file, text)
		assert.Equal(t, want.Filename, filepath.Join(filepath.Dir(file), "empty.y"))
		assert.Equal(t, 50, want.Line)
		assert.Equal(t, want.Filename, got.Filename, text)
		assert.Equal(t, want.Line, got.Line, text)
		positionsGot := assertSamePositions(t, src, edit, "A", "B", "closing")
		assert.Contains(t, positionsGot["A"], "empty.y:51")
	}
	t.Run("without injected code", func(t *testing.T) {
		checkBrace(t, func(*dst.File) {})
	})
	t.Run("with injected code before the block", func(t *testing.T) {
		checkBrace(t, func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			body.List = insertStmts(t, body.List, 1, printWhere("gen"))
		})
	})
}

func TestMarkLineDirectives_CompilerSelectLastClauseBeforeClosingBrace(t *testing.T) {
	// The last clause of a select statement ends in injected code, or in a
	// user "//line" comment. The directive for the closing brace of the select
	// statement goes after the last statement of the clause body.
	const src = preludeProgram + `func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	ch := make(chan int, 2)
	ch <- 1
	ch <- 2
	select {
	case <-ch:
		fmt.Println("A", where())
	}
	fmt.Println("B", where())
	select {
	case <-ch:
		fmt.Println("C", where())
//line s.y:40
	}
	fmt.Println("D", where())
}
`
	edit := func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		sel := body.List[4].(*dst.SelectStmt)
		clause := sel.Body.List[0].(*dst.CommClause)
		clause.Body = append(clause.Body, snippet(t, printWhere("gen"))...)
	}
	got := assertSamePositions(t, src, edit, "A", "B", "C", "D", "closing")
	assert.Regexp(t, generatedPosition, got["gen"])
	// The comment gives line 40 to the closing brace, so D is at line 41.
	assert.Contains(t, got["D"], "s.y:41")

	// The directive is in the clause body, just before the closing brace.
	printed := instrumentSource(t, src, edit)
	assert.Regexp(t,
		`\t\t/\*line /<generated>:1\*/ fmt\.Println\("gen", where\(\)\) `+
			`/\*line FILE:\d+\*/\n\t}\n\tfmt\.Println\("B"`,
		printed)
}

func TestMarkLineDirectives_CompilerBlankLineBeforeRestoredClosingBrace(t *testing.T) {
	// The injected statement is the last in its block, and a blank line
	// follows it. The directive for the closing brace applies to the next
	// newline, so the pass removes the blank line. Else the brace and the
	// code after it are one line too low.
	const src = preludeProgram + `func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	if len(fmt.Sprint()) == 0 {
		fmt.Println("A", where())
	}
	fmt.Println("B", where())
}
`
	edit := func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		ifBody := body.List[1].(*dst.IfStmt).Body
		gen := snippet(t, printWhere("gen"))
		gen[len(gen)-1].Decorations().After = dst.EmptyLine
		ifBody.List = append(ifBody.List, gen...)
	}
	got := assertSamePositions(t, src, edit, "A", "B", "closing")
	assert.Regexp(t, generatedPosition, got["gen"])
	assert.NotContains(t, instrumentSource(t, src, edit), "*/\n\n", "blank line before the closing brace")
}

func TestGeneratedFile(t *testing.T) {
	// On Windows the formula adds the volume of the file, for example
	// `C:\<generated>`. On Unix the volume is empty.
	assert.Equal(t, "/<generated>", generatedFile("/work/dir/main.go"))
	assert.True(t, filepath.IsAbs(generatedFile("/work/dir/main.go")))
}

func TestGeneratedFileOnUnsafeVolume(t *testing.T) {
	// A volume that is not safe in a comment gives the relative name, which
	// is always safe.
	assert.Equal(t, generatedName, generatedFileOnVolume("bad\nvolume"))
	assert.Equal(t, generatedName, generatedFileOnVolume("bad*/volume"))
}

func TestMarkLineDirectives_CompilerGeneratedStatementOfOriginalLabel(t *testing.T) {
	// A rule replaces the statement of an original label with generated code.
	// The generated statement is marked, and the original statement after it
	// is restored.
	const src = preludeProgram + `func main() {
L:
	for range 1 {
		fmt.Println("A", where())
		break L
	}
	fmt.Println("B", where())
}
`
	edit := func(root *dst.File) {
		labeled := findFunc(t, root, "main").Body.List[0].(*dst.LabeledStmt)
		labeled.Stmt = snippet(t, "for range 1 {\n"+printWhere("gen")+"\nbreak L\n}")[0]
	}
	want := positions(runOriginal(t, src))
	run := runInjected(t, src, edit)
	got := positions(run.output)
	assert.Regexp(t, generatedPosition, got["gen"])
	// B is restored in the original file, with its original line.
	_, wantLine, _ := strings.Cut(filepath.Base(want["B"]), ":")
	assert.Equal(t, run.origFile+":"+wantLine, got["B"])
	assert.Contains(t, instrumentSource(t, src, edit), "L:\n\t/*line /<generated>:1*/")
}

func TestMarkLineDirectives_CompilerUserLineCommentInsideExpression(t *testing.T) {
	// The printer indents a "//line" comment in the arguments of a call or in
	// a composite literal, so Go ignores it. The state must become stale, and
	// the statement after the expression must be restored. This is also true
	// when the comment is the only content of the parentheses or the braces,
	// and when it follows the last element.
	const src = preludeProgram + `func sink(...any) {}

type pair struct{ a, b int }

func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	sink(
//line mapped.y:42
		1,
	)
	fmt.Println("A", where())
	_ = pair{
//line mapped.y:50
		1, 2,
	}
	fmt.Println("B", where())
	sink(
//line mapped.y:60
	)
	fmt.Println("C", where())
	_ = []int{
//line mapped.y:70
	}
	fmt.Println("D", where())
	sink(
		1,
//line mapped.y:80
	)
	fmt.Println("E", where())
}
`
	got := assertSamePositions(t, src, func(*dst.File) {}, "A", "B", "C", "D", "E", "closing")
	// Check that the test shows what it must: the position is in the user file.
	assert.Contains(t, got["A"], "mapped.y:44")
	assert.Contains(t, got["B"], "mapped.y:52")
	assert.Contains(t, got["C"], "mapped.y:61")
	assert.Contains(t, got["D"], "mapped.y:71")
	assert.Contains(t, got["E"], "mapped.y:81")
}

func TestMarkLineDirectives_CompilerUserLineCommentInsideClauseExpression(t *testing.T) {
	// The expression of a "case" and the statement of a "select" clause are
	// not statements of a list. A "//line" comment before them must also make
	// the state stale, also when the comment is in the decorations of the
	// expression itself.
	const src = preludeProgram + `func main() {
	ch := make(chan int, 2)
	ch <- 1
	switch 1 {
	case
//line case.y:30
		1:
		fmt.Println("A", where())
	}
	fmt.Println("B", where())
	switch 1 {
	case 2,
//line case.y:40
		1:
		fmt.Println("C", where())
	}
	fmt.Println("D", where())
	select {
	case
//line comm.y:50
		ch <- 1:
		fmt.Println("E", where())
	}
	fmt.Println("F", where())
	switch 1 {
	case call(func() {
		_ = 0
	}) +
//line case.y:60
		0:
	default:
		fmt.Println("never", where())
	}
	fmt.Println("G", where())
}
`
	assertSamePositions(t, src, func(*dst.File) {}, "A", "B", "C", "D", "E", "F", "G")
}

// multiLineWrapper returns an expression that is not original and that has
// several lines when it is printed. It is like the wrapper of a call rule.
func multiLineWrapper(t *testing.T, code string) dst.Expr {
	t.Helper()
	stmts := snippet(t, "_ = func() int {\n\tfmt.Println("+`"gen"`+", where())\n\treturn "+code+"\n}()")
	return stmts[0].(*dst.AssignStmt).Rhs[0]
}

func TestMarkLineDirectives_CompilerGeneratedExpressionInsideOriginalStatement(t *testing.T) {
	// A rule replaces an expression with code of several lines. The code
	// before the statement was restored, so the state is known. The state
	// must become stale after the statement, and the next statement and the
	// closing brace must be restored.
	const src = preludeProgram + `func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	fmt.Println("A", where())
	_ = call(func() {})
	fmt.Println("B", where())
	if call(func() {}) > 0 {
		fmt.Println("C", where())
	}
	fmt.Println("D", where())
	_ = call(func() {})
}
`
	wrap := func(t *testing.T, root *dst.File, index int) {
		body := findFunc(t, root, "main").Body
		assign := body.List[index].(*dst.AssignStmt)
		assign.Rhs[0] = multiLineWrapper(t, "call(func() {})")
	}
	t.Run("in the middle of a block", func(t *testing.T) {
		got := assertSamePositions(t, src, func(root *dst.File) {
			prepend(t, findFunc(t, root, "main").Body, printWhere("start"))
			wrap(t, root, 3)
		}, "A", "B", "C", "D", "closing")
		assert.Regexp(t, generatedPosition, got["start"])
	})
	t.Run("in the condition of a statement with a block", func(t *testing.T) {
		got := assertSamePositions(t, src, func(root *dst.File) {
			prepend(t, findFunc(t, root, "main").Body, printWhere("start"))
			cond := findFunc(t, root, "main").Body.List[5].(*dst.IfStmt)
			cond.Cond = &dst.BinaryExpr{
				X:  multiLineWrapper(t, "call(func() {})"),
				Op: token.GTR,
				Y:  &dst.BasicLit{Kind: token.INT, Value: "0"},
			}
		}, "A", "B", "C", "D", "closing")
		assert.Regexp(t, generatedPosition, got["start"])
	})
	t.Run("in the last statement of a block", func(t *testing.T) {
		got := assertSamePositions(t, src, func(root *dst.File) {
			prepend(t, findFunc(t, root, "main").Body, printWhere("start"))
			wrap(t, root, 7)
		}, "A", "B", "C", "D", "closing")
		assert.Regexp(t, generatedPosition, got["start"])
	})
}

func TestMarkLineDirectives_CompilerGeneratedFieldInOriginalStruct(t *testing.T) {
	// A struct rule adds a field to an original type declaration. The field
	// has a line in the printed file, so the declarations after it are on
	// wrong lines until the pass restores the next one.
	const src = preludeProgram + `type T struct {
	A int
}

func next() string {
	return where()
}

func main() {
	fmt.Println("A", next())
	fmt.Println("B", where())
}
`
	edit := func(root *dst.File) {
		for _, decl := range root.Decls {
			gen, ok := decl.(*dst.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			fields := gen.Specs[0].(*dst.TypeSpec).Type.(*dst.StructType).Fields
			fields.List = append(fields.List, &dst.Field{
				Names: []*dst.Ident{dst.NewIdent("Added")},
				Type:  dst.NewIdent("string"),
			})
		}
	}
	assertSamePositions(t, src, edit, "A", "B")
	// The declaration after the struct has a restore directive.
	printed := instrumentSource(t, src, edit)
	assert.Contains(t, printed, "Added string\n}\n\n//line FILE:")
}

func TestMarkLineDirectives_CompilerGeneratedBlockInsideOriginalStatement(t *testing.T) {
	// A rule replaces the body of an original "if" statement with a block of
	// several lines. The pass does not enter the block, and the state becomes
	// stale, so the statement after the "if" and the closing brace are
	// restored.
	const src = preludeProgram + `func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	fmt.Println("A", where())
	if call(func() {}) > 0 {
		fmt.Println("B", where())
	}
	fmt.Println("C", where())
}
`
	edit := func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		prepend(t, body, printWhere("start"))
		ifStmt := body.List[3].(*dst.IfStmt)
		gen := printWhere("gen1") + "\n" + printWhere("gen2") + "\n" + printWhere("gen3")
		ifStmt.Body = &dst.BlockStmt{List: snippet(t, gen)}
	}
	got := assertSamePositions(t, src, edit, "A", "C", "closing")
	assert.Regexp(t, generatedPosition, got["start"])
	assert.NotContains(t, got, "B")
}

func TestMarkLineDirectives_CompilerUserLineCommentAfterFuncLiteralArgument(t *testing.T) {
	// The comment is before the ")" of the call, so the printer puts it after
	// the body of the function literal. The walk sees it before the body, and
	// the body restores the state. The statement after the call must still be
	// restored.
	const src = preludeProgram + `func sink(...any) {}

func main() {
	sink(func() { fmt.Println("inner", where()) },
//line mapped.y:50
	)
	fmt.Println("after", where())
}
`
	got := assertSamePositions(t, src, func(*dst.File) {}, "after")
	assert.Contains(t, got["after"], "mapped.y:51")
}

func TestMarkLineDirectives_CompilerUserLineCommentBetweenImportSpecs(t *testing.T) {
	// The printer indents a "//line" comment between the specs of a grouped
	// import, so Go ignores it. The next declaration must be restored.
	const src = `package main

import (
	"fmt"
//line mapped.y:50
	"runtime"
)

func where() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}

func main() {
	fmt.Println("M", where())
}
`
	got := assertSamePositions(t, src, func(*dst.File) {}, "M")
	assert.Contains(t, got["M"], "mapped.y:")
}

func TestMarkLineDirectives_CompilerCommentAfterGeneratedCodeInLastClause(t *testing.T) {
	// The last clause of each switch or select ends with generated code, and a
	// comment follows it. The comment is in the End decorations of the clause.
	// The printer prints it after the body of the clause, so the directive for
	// the closing brace must follow the comment.
	const src = preludeProgram + `func main() {
	defer func() {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("closing %s:%d\n", file, line)
	}()
	switch len(fmt.Sprint()) {
	case 1:
		fmt.Println("A", where())
	default:
		fmt.Println("B", where())
		// A comment after the last statement.

		// One more comment.
	}
	fmt.Println("C", where())
	ch := make(chan int, 1)
	ch <- 1
	select {
	case <-ch:
		fmt.Println("D", where())
		/* A block comment. */
	}
	fmt.Println("E", where())
}
`
	edit := func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		sw := body.List[1].(*dst.SwitchStmt)
		last := sw.Body.List[len(sw.Body.List)-1].(*dst.CaseClause)
		last.Body = append(last.Body, snippet(t, printWhere("gen-switch"))...)
		sel := body.List[5].(*dst.SelectStmt)
		comm := sel.Body.List[0].(*dst.CommClause)
		comm.Body = append(comm.Body, snippet(t, printWhere("gen-select"))...)
	}
	got := assertSamePositions(t, src, edit, "B", "C", "D", "E", "closing")
	assert.Regexp(t, generatedPosition, got["gen-switch"])
	assert.Regexp(t, generatedPosition, got["gen-select"])
}

func TestMarkLineDirectives_CompilerUserLineCommentInEmptyImportGroup(t *testing.T) {
	// The "//line" comment is in an empty import group. It is in the
	// decorations of the declaration, and not in those of a spec. The pass
	// does not look at these decorations. The printer does not indent a
	// comment in an empty group, so Go still reads it, and the positions after
	// it are correct without a restore.
	const src = `package main

import (
//line mapped.go:100
)

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
}
`
	got := assertSamePositions(t, src, func(*dst.File) {}, "A")
	assert.Contains(t, got["A"], "mapped.go:")
	t.Run("with injected code", func(t *testing.T) {
		withGen := assertSamePositions(t, src, func(root *dst.File) {
			prepend(t, findFunc(t, root, "main").Body, printWhere("gen"))
		}, "A")
		assert.Regexp(t, generatedPosition, withGen["gen"])
	})
}

func TestMarkLineDirectives_CommentAfterGeneratedCodeInLastClause(t *testing.T) {
	// A comment after the last statement of a clause is in the End decorations
	// of the clause. The directive for the closing brace must follow it, for
	// a comment on a line of its own and for a comment on the same line.
	src := `package main

func f(x int) {
	switch x {
	case 1:
		a()
		// Own line.
	}
	switch x {
	case 1:
		b() // Same line.
	}
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		for i, name := range []string{"genOwn()", "genSame()"} {
			clause := body.List[i].(*dst.SwitchStmt).Body.List[0].(*dst.CaseClause)
			clause.Body = append(clause.Body, snippet(t, name)...)
		}
	})
	want := `package main

func f(x int) {
	switch x {
	case 1:
		a()
		/*line /<generated>:1*/ genOwn()

		// Own line.
		/*line FILE:7*/
	}
	switch x {
	case 1:
		b()
		/*line /<generated>:1*/ genSame()
		// Same line.
		/*line FILE:11*/
	}
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_CompilerEmptyBlockAfterGeneratedExpression(t *testing.T) {
	// A rule replaces the condition of an "if" statement with code of several
	// lines. The block of the statement is empty, so no statement in it can
	// restore the position. The closing brace of the block must be restored,
	// else the condition of the "else if" is on a wrong line.
	const src = preludeProgram + `func at(id string) bool {
	_, file, line, _ := runtime.Caller(1)
	fmt.Printf("%s %s:%d\n", id, file, line)
	return true
}

func main() {
	fmt.Println("A", where())
	if call(func() {}) < 0 {
	} else if at("B") {
		fmt.Println("C", where())
	}
	if call(func() {}) < 0 {} else if at("D") {
		fmt.Println("E", where())
	}
}
`
	got := assertSamePositions(t, src, func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		prepend(t, body, printWhere("start"))
		for _, index := range []int{2, 3} {
			cond := body.List[index].(*dst.IfStmt)
			cond.Cond = &dst.BinaryExpr{
				X:  multiLineWrapper(t, "call(func() {})"),
				Op: token.LSS,
				Y:  &dst.BasicLit{Kind: token.INT, Value: "0"},
			}
		}
	}, "A", "B", "C", "D", "E")
	assert.Regexp(t, generatedPosition, got["start"])
}

func TestMarkLineDirectives_CompilerUserLineCommentWithoutFileName(t *testing.T) {
	// A "//line :L" comment of the user has no file name and no column, so Go
	// gives an empty file name to the code after it. A restore directive must
	// keep the empty file name, and not replace it with the directory of the
	// file.
	const src = preludeProgram + `func main() {
	fmt.Println("A", where())
//line :100
	fmt.Println("B", where())
	fmt.Println("C", where())
}
`
	for name, edit := range map[string]func(root *dst.File){
		"without injected code": func(*dst.File) {},
		"with injected code": func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			body.List = insertStmts(t, body.List, 1, printWhere("gen"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := assertSamePositions(t, src, edit, "A", "B", "C")
			assert.Equal(t, "??:100", got["B"])
			assert.Equal(t, "??:101", got["C"])
		})
	}
}

func TestMarkLineDirectives_EmptyBlockAfterGeneratedExpression(t *testing.T) {
	// The directive for the closing brace of an empty block is after the
	// opening brace and its comments. A newline follows the directive, so the
	// closing brace is on the next line.
	src := `package main

func f() {
	if g() < 0 {
	} else if h() {
	}
	if g() < 0 {} else if h() {
	}
	if g() < 0 { /* c */ }
	k()
}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		for i := range 3 {
			stmt := body.List[i].(*dst.IfStmt)
			stmt.Cond = &dst.BinaryExpr{
				X:  multiLineWrapper(t, "g()"),
				Op: token.LSS,
				Y:  &dst.BasicLit{Kind: token.INT, Value: "0"},
			}
		}
	})
	want := `package main

func f() {
	if func() int {
		fmt.Println("gen", where())
		return g()
	}() < 0 {
		/*line FILE:4*/
	} else if h() {
	}
	/*line FILE:7*/ if func() int {
		fmt.Println("gen", where())
		return g()
	}() < 0 { /*line FILE:6*/
	} else if h() {
	}
	/*line FILE:9*/ if func() int {
		fmt.Println("gen", where())
		return g()
	}() < 0 { /* c */ /*line FILE:8*/
	}
	/*line FILE:10*/ k()
}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_CompilerGeneratedStatementWithLeadingLineComment(t *testing.T) {
	// A generated statement starts with a "//line" comment, or with a comment
	// that only looks like one. The printer indents the comment in the
	// function body, so Go ignores it. The statement must still be in the
	// "<generated>" file.
	for _, comment := range []string{"//line foo.go:10", "//line of the rule"} {
		t.Run(comment, func(t *testing.T) {
			const src = preludeProgram + `func main() {
	fmt.Println("A", where())
}
`
			got := assertSamePositions(t, src, func(root *dst.File) {
				gen := snippet(t, printWhere("gen"))
				gen[0].Decorations().Start.Append(comment)
				body := findFunc(t, root, "main").Body
				body.List = append(gen, body.List...)
			}, "A")
			assert.Regexp(t, generatedPosition, got["gen"])
		})
	}
}

func TestMarkLineDirectives_GeneratedNodeWithLeadingDirective(t *testing.T) {
	// The pass does not add a "<generated>" directive before a directive that
	// Go reads: a "/*line*/" comment in a function body, and a "//line" or
	// "/*line*/" comment before a top-level declaration. It adds one before a
	// comment that Go does not read.
	src := `package main

func f() {
	a()
	b()
	c()
}

type A int

type B int

type C int
`
	got := instrumentSource(t, src, func(root *dst.File) {
		body := findFunc(t, root, "f").Body
		list := make([]dst.Stmt, 0, 2*len(body.List))
		for i, comment := range []string{"/*line foo.go:10*/", "//line foo.go:20", "/*line of the rule*/"} {
			stmt := snippet(t, "gen()")[0]
			stmt.Decorations().Before = dst.NewLine
			stmt.Decorations().Start.Append(comment)
			list = append(list, stmt, body.List[i])
		}
		body.List = list
		decls := make([]dst.Decl, 0, 2*len(root.Decls))
		decls = append(decls, root.Decls[0])
		for i, comment := range []string{"//line foo.go:30", "/*line foo.go:40*/", "//line of the rule"} {
			extra, err := ast.NewAstParser().ParseSource("package main\n\n" + comment + "\nvar _ int\n")
			require.NoError(t, err)
			decls = append(decls, extra.Decls[0], root.Decls[i+1])
		}
		root.Decls = decls
	})
	want := `package main

func f() {
	/*line foo.go:10*/ gen()
	/*line FILE:4*/ a()
	//line foo.go:20
	/*line /<generated>:1*/
	gen()
	/*line FILE:5*/ b()
	/*line of the rule*/ /*line /<generated>:1*/ gen()
	/*line FILE:6*/ c()
}

//line foo.go:30
var _ int

//line FILE:9
type A int

/*line foo.go:40*/
var _ int

//line FILE:11
type B int

//line of the rule
//line /<generated>:1
var _ int

//line FILE:13
type C int
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_CompilerAddedImportBeforeSkippedRestore(t *testing.T) {
	// A rule adds an import spec. The pass does not write a "<generated>"
	// directive for it, so the code after it is still in the user file. The
	// restore of the next declaration is skipped, because its file name ends
	// in a number. A generated statement in that declaration must still be in
	// the "<generated>" file.
	const src = `package main

import (
	"fmt"
	"runtime"
)

//line /tmp/foo:7:50:1
func main() {
	fmt.Println("A", where())
}

//line /tmp/ok.go:10:1
func where() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}
`
	edit := func(root *dst.File) {
		importDecl := root.Decls[0].(*dst.GenDecl)
		spec := &dst.ImportSpec{Name: dst.NewIdent("_"), Path: &dst.BasicLit{Kind: token.STRING, Value: `"unsafe"`}}
		importDecl.Specs = append(importDecl.Specs, spec)
		prepend(t, findFunc(t, root, "main").Body, printWhere("gen"))
	}
	// A is not compared: the added spec moves the lines, and its restore is
	// skipped, so it is off. This is a known limit (see the top of
	// line_directives.go).
	got := positions(runInjected(t, src, edit).output)
	assert.Regexp(t, generatedPosition, got["gen"])
}

func TestMarkLineDirectives_CompilerUserLineCommentBeforeElseBlock(t *testing.T) {
	// The "//line" comment is between "else" and its block. It is in the Start
	// decorations of the block. The printer indents it, so Go ignores it. The
	// statements of the "else" block must be restored, also when the first
	// block was restored. The same applies to a "switch" with the comment
	// before "{".
	const src = preludeProgram + `func main() {
	if len(fmt.Sprint()) > 0 {
		fmt.Println("A", where())
	} else
//line mapped.go:100
	{
		fmt.Println("B", where())
	}
	if len(fmt.Sprint()) > 0 {
		fmt.Println("C", where())
	} else
//line mapped.go:200
	{
		fmt.Println("D", where())
	}
	fmt.Println("E", where())
	switch
//line mapped.go:300
	{
	case true:
		fmt.Println("F", where())
	}
	fmt.Println("G", where())
}
`
	got := assertSamePositions(t, src, func(*dst.File) {}, "B", "D", "E", "F", "G")
	assert.Contains(t, got["B"], "mapped.go:101")
	assert.Contains(t, got["D"], "mapped.go:201")
	assert.Contains(t, got["F"], "mapped.go:302")
	t.Run("with injected code", func(t *testing.T) {
		withGen := assertSamePositions(t, src, func(root *dst.File) {
			prepend(t, findFunc(t, root, "main").Body, printWhere("gen"))
		}, "B", "D", "E", "F", "G")
		assert.Regexp(t, generatedPosition, withGen["gen"])
	})
}

func TestMarkLineDirectives_CompilerGeneratedRunAfterOwnDirective(t *testing.T) {
	// The first generated statement has its own "/*line*/" directive, and Go
	// reads it. The next generated statement is not in the "<generated>" file
	// then, so it needs a "<generated>" directive.
	const src = preludeProgram + `func main() {
	fmt.Println("A", where())
}
`
	got := assertSamePositions(t, src, func(root *dst.File) {
		own := snippet(t, printWhere("own"))[0]
		own.Decorations().Before = dst.NewLine
		own.Decorations().Start.Append("/*line foo.go:10*/")
		body := findFunc(t, root, "main").Body
		body.List = append([]dst.Stmt{own, snippet(t, printWhere("gen"))[0]}, body.List...)
	}, "A")
	assert.Equal(t, "foo.go:10", filepath.Base(got["own"]))
	assert.Regexp(t, generatedPosition, got["gen"])
}

func TestMarkLineDirectives_GeneratedDeclarationRunAfterOwnDirective(t *testing.T) {
	// The same rule applies at the top level.
	src := `package main

func f() {}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		for _, code := range []string{"//line foo.go:30\nvar _ int", "var _ int"} {
			extra, err := ast.NewAstParser().ParseSource("package main\n\n" + code + "\n")
			require.NoError(t, err)
			root.Decls = append(root.Decls, extra.Decls[0])
		}
	})
	want := `package main

func f() {}

//line foo.go:30
var _ int

//line /<generated>:1
var _ int
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_CompilerGeneratedDeclarationBeforeSkippedRestore(t *testing.T) {
	// A generated declaration is before an original function. The restore of
	// the function is skipped, because its file name ends in a number. Go reads
	// the "//line" comment of the user before the function, so the code of the
	// function is not in the "<generated>" file. A generated statement in the
	// function needs a "<generated>" directive.
	const src = `package main

import (
	"fmt"
	"runtime"
)

//line /tmp/foo:7:50:1
func main() {
	fmt.Println("A", where())
}

//line /tmp/ok.go:10:1
func where() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}
`
	edit := func(root *dst.File) {
		extra, err := ast.NewAstParser().ParseSource("package main\n\nvar _ int\n")
		require.NoError(t, err)
		root.Decls = append(root.Decls[:1], append([]dst.Decl{extra.Decls[0]}, root.Decls[1:]...)...)
		prepend(t, findFunc(t, root, "main").Body, printWhere("gen"))
	}
	// A is not compared: its restore is skipped, so its position is wrong.
	// This is a known limit (see the top of line_directives.go).
	got := positions(runInjected(t, src, edit).output)
	assert.Regexp(t, generatedPosition, got["gen"])
}

func TestMarkLineDirectives_IsLineDirective(t *testing.T) {
	for _, tc := range []struct {
		comment string
		want    bool
	}{
		{"//line foo.go:10", true},
		{"//line foo.go:10:2", true},
		{"//line :10", true},
		{"/*line foo.go:10*/", true},
		{"/*line foo.go:10:2*/", true},
		{"//line of the rule", false},
		{"//line foo.go:0", false},
		{"//line foo.go:abc", false},
		{"//line foo.go:+1", false},
		{"//line foo.go:1073741825", false},
		{"//line foo.go:10\n//line bar.go:1", false},
		{"//linefoo.go:10", false},
		{"// line foo.go:10", false},
		{"/*line foo.go:10", false},
		{"/*line plain*/ /* note:1*/", false},
		{"/*line */", false},
		{"/* line foo.go:10*/", false},
		{"", false},
	} {
		assert.Equal(t, tc.want, isLineDirective(tc.comment), "%q", tc.comment)
	}
}

func TestMarkLineDirectives_CompilerUserBlockDirectiveWithSkippedRestore(t *testing.T) {
	// An original node has a "/*line*/" comment of the user, and Go reads it.
	// Its file name ends in a number, so the restore is skipped. The code
	// after the comment is then not in the "<generated>" file, so the next
	// generated statement needs a "<generated>" directive. The comment can be
	// before a statement, before the statement of a label, or inside a
	// statement (here, after the condition of an "if").
	const src = preludeProgram + `func main() {
	fmt.Println("A", where())
	/*line /tmp/foo:7:50:1*/ fmt.Println("B", where())
	fmt.Println("C", where())
	n := 0
loop:
	/*line /tmp/foo:7:60:1*/ fmt.Println("D", where())
	n++
	if n < 1 {
		goto loop
	}
//line /tmp/foo:7:70:1
	if n > 0 /*line /tmp/bar.go:90*/ {
		fmt.Println("E", where())
	}
}
`
	edit := func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		ifStmt := body.List[7].(*dst.IfStmt)
		ifStmt.Body.List = insertStmts(t, ifStmt.Body.List, 0, printWhere("gen-if"))
		labeled := body.List[4].(*dst.LabeledStmt)
		body.List = insertStmts(t, body.List, 7, printWhere("gen-before-if"))
		body.List = insertStmts(t, body.List, 5, printWhere("gen-after-label"))
		body.List = insertStmts(t, body.List, 4, printWhere("gen-before-label"))
		body.List = insertStmts(t, body.List, 2, printWhere("gen-after-B"))
		body.List = insertStmts(t, body.List, 1, printWhere("gen-before-B"))
		require.Same(t, labeled, body.List[7])
	}
	// The other statements are not compared: their restores are skipped, so
	// they keep a wrong position. This is a known limit.
	got := positions(runInjected(t, src, edit).output)
	for _, id := range []string{"gen-before-B", "gen-after-B", "gen-before-label", "gen-after-label", "gen-before-if", "gen-if"} {
		assert.Regexp(t, generatedPosition, got[id], id)
	}
	assert.Equal(t, "/tmp/foo:7:50", got["B"])
	assert.Equal(t, "/tmp/foo:7:60", got["D"])
	assert.Equal(t, "/tmp/bar.go:91", got["E"])
}

func TestMarkLineDirectives_TopLevelDirectiveAfterBlockComment(t *testing.T) {
	// A top-level declaration starts with a "/*...*/" comment, and no newline
	// follows it. Go reads a "//line" directive only at column 1, so the pass
	// puts the directive on a new line. This applies to a generated
	// declaration and to the restore of an original declaration.
	src := `package main

/* a */ /* b */ func f() {}
`
	got := instrumentSource(t, src, func(root *dst.File) {
		extra, err := ast.NewAstParser().ParseSource("package main\n\nvar _ int\n")
		require.NoError(t, err)
		gen := extra.Decls[0]
		gen.Decorations().Start.Append("/* note */")
		root.Decls = append([]dst.Decl{gen}, root.Decls...)
	})
	want := `package main

/* note */
//line /<generated>:1
var _ int

/* a */ /* b */
//line FILE:3
func f() {}
`
	assert.Equal(t, want, got)
}

func TestMarkLineDirectives_CompilerDirectiveInsideOrAfterGeneratedNode(t *testing.T) {
	// Go reads a "/*line*/" comment inside or after a generated node. The code
	// after it is then not in the "<generated>" file, so the next generated
	// node needs a "<generated>" directive.
	const src = preludeProgram + `func main() {
	fmt.Println("A", where()) /*line other.go:50*/
	fmt.Println("B", where())
}
`
	for name, edit := range map[string]func(root *dst.File){
		"after a generated statement": func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			first := snippet(t, printWhere("first"))[0]
			first.Decorations().End.Append("/*line other.go:50*/")
			body.List = append([]dst.Stmt{first, snippet(t, printWhere("gen"))[0]}, body.List...)
		},
		"in one decoration with other comments": func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			first := snippet(t, printWhere("first"))[0]
			first.Decorations().End.Append("/* note */ /*line other.go:50*/")
			body.List = append([]dst.Stmt{first, snippet(t, printWhere("gen"))[0]}, body.List...)
		},
		"in a parsed snippet": func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			first := snippet(t, printWhere("first")+" /*line other.go:50*/")[0]
			require.Equal(t, dst.Decorations{"/*line other.go:50*/"}, first.Decorations().End)
			body.List = append([]dst.Stmt{first, snippet(t, printWhere("gen"))[0]}, body.List...)
		},
		"in an original statement that a rule wrapped": func(root *dst.File) {
			body := findFunc(t, root, "main").Body
			wrapper := &dst.BlockStmt{List: []dst.Stmt{body.List[0]}}
			body.List = append([]dst.Stmt{wrapper, snippet(t, printWhere("gen"))[0]}, body.List[1:]...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := positions(runInjected(t, src, edit).output)
			assert.Regexp(t, generatedPosition, got["gen"])
		})
	}
}

func TestMarkLineDirectives_CompilerUserBlockDirectiveAfterNestedBlock(t *testing.T) {
	// The "/*line*/" comment of the user is after the body of a function
	// literal. Go reads it after the body, but the pass sees it before the
	// body. The body ends in generated code, and the restore of its closing
	// brace is skipped, because the file name ends in a number. The next
	// generated statement must still get a "<generated>" directive.
	const src = preludeProgram + `func sink(f func()) { f() }

//line /tmp/foo:7:50:1
func main() {
	sink(func() {
		fmt.Println("inner", where())
	} /*line /tmp/special.go:90*/)
	fmt.Println("after", where())
}
`
	got := positions(runInjected(t, src, func(root *dst.File) {
		body := findFunc(t, root, "main").Body
		lit := body.List[0].(*dst.ExprStmt).X.(*dst.CallExpr).Args[0].(*dst.FuncLit)
		lit.Body.List = append(lit.Body.List, snippet(t, printWhere("innergen"))...)
		body.List = insertStmts(t, body.List, 1, printWhere("outergen"))
	}).output)
	assert.Regexp(t, generatedPosition, got["innergen"])
	assert.Regexp(t, generatedPosition, got["outergen"])
}

func TestMarkLineDirectives_CompilerDirectiveInOriginalCodeAfterSkippedRestore(t *testing.T) {
	// The function is in a file whose name ends in a number, so no restore in
	// it can be written. Go reads a "/*line*/" comment that is in original
	// code, or in a node that a rule added around original code. The code
	// after the comment is then not in the "<generated>" file, so the next
	// generated statement needs a "<generated>" directive.
	const prefix = preludeProgram + "//line /tmp/foo:7:50:1\n"
	ifSource := prefix + `func main() {
	if true {
		fmt.Println("inside", where())
	}
	fmt.Println("after", where())
}
`
	for _, tc := range []struct {
		name string
		src  string
		edit func(t *testing.T, body *dst.BlockStmt)
	}{
		{
			name: "after the opening brace of a block",
			src: prefix + `func main() {
	if true { /*line /tmp/bar.go:90*/
		fmt.Println("inside", where())
	}
	fmt.Println("after", where())
}
`,
			edit: func(t *testing.T, body *dst.BlockStmt) {
				stmt := body.List[0].(*dst.IfStmt)
				stmt.Body.List = insertStmts(t, stmt.Body.List, 0, printWhere("gen"))
			},
		},
		{
			name: "after the closing brace of a block",
			src:  ifSource,
			edit: func(t *testing.T, body *dst.BlockStmt) {
				stmt := body.List[0].(*dst.IfStmt)
				stmt.Body.Decs.End.Append("/*line /tmp/bar.go:90*/")
				body.List = insertStmts(t, body.List, 1, printWhere("gen"))
			},
		},
		{
			name: "in a block that a rule added",
			src:  ifSource,
			edit: func(t *testing.T, body *dst.BlockStmt) {
				stmt := body.List[0].(*dst.IfStmt)
				block := &dst.BlockStmt{List: stmt.Body.List}
				block.Decs.Lbrace.Append("/*line /tmp/bar.go:90*/")
				stmt.Body = block
				body.List = insertStmts(t, body.List, 1, printWhere("gen"))
			},
		},
		{
			name: "in an expression that a rule added",
			src:  ifSource,
			edit: func(t *testing.T, body *dst.BlockStmt) {
				stmt := body.List[0].(*dst.IfStmt)
				call := stmt.Body.List[0].(*dst.ExprStmt).X.(*dst.CallExpr)
				arg := call.Args[1]
				arg.Decorations().End.Append("/*line /tmp/bar.go:90*/")
				call.Args[1] = &dst.ParenExpr{X: arg}
				stmt.Body.List = append(stmt.Body.List, snippet(t, printWhere("gen"))...)
			},
		},
		{
			name: "after other comments",
			src: prefix + `func main() {
	fmt.Println("inside", where()) /* note */ /*line /tmp/bar.go:90*/
	fmt.Println("after", where())
}
`,
			edit: func(t *testing.T, body *dst.BlockStmt) {
				body.List = insertStmts(t, body.List, 1, printWhere("gen"))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := positions(runInjected(t, tc.src, func(root *dst.File) {
				body := findFunc(t, root, "main").Body
				tc.edit(t, body)
				// A generated statement before the code makes the state
				// generated. The restore of the code is skipped.
				body.List = insertStmts(t, body.List, 0, printWhere("pre"))
			}).output)
			assert.Regexp(t, generatedPosition, got["pre"])
			assert.Regexp(t, generatedPosition, got["gen"])
		})
	}
}

func TestMarkLineDirectives_CompilerDeclarationEndDirectiveAfterSkippedRestore(t *testing.T) {
	// The original function ends in generated code, and the restore of its
	// closing brace is skipped. Go reads the "/*line*/" comment after the
	// function. The next generated declaration needs a "<generated>"
	// directive.
	const src = preludeProgram + `//line /tmp/foo:7:50:1
func f() {
	fmt.Println("A", where())
} /*line /tmp/special.go:90*/

func main() {
	f()
	generated()
}
`
	got := positions(runInjected(t, src, func(root *dst.File) {
		f := findFunc(t, root, "f")
		require.Equal(t, dst.Decorations{"/*line /tmp/special.go:90*/"}, f.Decorations().End)
		f.Body.List = append(f.Body.List, snippet(t, printWhere("inner"))...)
		extra, err := ast.NewAstParser().ParseSource(
			"package main\n\nfunc generated() {\n\t" + printWhere("outer") + "\n}\n",
		)
		require.NoError(t, err)
		decls := append([]dst.Decl{}, root.Decls...)
		for i, decl := range decls {
			if decl == f {
				root.Decls = append(append(decls[:i+1:i+1], extra.Decls[0]), decls[i+1:]...)
			}
		}
	}).output)
	assert.Regexp(t, generatedPosition, got["inner"])
	assert.Regexp(t, generatedPosition, got["outer"])
}
