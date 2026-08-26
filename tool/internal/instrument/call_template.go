// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"text/template"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"

	"go.opentelemetry.io/otelc/tool/ex"
	toolast "go.opentelemetry.io/otelc/tool/internal/ast"
)

// placeholderIdent is substituted for "{{ . }}" during template execution,
// then replaced with the actual AST node once the rendered text has been
// parsed. It must be a syntactically valid Go expression on its own.
const placeholderIdent = "_.PLACEHOLDER_0"

// callTemplate represents a code template that can be used to wrap or transform
// Go expressions. It uses text/template for template execution and supports
// placeholder substitution for AST nodes.
type callTemplate struct {
	template *template.Template
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
	tmpl, err := template.New("call").Parse(text)
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

type callTemplateData struct {
	enclosing *funcTemplateData

	// isCall and callArgs describe the wrapped expression when it is a
	// function call (e.g. wrap_call's matched call site)
	isCall   bool
	callArgs []dst.Expr
}

// String implements fmt.Stringer so "{{ . }}" renders as placeholderIdent.
func (*callTemplateData) String() string {
	return placeholderIdent
}

func noEnclosingFuncErr() error {
	return ex.Newf("no enclosing function is available at this position")
}

// FuncName returns the enclosing function's name. Template usage: {{.FuncName}}
func (d *callTemplateData) FuncName() (string, error) {
	if d.enclosing == nil {
		return "", noEnclosingFuncErr()
	}
	return d.enclosing.FuncName(), nil
}

// FuncArgument returns the identifier of the idx-th (0-indexed) parameter of
// the enclosing function, excluding the receiver. Template usage:
// {{.FuncArgument N}}
func (d *callTemplateData) FuncArgument(idx int) (string, error) {
	if d.enclosing == nil {
		return "", noEnclosingFuncErr()
	}
	return d.enclosing.FuncArgument(idx)
}

// FuncReturn returns the identifier of the idx-th (0-indexed) return value of
// the enclosing function. Template usage: {{.FuncReturn N}}
func (d *callTemplateData) FuncReturn(idx int) (string, error) {
	if d.enclosing == nil {
		return "", noEnclosingFuncErr()
	}
	return d.enclosing.FuncReturn(idx)
}

// FuncArgumentCount returns the number of parameters of the enclosing
// function, excluding the receiver. Template usage: {{.FuncArgumentCount}}
func (d *callTemplateData) FuncArgumentCount() (int, error) {
	if d.enclosing == nil {
		return 0, noEnclosingFuncErr()
	}
	return d.enclosing.FuncArgumentCount(), nil
}

// FuncReturnCount returns the number of return values of the enclosing
// function. Template usage: {{.FuncReturnCount}}
func (d *callTemplateData) FuncReturnCount() (int, error) {
	if d.enclosing == nil {
		return 0, noEnclosingFuncErr()
	}
	return d.enclosing.FuncReturnCount(), nil
}

// FuncArgumentOfType returns the identifier of the first parameter of the
// enclosing function (excluding the receiver) whose type matches typeStr
// or "" if none match. Template usage: {{.FuncArgumentOfType "context.Context"}}
func (d *callTemplateData) FuncArgumentOfType(typeStr string) (string, error) {
	if d.enclosing == nil {
		return "", noEnclosingFuncErr()
	}
	return d.enclosing.FuncArgumentOfType(typeStr)
}

func notACallErr() error {
	return ex.Newf("requires the wrapped expression to be a function call")
}

// CallArgumentCount returns the number of arguments in the wrapped call
// expression. Only available when the wrapped expression is itself
// a function call. Template usage: {{.CallArgumentCount}}
func (d *callTemplateData) CallArgumentCount() (int, error) {
	if !d.isCall {
		return 0, notACallErr()
	}
	return len(d.callArgs), nil
}

// CallArgument returns the source text of the idx-th (0-indexed) argument of
// the wrapped call expression. Only available when the wrapped expression is
// itself a function call. Template usage: {{.CallArgument N}}
func (d *callTemplateData) CallArgument(idx int) (string, error) {
	if !d.isCall {
		return "", notACallErr()
	}
	if idx < 0 || idx >= len(d.callArgs) {
		return "", ex.Newf("CallArgument index %d out of range [0, %d)", idx, len(d.callArgs))
	}
	return toolast.RenderExpr(d.callArgs[idx])
}

// compileExpression executes the template with the given expression node as
// the placeholder value, parses the result, and returns the transformed expression.
// enclosing is the function declaration that contains node, or
// nil if node sits outside any function body (e.g. a package-level variable
// initializer); when non-nil, it makes the shared function template
// variables (FuncName, FuncArgument N, FuncReturn N, ...) available in the
// template alongside {{ . }}.
//
// The process:
// 1. Execute the template with a fixed placeholder string (_.PLACEHOLDER_0)
// 2. Wrap the result in a minimal function and parse it
// 3. Extract the expression from the parsed function
// 4. Replace the placeholder with the actual AST node
func (t *callTemplate) compileExpression(node dst.Expr, enclosing *dst.FuncDecl) (dst.Expr, error) {
	data := &callTemplateData{}
	if enclosing != nil {
		data.enclosing = newFuncTemplateData(enclosing, nil, nil, "")
	}
	if call, ok := unwrap(node).(*dst.CallExpr); ok {
		data.isCall = true
		data.callArgs = call.Args
	}

	var sb strings.Builder
	if err := t.template.Execute(&sb, data); err != nil {
		return nil, ex.Wrapf(err, "failed to execute template")
	}
	userResult := sb.String()

	placeholderRendered := strings.Contains(userResult, placeholderIdent)

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

	result, replaced := replacePlaceholder(exprStmt.X, node)
	if placeholderRendered && !replaced {
		return nil, ex.New(
			"template output did not contain expected placeholder expression for {{ . }}",
		)
	}

	resultExpr, ok := result.(dst.Expr)
	if !ok {
		return nil, ex.New("placeholder replacement didn't produce an expression")
	}

	return resultExpr, nil
}

// unwrap strips any enclosing parentheses from expr, e.g. (foo()) -> foo().
func unwrap(expr dst.Expr) dst.Expr {
	for {
		paren, ok := expr.(*dst.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

// parseGoExpression parses a Go expression string into a dst.Expr.
func parseGoExpression(expr string) (dst.Expr, error) {
	funcDecl, err := parseSnippetFuncDecl("package _\nfunc _() {\n\t"+expr+"\n}\n", expr)
	if err != nil {
		return nil, err
	}
	exprStmt, ok := funcDecl.Body.List[0].(*dst.ExprStmt)
	if !ok {
		return nil, ex.Newf(
			"expression %q did not parse as an expression statement (got %T)",
			expr, funcDecl.Body.List[0])
	}
	return exprStmt.X, nil
}

// parseGoTypeExpression parses a Go type string (e.g. "grpc.DialOption") into a dst.Expr.
func parseGoTypeExpression(typeStr string) (dst.Expr, error) {
	funcDecl, err := parseSnippetFuncDecl("package _\nfunc _() {\n\tvar _ "+typeStr+"\n}\n", typeStr)
	if err != nil {
		return nil, err
	}
	declStmt, ok := funcDecl.Body.List[0].(*dst.DeclStmt)
	if !ok {
		return nil, ex.Newf(
			"type %q did not parse as a declaration statement (got %T)",
			typeStr, funcDecl.Body.List[0])
	}
	genDecl, ok := declStmt.Decl.(*dst.GenDecl)
	if !ok || len(genDecl.Specs) == 0 {
		return nil, ex.Newf("unexpected declaration shape for type %q", typeStr)
	}
	valueSpec, ok := genDecl.Specs[0].(*dst.ValueSpec)
	if !ok || valueSpec.Type == nil {
		return nil, ex.Newf("unexpected spec shape for type %q", typeStr)
	}
	return valueSpec.Type, nil
}

// parseSnippetFuncDecl parses a minimal Go source snippet of the form
// "package _\nfunc _() { <body> }\n" and returns the function declaration.
// label is used in error messages to identify the original snippet.
func parseSnippetFuncDecl(src, label string) (*dst.FuncDecl, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", []byte(src), parser.ParseComments)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to parse %q", label)
	}
	dec := decorator.NewDecorator(fset)
	dstFile, err := dec.DecorateFile(file)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to decorate AST for %q", label)
	}
	if len(dstFile.Decls) == 0 {
		return nil, ex.Newf("no declarations found for %q", label)
	}
	funcDecl, ok := dstFile.Decls[0].(*dst.FuncDecl)
	if !ok || funcDecl.Body == nil || len(funcDecl.Body.List) == 0 {
		return nil, ex.Newf("unexpected AST shape for %q", label)
	}
	return funcDecl, nil
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
