// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package instrument

import (
	"bytes"
	"context"
	"fmt"
	"go/parser"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
)

func TestGeneratedFileWindows(t *testing.T) {
	// The name is at the root of the volume of the file, so it is absolute,
	// and Go does not join it with the directory of the file.
	tests := map[string]string{
		`C:\work\dir\main.go`:        `C:\<generated>`,
		`D:\main.go`:                 `D:\<generated>`,
		`\\server\share\dir\main.go`: `\\server\share\<generated>`,
		`\\?\C:\work\dir\main.go`:    `\\?\C:\<generated>`,
	}
	for file, want := range tests {
		got := generatedFile(file)
		assert.Equal(t, want, got, file)
		assert.True(t, filepath.IsAbs(got), got)
	}

	// For an extended UNC name, the volume that filepath.VolumeName gives
	// changes with the Go version: Go 1.26 gives only `\\?\UNC`, and Go 1.27
	// also gives the server and the share. The name is at the root of this
	// volume, and it is absolute in all versions.
	file := `\\?\UNC\server\share\x\main.go`
	got := generatedFile(file)
	assert.Equal(t, filepath.VolumeName(file)+`\<generated>`, got, file)
	assert.Contains(t, []string{`\\?\UNC\<generated>`, `\\?\UNC\server\share\<generated>`}, got, file)
	assert.True(t, filepath.IsAbs(got), got)
}

func TestGeneratedFileWindowsRuntimeCaller(t *testing.T) {
	// The Go compiler on Windows reports code after the directive in the
	// generated file, with no directory before it.
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	name := generatedFile(file)
	src := "package main\n\nimport (\n\t\"fmt\"\n\t\"runtime\"\n)\n\nfunc main() {\n" +
		"\t/*line " + name + ":1*/ _, f, l, _ := runtime.Caller(0)\n" +
		"\tfmt.Printf(\"%s:%d\\n\", f, l)\n}\n"
	require.NoError(t, os.WriteFile(file, []byte(src), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o600))
	cmd := exec.Command("go", "run", "main.go")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	got := strings.TrimSpace(string(out))
	// The runtime can use "/" as separator. Compare with one separator.
	assert.Equal(t, filepath.ToSlash(name)+":1", filepath.ToSlash(got))
}

func TestAbsoluteWindows(t *testing.T) {
	p := &lineDirectivePass{dir: `C:\work\dir`}
	tests := map[string]string{
		// An absolute name does not change.
		`C:\alt\main.go`:          `C:\alt\main.go`,
		`D:\main.go`:              `D:\main.go`,
		`\\server\share\dir\a.go`: `\\server\share\dir\a.go`,
		`\\?\C:\alt\main.go`:      `\\?\C:\alt\main.go`,
		// A relative name is joined with the directory of the file.
		`main.go`:     `C:\work\dir\main.go`,
		`sub\main.go`: `C:\work\dir\sub\main.go`,
		`..\main.go`:  `C:\work\main.go`,
		// A name with no volume is not absolute on Windows.
		`\rooted\main.go`: `C:\work\dir\rooted\main.go`,
		// An empty name stays empty.
		``: ``,
	}
	for file, want := range tests {
		assert.Equal(t, want, p.absolute(file), file)
	}
}

func TestDirectiveWindows(t *testing.T) {
	tests := []struct {
		file string
		line int
		want string
		ok   bool
	}{
		{`C:\dir\main.go`, 5, `C:\dir\main.go:5`, true},
		{`D:\main.go`, 1, `D:\main.go:1`, true},
		{`\\server\share\dir\main.go`, 12, `\\server\share\dir\main.go:12`, true},
		{`\\?\C:\dir\main.go`, 3, `\\?\C:\dir\main.go:3`, true},
		// The ":" of the drive is not read as a line.
		{`C:\main.go`, 7, `C:\main.go:7`, true},
		// Go reads the end of the name as the line, so Go cannot read it.
		{`C:\dir\main.go:123`, 5, "", false},
		{`\\server\share\main.go:9`, 5, "", false},
		// A line less than 1 is not valid.
		{`C:\dir\main.go`, 0, "", false},
		{`C:\dir\main.go`, -1, "", false},
		// A name that is not safe in a comment is not valid.
		{"C:\\dir\\a*/b.go", 5, "", false},
		{"C:\\dir\\a\nb.go", 5, "", false},
	}
	for _, tt := range tests {
		got, ok := directive(tt.file, tt.line)
		assert.Equal(t, tt.ok, ok, "%s:%d", tt.file, tt.line)
		assert.Equal(t, tt.want, got, "%s:%d", tt.file, tt.line)
	}
}

func TestEndsWithNumberWindows(t *testing.T) {
	tests := map[string]bool{
		`C:\dir\main.go`: false,
		`C:\main.go`:     false,
		// The ":" of the drive has no digits after it.
		`C:`:                       false,
		`C:\`:                      false,
		`C:\dir\file1`:             false,
		`C:\dir\a:b1`:              false,
		`C:\dir:`:                  false,
		`C:\dir\main.go:123`:       true,
		`C:\dir\main.go:1`:         true,
		`\\server\share\main.go:9`: true,
		`\\?\C:\dir\main.go:42`:    true,
		``:                         false,
	}
	for file, want := range tests {
		assert.Equal(t, want, endsWithNumber(file), file)
	}
}

// windowsProgram holds the original program of the tests below. It has no
// directive. The tests add code to it and run the result.
const windowsProgram = `package main

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
	fmt.Println("C", where())
	helper()
}

func helper() {
	fmt.Println("H", where())
}
`

// windowsGoRun runs "go run" for file in dir, and returns the lines that the
// program printed. Each line is an identifier, a space, and a position. The
// positions use "/" as separator. A position can have spaces (for example in
// "C:\Users\Jane Doe"), so only the first space separates the two parts.
func windowsGoRun(t *testing.T, dir, file string) map[string]string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "go", "run", file)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GO111MODULE=on")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	require.NoError(t, cmd.Run(), buf.String())
	positions := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		id, pos, ok := strings.Cut(strings.TrimRight(line, "\r"), " ")
		require.True(t, ok, line)
		positions[id] = filepath.ToSlash(pos)
	}
	return positions
}

// windowsLineOf returns the line of the first line in windowsProgram that has
// marker in it.
func windowsLineOf(t *testing.T, marker string) int {
	t.Helper()
	for i, l := range strings.Split(windowsProgram, "\n") {
		if strings.Contains(l, marker) {
			return i + 1
		}
	}
	require.Failf(t, "marker not found", "marker: %s", marker)
	return 0
}

// windowsInstrument writes windowsProgram to origFile, parses it, calls edit,
// and runs the line directive pass. It writes the result to inst.go in instDir,
// with a module. instDir must not be the directory of origFile, so that the
// path of the original file is not the path of the file that Go compiles.
func windowsInstrument(t *testing.T, origFile, instDir string, edit func(root *dst.File)) {
	t.Helper()
	require.NoError(t, os.WriteFile(origFile, []byte(windowsProgram), 0o600))
	p := ast.NewAstParser()
	root, err := p.Parse(origFile, parser.ParseComments)
	require.NoError(t, err)
	edit(root)
	markLineDirectives(p, root, origFile)

	require.NoError(
		t,
		os.WriteFile(filepath.Join(instDir, "go.mod"), []byte("module example.com/m\n\ngo 1.21\n"), 0o600),
	)
	require.NoError(t, ast.WriteFile(filepath.Join(instDir, "inst.go"), root))
}

func TestMarkLineDirectivesWindows_RestoreAfterRun(t *testing.T) {
	// Two generated statements are in the body of main, between A and B. The
	// first original statement after them must report the original file, with
	// the original line. The directory of the original file has a space, as
	// in "C:\Users\Jane Doe".
	origDir := filepath.Join(t.TempDir(), "with space")
	require.NoError(t, os.Mkdir(origDir, 0o700))
	origFile := filepath.Join(origDir, "orig.go")
	instDir := t.TempDir()
	windowsInstrument(t, origFile, instDir, func(root *dst.File) {
		var body *dst.BlockStmt
		for _, decl := range root.Decls {
			if fn, ok := decl.(*dst.FuncDecl); ok && fn.Name.Name == "main" {
				body = fn.Body
			}
		}
		require.NotNil(t, body)
		gen, err := ast.NewAstParser().ParseSnippet(
			`fmt.Println("G1", where())` + "\n" + `fmt.Println("G2", where())`)
		require.NoError(t, err)
		list := append([]dst.Stmt{}, body.List[:1]...)
		list = append(list, gen...)
		body.List = append(list, body.List[1:]...)
	})

	got := windowsGoRun(t, instDir, "inst.go")
	orig := filepath.ToSlash(origFile)
	want := func(marker string) string {
		return fmt.Sprintf("%s:%d", orig, windowsLineOf(t, marker))
	}
	// The code of the generated file is in the generated name.
	generated := filepath.ToSlash(generatedFile(origFile)) + ":"
	assert.True(t, strings.HasPrefix(got["G1"], generated), got["G1"])
	assert.True(t, strings.HasPrefix(got["G2"], generated), got["G2"])
	// The original statements keep the original file and line.
	assert.Equal(t, want(`"A"`), got["A"])
	assert.Equal(t, want(`"B"`), got["B"])
	assert.Equal(t, want(`"C"`), got["C"])
	assert.Equal(t, want(`"H"`), got["H"])
}

func TestMarkLineDirectivesWindows_TopLevelRestore(t *testing.T) {
	// A generated function is before helper. helper must report the original
	// file and line, from a top-level "//line C:\...:N" directive. The
	// generated function is called by a generated statement in main.
	origFile := filepath.Join(t.TempDir(), "orig.go")
	instDir := t.TempDir()
	windowsInstrument(t, origFile, instDir, func(root *dst.File) {
		gen, err := ast.NewAstParser().ParseSource(
			"package main\n\nimport \"fmt\"\n\nfunc generated() {\n\tfmt.Println(\"GT\", where())\n}\n")
		require.NoError(t, err)
		var genFn dst.Decl
		for _, decl := range gen.Decls {
			if _, ok := decl.(*dst.FuncDecl); ok {
				genFn = decl
			}
		}
		require.NotNil(t, genFn)
		var decls []dst.Decl
		for _, decl := range root.Decls {
			if fn, ok := decl.(*dst.FuncDecl); ok {
				switch fn.Name.Name {
				case "helper":
					decls = append(decls, genFn)
				case "main":
					call, callErr := ast.NewAstParser().ParseSnippet("generated()")
					require.NoError(t, callErr)
					fn.Body.List = append(fn.Body.List, call...)
				}
			}
			decls = append(decls, decl)
		}
		root.Decls = decls
	})

	got := windowsGoRun(t, instDir, "inst.go")
	orig := filepath.ToSlash(origFile)
	generated := filepath.ToSlash(generatedFile(origFile)) + ":"
	assert.True(t, strings.HasPrefix(got["GT"], generated), got["GT"])
	assert.Equal(t, fmt.Sprintf("%s:%d", orig, windowsLineOf(t, `"H"`)), got["H"])
	assert.Equal(t, fmt.Sprintf("%s:%d", orig, windowsLineOf(t, `"C"`)), got["C"])
}
