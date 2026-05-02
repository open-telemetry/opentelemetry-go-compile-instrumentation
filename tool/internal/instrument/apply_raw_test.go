// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/require"
)

func TestInsertRawAtPos(t *testing.T) {
	tests := []struct {
		name           string
		src            string
		pattern        string
		expectInserted bool
		expected       string
	}{
		{
			name: "basic insert",
			src: `package main

func a() {
	println("x")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	print("Hello, ")
	print("World!")
	println("x")
}
`,
		},
		{
			name: "only first match",
			src: `package main

func a() {
	println("x")
	println("x")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	print("Hello, ")
	print("World!")
	println("x")
	println("x")
}
`,
		},
		{
			name: "nested block",
			src: `package main

func a() {
	if true {
		println("x")
	}
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	if true {
		print("Hello, ")
		print("World!")
		println("x")
	}
}
`,
		},
		{
			name: "first match in nested block",
			src: `package main

func a() {
	if true {
		println("x")
	}
	println("x")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	if true {
		print("Hello, ")
		print("World!")
		println("x")
	}
	println("x")
}
`,
		},
		{
			name: "match block statement header",
			src: `package main

func a() {
	go func() {
		println("x")
	}()
}
`,
			pattern:        `^go func\(\) \{`,
			expectInserted: true,
			expected: `package main

func a() {
	print("Hello, ")
	print("World!")
	go func() {
		println("x")
	}()
}
`,
		},
		{
			name: "multiple statements in a single line",
			src: `package main

func a() {
	println("y"); println("x")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: true,
			expected: `package main

func a() {
	println("y")
	print("Hello, ")
	print("World!")
	println("x")
}
`,
		},
		{
			name: "empty block",
			src: `package main

func a() {}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: false,
			expected: `package main

func a() {}
`,
		},
		{
			name: "no matches",
			src: `package main

func a() {
	println("y")
}
`,
			pattern:        `^println\("x"\)$`,
			expectInserted: false,
			expected: `package main

func a() {
	println("y")
}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, parseErr := parser.ParseFile(fset, "", tc.src, parser.ParseComments)
			require.NoError(t, parseErr)

			dec := decorator.NewDecorator(fset)
			dstFile, decorateErr := dec.DecorateFile(f)
			require.NoError(t, decorateErr)

			restorer := decorator.NewRestorer()
			_, restoreErr := restorer.RestoreFile(dstFile)
			require.NoError(t, restoreErr)

			var fn *dst.FuncDecl
			for _, decl := range dstFile.Decls {
				if f, ok := decl.(*dst.FuncDecl); ok && f.Name.Name == "a" {
					fn = f
					break
				}
			}
			require.NotNil(t, fn, "function a not found")

			stmts := []dst.Stmt{
				&dst.ExprStmt{
					X: &dst.CallExpr{
						Fun:  dst.NewIdent("print"),
						Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: `"Hello, "`}},
					},
				},
				&dst.ExprStmt{
					X: &dst.CallExpr{
						Fun:  dst.NewIdent("print"),
						Args: []dst.Expr{&dst.BasicLit{Kind: token.STRING, Value: `"World!"`}},
					},
				},
			}

			inserted := insertRawAtPos(fn, restorer, regexp.MustCompile(tc.pattern), stmts)
			require.Equal(t, tc.expectInserted, inserted)

			var modifiedSrc strings.Builder
			decorator.Fprint(&modifiedSrc, dstFile)

			require.Equal(t, tc.expected, modifiedSrc.String())
		})
	}
}
