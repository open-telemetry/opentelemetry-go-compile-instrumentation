// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"strconv"
	"strings"
	"text/template"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"

	"go.opentelemetry.io/otelc/tool/ex"
	toolast "go.opentelemetry.io/otelc/tool/internal/ast"
)

// placeholderIdent is substituted for "{{ . }}" during template execution,
// then replaced with the actual AST node once the rendered text has been
// parsed. It must be a syntactically valid Go expression on its own.
const placeholderIdent = "_.PLACEHOLDER_0"

// callArgPlaceholderSel is the selector part of the placeholder substituted
// for "{{ .CallArgument N }}" during template execution (full form:
// _.CALLARG_<idx>). Like placeholderIdent, it is replaced with the actual
// AST node once the rendered text has been parsed and the rule's qualifiers
// rewritten, so the argument's own code is never rewritten.
const callArgPlaceholderSel = "CALLARG_"

// callArgPlaceholderIdent returns the placeholder for the idx-th wrapped
// call argument. It must be a syntactically valid Go expression on its own.
func callArgPlaceholderIdent(idx int) string {
	return toolast.IdentIgnore + "." + callArgPlaceholderSel + strconv.Itoa(idx)
}

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

// Receiver returns the identifier of the enclosing method's receiver, or an
// error if there is no enclosing function or the enclosing function has no
// receiver. Template usage: {{.Receiver}}
func (d *callTemplateData) Receiver() (string, error) {
	if d.enclosing == nil {
		return "", noEnclosingFuncErr()
	}
	return d.enclosing.Receiver()
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

// FuncReturnOfType returns the identifier of the first return value of the
// enclosing function whose type matches typeStr, or "" if none match.
// Template usage: {{.FuncReturnOfType "error"}}
func (d *callTemplateData) FuncReturnOfType(typeStr string) (string, error) {
	if d.enclosing == nil {
		return "", noEnclosingFuncErr()
	}
	return d.enclosing.FuncReturnOfType(typeStr)
}

// renderingCallTemplateData wraps callTemplateData for use as text/template
// render data. Use callTemplateData directly when the real name is needed
// instead of the marked one, such as in tests.
type renderingCallTemplateData struct {
	*callTemplateData

	// renderedCallArgs records each index rendered by {{ .CallArgument N }}
	// so compileExpression can verify every placeholder was swapped for
	// the argument's real AST.
	renderedCallArgs map[int]bool
}

func (d renderingCallTemplateData) FuncName() (string, error) {
	name, err := d.callTemplateData.FuncName()
	return markDynamicIdent(name), err
}

func (d renderingCallTemplateData) FuncArgument(idx int) (string, error) {
	name, err := d.callTemplateData.FuncArgument(idx)
	return markDynamicIdent(name), err
}

func (d renderingCallTemplateData) FuncReturn(idx int) (string, error) {
	name, err := d.callTemplateData.FuncReturn(idx)
	return markDynamicIdent(name), err
}

func (d renderingCallTemplateData) Receiver() (string, error) {
	name, err := d.callTemplateData.Receiver()
	return markDynamicIdent(name), err
}

func (d renderingCallTemplateData) FuncArgumentOfType(typeStr string) (string, error) {
	name, err := d.callTemplateData.FuncArgumentOfType(typeStr)
	return markDynamicIdent(name), err
}

func (d renderingCallTemplateData) FuncReturnOfType(typeStr string) (string, error) {
	name, err := d.callTemplateData.FuncReturnOfType(typeStr)
	return markDynamicIdent(name), err
}

// CallArgument renders the idx-th (0-indexed) argument of the wrapped call
// expression as a fixed placeholder (see callArgPlaceholderIdent) instead of
// the argument's source text, so replaceQualifierAliases cannot rewrite the
// argument's own code. compileExpression swaps the placeholder for a copy of
// the argument AST after the rewrite. Validation and error messages match
// callTemplateData.CallArgument. Template usage: {{.CallArgument N}}
func (d renderingCallTemplateData) CallArgument(idx int) (string, error) {
	if _, err := d.callTemplateData.CallArgument(idx); err != nil {
		return "", err
	}
	d.renderedCallArgs[idx] = true
	return callArgPlaceholderIdent(idx), nil
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
//
// Template rendering goes through renderingCallTemplateData.CallArgument,
// which returns a placeholder instead; see that method for why.
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
// imports is the target file's import alias map (see ast.ImportAliasMap);
// FuncArgumentOfType / FuncReturnOfType need it to resolve aliased imports
// and packages that share a default name. Pass nil when no import context
// is available.
// aliasOverrides maps each rule alias to the file's alias.
//
// The process:
//  1. Execute the template with fixed placeholder strings (_.PLACEHOLDER_0 for
//     "{{ . }}", _.CALLARG_<N> for "{{ .CallArgument N }}")
//  2. Parse the result as a Go statement snippet
//  3. Rewrite the rule's own qualifiers in that parsed result
//  4. Replace the placeholders with the actual AST nodes, which keeps the
//     target's own code untouched by the rewrite in step 3
func (t *callTemplate) compileExpression(
	node dst.Expr, enclosing *dst.FuncDecl, imports, aliasOverrides map[string]string,
) (dst.Expr, error) {
	data := &callTemplateData{}
	if enclosing != nil {
		data.enclosing = newFuncTemplateData(enclosing, nil, imports, "")
	}
	if call, ok := unwrap(node).(*dst.CallExpr); ok {
		data.isCall = true
		data.callArgs = call.Args
	}

	renderData := renderingCallTemplateData{
		callTemplateData: data,
		renderedCallArgs: make(map[int]bool),
	}
	var sb strings.Builder
	if err := t.template.Execute(&sb, renderData); err != nil {
		return nil, ex.Wrapf(err, "failed to execute template")
	}
	userResult := sb.String()

	placeholderRendered := strings.Contains(userResult, placeholderIdent)

	stmts, err := toolast.NewAstParser().ParseSnippet(userResult)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to parse generated code\nGenerated code:\n%s", userResult)
	}
	if len(stmts) != 1 {
		return nil, ex.Newf("expected single expression statement, got %d statements", len(stmts))
	}

	exprStmt, ok := stmts[0].(*dst.ExprStmt)
	if !ok {
		return nil, ex.Newf("expected expression statement, got %T", stmts[0])
	}

	replaceQualifierAliases(exprStmt.X, aliasOverrides)
	stripDynamicIdents(exprStmt.X)

	// Swap the call-argument placeholders before the {{ . }} placeholder so
	// both run after the qualifier rewrite; the swapped-in argument ASTs then
	// keep the target's original argument code untouched.
	swappedArgs, swappedIdxs := replaceCallArgPlaceholders(exprStmt.X, data.callArgs)
	for idx := range renderData.renderedCallArgs {
		if !swappedIdxs[idx] {
			return nil, ex.Newf(
				"template output did not contain expected placeholder expression for {{ .CallArgument %d }}",
				idx,
			)
		}
	}
	exprStmt.X = swappedArgs

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
	stmts, err := toolast.NewAstParser().ParseSnippet(expr)
	if err != nil {
		return nil, err
	}
	if len(stmts) != 1 {
		return nil, ex.Newf("expression %q did not parse as a single statement (got %d)", expr, len(stmts))
	}
	exprStmt, ok := stmts[0].(*dst.ExprStmt)
	if !ok {
		return nil, ex.Newf(
			"expression %q did not parse as an expression statement (got %T)",
			expr, stmts[0])
	}
	return exprStmt.X, nil
}

// parseGoTypeExpression parses a Go type string (e.g. "grpc.DialOption") into a dst.Expr.
func parseGoTypeExpression(typeStr string) (dst.Expr, error) {
	stmts, err := toolast.NewAstParser().ParseSnippet("var _ " + typeStr)
	if err != nil {
		return nil, err
	}
	if len(stmts) != 1 {
		return nil, ex.Newf("type %q did not parse as a single statement (got %d)", typeStr, len(stmts))
	}
	declStmt, ok := stmts[0].(*dst.DeclStmt)
	if !ok {
		return nil, ex.Newf(
			"type %q did not parse as a declaration statement (got %T)",
			typeStr, stmts[0])
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

// replaceCallArgPlaceholders replaces every _.CALLARG_<idx> occurrence in
// node with its own dst.Clone copy of callArgs[idx], and returns the
// resulting expression plus the indices it swapped. This is used to inject
// the wrapped call's arguments into the template-generated code after the
// alias rewrite, so the arguments' original code is never rewritten.
func replaceCallArgPlaceholders(node dst.Expr, callArgs []dst.Expr) (dst.Expr, map[int]bool) {
	swappedIdxs := make(map[int]bool)
	result := dstutil.Apply(
		node,
		func(cursor *dstutil.Cursor) bool {
			selectorExpr, ok := cursor.Node().(*dst.SelectorExpr)
			if !ok {
				return true
			}

			// Check if this is _.CALLARG_<idx>
			ident, ok := selectorExpr.X.(*dst.Ident)
			if !ok || ident.Name != toolast.IdentIgnore {
				return true
			}

			idx, err := strconv.Atoi(strings.TrimPrefix(selectorExpr.Sel.Name, callArgPlaceholderSel))
			if err != nil || idx < 0 || idx >= len(callArgs) {
				return true
			}

			cursor.Replace(dst.Clone(callArgs[idx]))
			swappedIdxs[idx] = true
			return false
		},
		nil,
	)
	expr, _ := result.(dst.Expr)
	return expr, swappedIdxs
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
