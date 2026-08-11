// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// countImportSpecs counts the import specs declared in the file. Import
// injection appends to root.Decls rather than root.Imports, so the decls are
// what a file written back out actually reflects.
func countImportSpecs(root *dst.File) int {
	count := 0
	for _, decl := range root.Decls {
		genDecl, ok := decl.(*dst.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		count += len(genDecl.Specs)
	}
	return count
}

// The directive sits on a statement inside a function body, so it annotates no
// top-level func and the rule matches nothing.
const directiveInsideBodySource = `package main

func main() {
	//otelc:span
	value := 1
	println(value)
}
`

const directiveOnFuncSource = `package main

//otelc:span
func foo() {
	println("hello")
}
`

func TestApplyDirectiveRule_NoMatchingFuncs(t *testing.T) {
	root, err := ast.NewAstParser().ParseSource(directiveInsideBodySource)
	require.NoError(t, err)

	r := &rule.InstDirectiveRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "span_directive",
			Imports: map[string]string{"fmt": "fmt"},
		},
		Directive: "otelc:span",
		Template:  `fmt.Println("span start: {{FuncName}}")`,
	}

	modified, err := newTestPhase().applyDirectiveRule(context.Background(), r, root)

	require.NoError(t, err)
	assert.False(t, modified, "a rule that instruments nothing must not request the globals file")
	assert.Zero(t, countImportSpecs(root), "imports must not be injected when no func is instrumented")
}

func TestApplyDirectiveRule_MatchingFunc(t *testing.T) {
	root, err := ast.NewAstParser().ParseSource(directiveOnFuncSource)
	require.NoError(t, err)

	r := &rule.InstDirectiveRule{
		InstBaseRule: rule.InstBaseRule{Name: "span_directive"},
		Directive:    "otelc:span",
		Template:     `println("span start: {{FuncName}}")`,
	}

	modified, err := newTestPhase().applyDirectiveRule(context.Background(), r, root)

	require.NoError(t, err)
	assert.True(t, modified)
	funcDecl, ok := root.Decls[0].(*dst.FuncDecl)
	require.True(t, ok, "expected *dst.FuncDecl, got %T", root.Decls[0])
	assert.Len(t, funcDecl.Body.List, 2, "rendered statement should be prepended to the body")
}

// The template failures below are rejected by InstDirectiveRule.validate before
// a rule reaches this point, so these cases build the rule struct directly.
// They pin the behaviour of applyDirectiveRule itself: a template that cannot
// be compiled, rendered, or parsed is reported, never silently skipped.
func TestApplyDirectiveRule_TemplateErrors(t *testing.T) {
	tests := []struct {
		name     string
		template string
		wantErr  string
	}{
		{
			name:     "unterminated tag",
			template: `println("{{FuncName")`,
			wantErr:  "end tag",
		},
		{
			name:     "unknown tag",
			template: `println("{{Bogus}}")`,
			wantErr:  "unknown template tag",
		},
		{
			name:     "renders to invalid Go",
			template: "if {",
			wantErr:  "parsing rendered template for func foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := ast.NewAstParser().ParseSource(directiveOnFuncSource)
			require.NoError(t, err)

			r := &rule.InstDirectiveRule{
				InstBaseRule: rule.InstBaseRule{Name: "span_directive"},
				Directive:    "otelc:span",
				Template:     tt.template,
			}

			modified, err := newTestPhase().applyDirectiveRule(context.Background(), r, root)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.False(t, modified)
		})
	}
}
