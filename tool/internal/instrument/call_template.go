// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"
	"github.com/valyala/fasttemplate"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	toolast "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
)

// callTemplate represents a code template that can be used to wrap or transform
// Go expressions. It uses fasttemplate for template execution
// and supports placeholder substitution for AST nodes.
type callTemplate struct {
	template *fasttemplate.Template
	source   string
}

// newCallTemplate creates a new callTemplate from the provided template text.
// The template text should contain {{ . }} as a placeholder for the expression
// being wrapped.
//
// Example:
//
//	newCallTemplate("wrapper({{ . }})")
func newCallTemplate(text string) (*callTemplate, error) {
	tmpl, err := fasttemplate.NewTemplate(text, "{{", "}}")
	if err != nil {
		return nil, ex.Newf("failed to parse template %s", text)
	}

	return &callTemplate{
		template: tmpl,
		source:   text,
	}, nil
}

// String returns the original template source text.
func (t *callTemplate) String() string {
	return t.source
}

// compileExpression executes the template with the given expression node as
// the placeholder value, parses the result, and returns the transformed expression.
//
// The process:
// 1. Execute the template with a fixed placeholder string (_.PLACEHOLDER_0)
// 2. Wrap the result in a minimal function and parse it
// 3. Extract the expression from the parsed function
// 4. Replace the placeholder with the actual AST node
func (t *callTemplate) compileExpression(node dst.Expr) (dst.Expr, error) {
	// Execute the user's template with a fixed placeholder string.
	// The TagFunc handles {{ . }}, {{.}}, and {{- . -}} variants by
	// normalizing the tag content before matching.
	userResult, err := t.template.ExecuteFuncStringWithErr(func(w io.Writer, tag string) (int, error) {
		// Trim spaces and optional trim markers (e.g. {{- . -}})
		cleaned := strings.TrimSpace(tag)
		cleaned = strings.Trim(cleaned, "-")
		cleaned = strings.TrimSpace(cleaned)
		if cleaned == "." {
			return io.WriteString(w, "_.PLACEHOLDER_0")
		}
		return 0, ex.Newf("unknown template tag %q; only {{ . }} is supported", tag)
	})
	if err != nil {
		return nil, ex.Newf("failed to execute template")
	}

	// Wrap the result in a minimal function so we can parse it as Go code.
	wrapped := "package _\nfunc _() {\n\t" + userResult + "\n}\n"

	// Parse the wrapped code
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", []byte(wrapped), parser.ParseComments)
	if err != nil {
		// Format the error with the generated code for debugging
		formatted, _ := format.Source([]byte(wrapped))
		return nil, ex.Wrapf(err, "failed to parse generated code\nGenerated code:\n%s", formatted)
	}

	// Convert ast.File to dst.File
	dec := decorator.NewDecorator(fset)
	dstFile, err := dec.DecorateFile(file)
	if err != nil {
		return nil, ex.Newf("failed to decorate AST")
	}

	// Extract the expression from the function body
	if len(dstFile.Decls) == 0 {
		return nil, ex.New("no declarations found in generated code")
	}

	funcDecl, ok := dstFile.Decls[0].(*dst.FuncDecl)
	if !ok {
		return nil, ex.Newf("expected function declaration, got %T", dstFile.Decls[0])
	}

	if funcDecl.Body == nil || len(funcDecl.Body.List) == 0 {
		return nil, ex.New("function body is empty")
	}
	if len(funcDecl.Body.List) != 1 {
		return nil, ex.Newf("expected single expression statement, got %d statements", len(funcDecl.Body.List))
	}

	exprStmt, ok := funcDecl.Body.List[0].(*dst.ExprStmt)
	if !ok {
		return nil, ex.Newf("expected expression statement, got %T", funcDecl.Body.List[0])
	}

	// Replace placeholder with the actual node
	result, replaced := replacePlaceholder(exprStmt.X, node)
	if !replaced {
		return nil, ex.New("template output did not contain placeholder expression")
	}

	resultExpr, ok := result.(dst.Expr)
	if !ok {
		return nil, ex.New("placeholder replacement didn't produce an expression")
	}

	return resultExpr, nil
}

// replacePlaceholder replaces all occurrences of _.PLACEHOLDER_0 in the AST
// with the given node. This is used to inject the original call expression
// into the template-generated code.
func replacePlaceholder(node, replacement dst.Node) (dst.Node, bool) {
	replaced := false
	result := dstutil.Apply(
		node,
		func(cursor *dstutil.Cursor) bool {
			selectorExpr, ok := cursor.Node().(*dst.SelectorExpr)
			if !ok {
				return true
			}

			// Check if this is _.PLACEHOLDER_0
			ident, ok := selectorExpr.X.(*dst.Ident)
			if !ok || ident.Name != toolast.IdentIgnore {
				return true
			}

			if selectorExpr.Sel.Name == "PLACEHOLDER_0" {
				cursor.Replace(replacement)
				replaced = true
				return false
			}

			return true
		},
		nil,
	)
	return result, replaced
}
