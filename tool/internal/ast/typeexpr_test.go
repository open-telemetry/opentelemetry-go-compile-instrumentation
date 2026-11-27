// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ast

import (
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// typeOf returns the type of the only parameter of `func(_ <src>)`, so that a
// variadic type can be written too.
func typeOf(t *testing.T, src string) dst.Expr {
	t.Helper()
	file, err := decorator.Parse("package p\nvar _ func(_ " + src + ")\n")
	require.NoError(t, err)
	decl, ok := file.Decls[0].(*dst.GenDecl)
	require.True(t, ok)
	spec, ok := decl.Specs[0].(*dst.ValueSpec)
	require.True(t, ok)
	fn, ok := spec.Type.(*dst.FuncType)
	require.True(t, ok)
	return fn.Params.List[0].Type
}

func TestTypeExprMatches(t *testing.T) {
	imports := map[string]string{
		"context": "context",
		"http":    "net/http",
		"sql":     "database/sql",
	}
	tests := []struct {
		filter string
		source string
		want   bool
	}{
		{"[]byte", "[]byte", true},
		{"[]byte", "[]string", false},
		{"[]byte", "[4]byte", false},
		{"[]byte", "byte", false},
		{"[]string", "...string", false},
		{"...string", "string", false},
		{"[]*net/http.Request", "[]*http.Request", true},
		{"[]*net/http.Request", "[]http.Request", false},
		{"[]net/http.Request", "[]http.Request", true},
		{"[][]byte", "[][]byte", true},
		{"[][]byte", "[]byte", false},
		{"*[]byte", "*[]byte", true},
		{"*[]byte", "[]byte", false},
		{"[4]byte", "[4]byte", true},
		{"[4]byte", "[5]byte", false},
		{"[4]byte", "[]byte", false},
		{"[N]byte", "[N]byte", true},
		{"[N]byte", "[M]byte", false},
		{"map[string]any", "map[string]any", true},
		{"map[string]any", "map[string]interface{}", true},
		{"map[string]interface{}", "map[string]interface{}", true},
		{"map[string]interface{}", "map[string]any", true},
		{"map[string]any", "map[string]string", false},
		{"map[string]any", "map[int]any", false},
		{"map[string][]byte", "map[string][]byte", true},
		{"map[string]map[string]int", "map[string]map[string]int", true},
		{"map[string]map[string]int", "map[string]map[string]string", false},
		{"map[context.Context]*database/sql.DB", "map[context.Context]*sql.DB", true},
		{"chan int", "chan int", true},
		{"chan int", "<-chan int", false},
		{"chan int", "chan<- int", false},
		{"<-chan int", "<-chan int", true},
		{"<-chan int", "chan int", false},
		{"chan<- int", "chan<- int", true},
		{"chan<- int", "<-chan int", false},
		{"chan struct{}", "chan struct{}", true},
		{"chan struct{}", "chan int", false},
		{"struct{}", "struct{}", true},
		{"struct{}", "struct{ x int }", false},
		{"chan  <-  error", "chan<- error", true},
		{"func()", "func()", true},
		{"func()", "func(int)", false},
		{"func()", "func() error", false},
		{"func(int)", "func(a int)", true},
		{"func(int, int)", "func(a, b int)", true},
		{"func(int, int)", "func(a int)", false},
		{"func(int) error", "func(int) error", true},
		{"func(int) error", "func(int) (error)", true},
		{"func(int) error", "func(string) error", false},
		{"func(int) (int, error)", "func(int) (int, error)", true},
		{"func(int) (int, error)", "func(int) (n int, err error)", true},
		{"func(int) (int, error)", "func(int) error", false},
		{"func(context.Context) error", "func(context.Context) error", true},
		{"func(context.Context) error", "func(http.Request) error", false},
		{"func(*net/http.Request)", "func(*http.Request)", true},
		{"func(...string)", "func(...string)", true},
		{"func(...string)", "func([]string)", false},
		{"func(func())", "func(func())", true},
		{"func(int,)", "func(int)", true},
		{"[]func(int) error", "[]func(int) error", true},
		{"map[string]func()", "map[string]func()", true},
		{"[]struct{}", "[]struct{}", true},
		{"[]byte", "[](byte)", true},
		{"[]database/sql.DB", "[]http.DB", false},
		{"[]http.Request", "[]http.Request", false},
	}
	for _, tt := range tests {
		t.Run(tt.filter+" vs "+tt.source, func(t *testing.T) {
			tn, err := parseTypeName(tt.filter)
			require.NoError(t, err)
			assert.Equal(t, tt.want, tn.matches(typeOf(t, tt.source), imports))
		})
	}
}

func TestParseTypeNameRejectsMalformedComposites(t *testing.T) {
	for _, filter := range []string{
		"[]",
		"[",
		"[4",
		"[4]",
		"[-1]int",
		"[a.b]int",
		"[]]int",
		"map[string",
		"map[string]",
		"map[]int",
		"map[string]int]",
		"map string",
		"chan",
		"<-",
		"<-int",
		"<-chan",
		"func",
		"func(",
		"func(int",
		"func(int,,int)",
		"func(a int)",
		"func(int) (",
		"func(int) (int",
		"func(int) error int",
		"struct",
		"struct{",
		"chan chan",
		"map",
		"struct{ x int }",
		"interface",
		"interface{ Close() error }",
		"...",
		"[]int extra",
		"[]Seq[T]",
		"*",
		"**",
		"[]*",
		"",
	} {
		t.Run(filter, func(t *testing.T) {
			_, err := parseTypeName(filter)
			assert.Error(t, err)
		})
	}
}

func TestParseTypeNameStillReadsPlainNames(t *testing.T) {
	tn, err := parseTypeName("*net/http.Request")
	require.NoError(t, err)
	assert.Nil(t, tn.composite)
	assert.True(t, tn.pointer)
	assert.Equal(t, "net/http", tn.importPath)
	assert.Equal(t, "Request", tn.name)
}

func TestParseTypeNameLimitsNesting(t *testing.T) {
	deep := strings.Repeat("[]", maxTypeExprDepth+1) + "int"
	_, err := parseTypeName(deep)
	require.Error(t, err)

	ok := strings.Repeat("[]", maxTypeExprDepth-1) + "int"
	_, err = parseTypeName(ok)
	require.NoError(t, err)
}

func TestParseInterfaceNameRejectsComposite(t *testing.T) {
	_, _, err := ParseInterfaceName("[]error")
	require.Error(t, err)
	_, _, err = ParseInterfaceName("map[string]any")
	require.Error(t, err)
}

func TestMatchesTypeNameComposite(t *testing.T) {
	matched, err := MatchesTypeName(typeOf(t, "[]byte"), "[]byte", nil)
	require.NoError(t, err)
	assert.True(t, matched)

	matched, err = MatchesTypeName(typeOf(t, "[]byte"), "[]string", nil)
	require.NoError(t, err)
	assert.False(t, matched)
}
