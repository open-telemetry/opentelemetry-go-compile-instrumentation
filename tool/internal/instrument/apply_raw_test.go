// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/require"
)

func TestInsertRawAtPattern(t *testing.T) {
	ctx := util.ContextWithLogger(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	tests := []struct {
		name           string
		src            string
		pattern        string
		placement      string
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
			name: "place after the matched statement",
			src: `package main

func a() {
	println("y")
	println("x")
}
`,
			pattern:        `^println\("y"\)$`,
			placement:      "after",
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, parseErr := parser.ParseFile(fset, "", tt.src, parser.ParseComments)
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

			pos := insertPos{
				pattern:   regexp.MustCompile(tt.pattern),
				placement: tt.placement,
			}
			inserted := insertRawAtPattern(ctx, fn, restorer, pos, stmts)
			require.Equal(t, tt.expectInserted, inserted)

			var modifiedSrc strings.Builder
			decorator.Fprint(&modifiedSrc, dstFile)

			require.Equal(t, tt.expected, modifiedSrc.String())
		})
	}
}
