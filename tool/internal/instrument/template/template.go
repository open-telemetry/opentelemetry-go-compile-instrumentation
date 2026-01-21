// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package template

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"text/template"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// Template represents a code template that can be used to wrap or transform
// Go expressions. It uses Go's text/template package for template execution
// and supports placeholder substitution for AST nodes.
type Template struct {
	template *template.Template
	Source   string
}

// wrapper is a template that wraps user templates in a minimal function
// to allow them to be parsed as valid Go code.
//
//nolint:gochecknoglobals // Template constant
var wrapper = template.Must(template.New("wrapper").Parse(
	`package _
func _() {
	{{ . }}
}
`))

// NewTemplate creates a new Template from the provided template text.
// The template text should contain {{ . }} as a placeholder for the expression
// being wrapped.
//
// Example:
//
//	NewTemplate("wrapper({{ . }})")
func NewTemplate(text string) (*Template, error) {
	// Create a new template and parse the user's template text
	tmpl := template.New("code")
	tmpl, err := tmpl.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	return &Template{
		template: tmpl,
		Source:   text,
	}, nil
}

// CompileExpression executes the template with the given expression node as
// the placeholder value, parses the result, and returns the transformed expression.
//
// The process:
// 1. Execute the template with a fixed placeholder string (_.PLACEHOLDER_0)
// 2. Wrap the result in a minimal function and parse it
// 3. Extract the expression from the parsed function
// 4. Replace the placeholder with the actual AST node
func (t *Template) CompileExpression(node dst.Expr) (dst.Expr, error) {
	// Execute the user's template with a fixed placeholder string
	var userBuf bytes.Buffer
	if err := t.template.Execute(&userBuf, "_.PLACEHOLDER_0"); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	// Wrap the result in a minimal function so we can parse it
	var wrappedBuf bytes.Buffer
	if err := wrapper.Execute(&wrappedBuf, userBuf.String()); err != nil {
		return nil, fmt.Errorf("failed to wrap template result: %w", err)
	}

	// Parse the wrapped code
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", wrappedBuf.Bytes(), parser.ParseComments)
	if err != nil {
		// Format the error with the generated code for debugging
		formatted, _ := format.Source(wrappedBuf.Bytes())
		return nil, fmt.Errorf("failed to parse generated code: %w\nGenerated code:\n%s", err, formatted)
	}

	// Convert ast.File to dst.File
	dec := decorator.NewDecorator(fset)
	dstFile, err := dec.DecorateFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decorate AST: %w", err)
	}

	// Extract the expression from the function body
	if len(dstFile.Decls) == 0 {
		return nil, errors.New("no declarations found in generated code")
	}

	funcDecl, ok := dstFile.Decls[0].(*dst.FuncDecl)
	if !ok {
		return nil, fmt.Errorf("expected function declaration, got %T", dstFile.Decls[0])
	}

	if funcDecl.Body == nil || len(funcDecl.Body.List) == 0 {
		return nil, errors.New("function body is empty")
	}
	if len(funcDecl.Body.List) != 1 {
		return nil, fmt.Errorf("expected single expression statement, got %d statements", len(funcDecl.Body.List))
	}

	exprStmt, ok := funcDecl.Body.List[0].(*dst.ExprStmt)
	if !ok {
		return nil, fmt.Errorf("expected expression statement, got %T", funcDecl.Body.List[0])
	}

	// Replace placeholder with the actual node
	result, replaced := replacePlaceholder(exprStmt.X, node)
	if !replaced {
		return nil, errors.New("template output did not contain placeholder expression")
	}

	resultExpr, ok := result.(dst.Expr)
	if !ok {
		return nil, errors.New("placeholder replacement didn't produce an expression")
	}

	return resultExpr, nil
}
