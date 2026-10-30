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
	"go.opentelemetry.io/otelc/tool/util"
)

// placeholderIdent is substituted for "{{ . }}" during template execution,
// then replaced with the actual AST node once the rendered text has been
// parsed. It must be a syntactically valid Go expression on its own.
const placeholderIdent = toolast.IdentIgnore + "." + placeholderSel

// placeholderSel is the selector part of placeholderIdent.
const placeholderSel = "PLACEHOLDER_0"

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

// callRecvPlaceholderSel is the selector part of the placeholder substituted
// for "{{ .CallReceiver }}" during template execution. It is swapped for the
// receiver's AST the same way as the call argument placeholders.
const callRecvPlaceholderSel = "CALLRECV"

// callRecvPlaceholderIdent is the placeholder for the wrapped call's
// receiver. It must be a syntactically valid Go expression on its own.
const callRecvPlaceholderIdent = toolast.IdentIgnore + "." + callRecvPlaceholderSel

// callTemplate represents a code template that can be used to wrap or transform
// Go expressions. It uses text/template for template execution and supports
// placeholder substitution for AST nodes.
type callTemplate struct {
	template *template.Template
	source   string
}

// newCallTemplate creates a new callTemplate from the provided template text.
// The template text uses {{ . }} for the wrapped expression, or
// {{ .CallReceiver }}, {{ .CallArguments }} and {{ .CallArgument N }} for the
// parts of a wrapped call.
//
// Examples:
//
//	newCallTemplate("wrapper({{ . }})")
//	newCallTemplate("{{ .CallReceiver }}.Info({{ .CallArguments }})")
func newCallTemplate(text string) (*callTemplate, error) {
	tmpl, err := template.New("call").Parse(text)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to parse template %s", text)
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

	// isCall, callArgs and callEllipsis describe the wrapped expression when
	// it is a function call (e.g. wrap_call's matched call site).
	// callEllipsis is true when the call spreads its last argument with "...".
	isCall       bool
	callArgs     []dst.Expr
	callEllipsis bool

	// callRecv is the receiver expression of the wrapped call when it was
	// matched as a method call, and nil otherwise.
	callRecv dst.Expr

	// clone copies a part of the wrapped call into the template output.
	clone func(dst.Expr) dst.Expr
}

// callSite describes where the wrapped call sits.
type callSite struct {
	// methodCall is true when the call was matched by method_call, so the X
	// of its selector is the receiver expression.
	methodCall bool
	// deferred is true when the call is the call of a defer or go statement.
	deferred bool
	// clone copies the receiver or an argument of the call into the template
	// output. Nil means dst.Clone.
	clone func(dst.Expr) dst.Expr
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

	// rendered records which parts of the wrapped call the template
	// rendered, so compileExpression can verify every placeholder was
	// swapped for the real AST and check the evaluation of those parts.
	rendered *renderedCallParts
}

// renderedCallParts records the call parts rendered by a template.
type renderedCallParts struct {
	// args holds each index rendered by {{ .CallArgument N }}.
	args map[int]bool
	// receiver is true when {{ .CallReceiver }} was rendered.
	receiver bool
	// argumentList is true when {{ .CallArguments }} was rendered.
	argumentList bool
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
	if err := d.checkCallArgument(idx); err != nil {
		return "", err
	}
	d.rendered.args[idx] = true
	return callArgPlaceholderIdent(idx), nil
}

// CallArguments renders the wrapped call's arguments as placeholders, in the
// same form as callTemplateData.CallArguments. Template usage:
// {{.CallArguments}}
func (d renderingCallTemplateData) CallArguments() (string, error) {
	if _, err := d.CallArgumentCount(); err != nil {
		return "", err
	}
	args := make([]string, len(d.callArgs))
	for i := range d.callArgs {
		args[i] = callArgPlaceholderIdent(i)
	}
	d.rendered.argumentList = true
	return joinCallArguments(args, d.callEllipsis), nil
}

// CallReceiver renders the wrapped call's receiver as a placeholder. See
// CallArgument for why. Template usage: {{.CallReceiver}}
func (d renderingCallTemplateData) CallReceiver() (string, error) {
	if err := d.checkCallReceiver(); err != nil {
		return "", err
	}
	d.rendered.receiver = true
	return callRecvPlaceholderIdent, nil
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
	if err := d.checkCallArgument(idx); err != nil {
		return "", err
	}
	return toolast.RenderExpr(d.callArgs[idx])
}

// checkCallArgument returns the error of {{ .CallArgument idx }}, or nil when
// the wrapped call has that argument.
func (d *callTemplateData) checkCallArgument(idx int) error {
	if !d.isCall {
		return notACallErr()
	}
	if idx < 0 || idx >= len(d.callArgs) {
		return ex.Newf("CallArgument index %d out of range [0, %d)", idx, len(d.callArgs))
	}
	return nil
}

// CallArguments returns the source text of all arguments of the wrapped call
// expression, separated by ", ", with a trailing "..." when the call spreads
// its last argument. It returns "" for a call without arguments. Only
// available when the wrapped expression is itself a function call. Template
// usage: {{.CallArguments}}
func (d *callTemplateData) CallArguments() (string, error) {
	if !d.isCall {
		return "", notACallErr()
	}
	args := make([]string, len(d.callArgs))
	for i, arg := range d.callArgs {
		text, err := toolast.RenderExpr(arg)
		if err != nil {
			return "", err
		}
		args[i] = text
	}
	return joinCallArguments(args, d.callEllipsis), nil
}

func joinCallArguments(args []string, ellipsis bool) string {
	joined := strings.Join(args, ", ")
	if ellipsis && len(args) > 0 {
		joined += "..."
	}
	return joined
}

// CallReceiver returns the source text of the receiver expression of the
// wrapped method call, e.g. "log" for log.Info(msg). Only available when the
// call was matched by method_call. Template usage: {{.CallReceiver}}
func (d *callTemplateData) CallReceiver() (string, error) {
	if err := d.checkCallReceiver(); err != nil {
		return "", err
	}
	return toolast.RenderExpr(d.callRecv)
}

// checkCallReceiver returns the error of {{ .CallReceiver }}, or nil when the
// wrapped call has a receiver.
func (d *callTemplateData) checkCallReceiver() error {
	if !d.isCall {
		return notACallErr()
	}
	if d.callRecv == nil {
		return ex.Newf("CallReceiver is only available for a call matched by method_call in a call rule; " +
			"the wrapped expression has no receiver")
	}
	return nil
}

// compileExpression executes the template with the given expression node as
// the placeholder value, parses the result, and returns the transformed expression.
// site describes node when it is a matched call site: it makes the receiver
// of a method call available to {{ .CallReceiver }}, and tells whether the
// call is the call of a defer or go statement. Pass callSite{} otherwise.
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
//     "{{ . }}", _.CALLARG_<N> for "{{ .CallArgument N }}" and
//     "{{ .CallArguments }}", _.CALLRECV for "{{ .CallReceiver }}")
//  2. Parse the result as a Go statement snippet
//  3. Rewrite the rule's own qualifiers in that parsed result
//  4. Replace the placeholders with the actual AST nodes, which keeps the
//     target's own code untouched by the rewrite in step 3
func (t *callTemplate) compileExpression(
	node dst.Expr, site callSite, enclosing *dst.FuncDecl, imports, aliasOverrides map[string]string,
) (dst.Expr, error) {
	data := &callTemplateData{clone: site.clone}
	if data.clone == nil {
		data.clone = func(expr dst.Expr) dst.Expr { return util.AssertType[dst.Expr](dst.Clone(expr)) }
	}
	if enclosing != nil {
		data.enclosing = newFuncTemplateData(enclosing, nil, imports, "")
	}
	if call, ok := unwrap(node).(*dst.CallExpr); ok {
		data.isCall = true
		data.callArgs = call.Args
		data.callEllipsis = call.Ellipsis
		if sel, isSel := call.Fun.(*dst.SelectorExpr); isSel && site.methodCall {
			data.callRecv = sel.X
		}
	}

	renderData := renderingCallTemplateData{
		callTemplateData: data,
		rendered:         &renderedCallParts{args: make(map[int]bool)},
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

	rendered := renderData.rendered
	if !placeholderRendered && len(rendered.args) == 0 && !rendered.receiver && !rendered.argumentList {
		return nil, ex.Newf(
			"template output did not use {{ . }}, {{ .CallReceiver }}, {{ .CallArguments }} "+
				"or {{ .CallArgument N }}\nGenerated code:\n%s", userResult,
		)
	}

	replaceQualifierAliases(exprStmt.X, aliasOverrides)
	stripDynamicIdents(exprStmt.X)

	if rendered.receiver || rendered.argumentList || len(rendered.args) > 0 {
		if err = checkCallPartsEvaluation(exprStmt.X, data, site.deferred); err != nil {
			return nil, err
		}
	}

	// Swap the call-part placeholders before the {{ . }} placeholder so
	// all of them run after the qualifier rewrite; the swapped-in ASTs then
	// keep the target's original code untouched.
	var swapped map[int]bool
	exprStmt.X, swapped = data.replaceCallPartPlaceholders(exprStmt.X)
	if err = checkSwappedCallParts(rendered, swapped, len(data.callArgs)); err != nil {
		return nil, err
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
	if name, found := findPlaceholder(resultExpr); found {
		return nil, ex.Newf("template output uses _.%s, a name reserved for otelc placeholders", name)
	}

	return resultExpr, nil
}

// findPlaceholder returns the selector name of a placeholder left in expr,
// which happens when the template text itself contains one, e.g. _.CALLRECV
// in a rule matched by function_call.
func findPlaceholder(expr dst.Expr) (string, bool) {
	var name string
	dst.Inspect(expr, func(node dst.Node) bool {
		sel, ok := placeholderSelName(node)
		if ok && (sel == placeholderSel || sel == callRecvPlaceholderSel ||
			strings.HasPrefix(sel, callArgPlaceholderSel)) {
			name = sel
		}
		return name == ""
	})
	return name, name != ""
}

// checkSwappedCallParts returns an error when a call part that the template
// rendered was not swapped, because its placeholder is not an expression of
// the template output (e.g. it is inside a string literal).
func checkSwappedCallParts(rendered *renderedCallParts, swapped map[int]bool, argc int) error {
	const msg = "template output did not contain expected placeholder expression for "
	for idx := range rendered.args {
		if !swapped[idx] {
			return ex.Newf(msg+"{{ .CallArgument %d }}", idx)
		}
	}
	if rendered.argumentList {
		for idx := range argc {
			if !swapped[idx] {
				return ex.New(msg + "{{ .CallArguments }}")
			}
		}
	}
	if rendered.receiver && !swapped[callPartReceiver] {
		return ex.New(msg + "{{ .CallReceiver }}")
	}
	return nil
}

// callPartReceiver identifies the receiver in checkCallPartsEvaluation. An
// index >= 0 identifies the call argument at that index.
const callPartReceiver = -1

// callPartUse is one use of the receiver or of an argument of the wrapped
// call in the template output.
type callPartUse struct {
	part      int
	inFuncLit bool
}

// checkCallPartsEvaluation checks that the template output expr evaluates the
// receiver and the arguments of the wrapped call as the original call does.
// It runs on the parsed output before the placeholders are swapped, for
// templates that use {{ .CallReceiver }}, {{ .CallArguments }} or
// {{ .CallArgument N }}. Every placeholder that stands for original code is
// counted, including {{ . }}.
//
// A receiver or argument that is a name, a selector on a name, or a literal
// has no side effects, so the output can use it any number of times. The
// output must use any other receiver or argument, such as a call, exactly
// once, and in the order of the original call: the receiver first, then the
// arguments from left to right.
//
// When the wrapped call is the call of a defer or go statement (deferred),
// the output must not use the receiver or an argument inside a function
// literal. The original call evaluates them at the defer or go statement,
// and the function literal evaluates them later.
func checkCallPartsEvaluation(expr dst.Expr, data *callTemplateData, deferred bool) error {
	counts := make(map[int]int)
	lastPart := callPartReceiver - 1
	for _, use := range collectCallPartUses(expr, data) {
		if deferred && use.inFuncLit {
			return ex.Newf("the template uses the %s inside a function literal, but the matched call "+
				"is the call of a defer or go statement; the original call evaluates it at that statement",
				data.describeCallPart(use.part))
		}
		if hasNoSideEffects(data.callPart(use.part)) {
			continue
		}
		if use.part < lastPart {
			return ex.Newf("the template evaluates the %s before the %s; it must keep the order of the "+
				"original call because both can have side effects",
				data.describeCallPart(lastPart), data.describeCallPart(use.part))
		}
		lastPart = use.part
		counts[use.part]++
	}

	first := 0
	if data.callRecv != nil {
		first = callPartReceiver
	}
	for part := first; part < len(data.callArgs); part++ {
		if n := counts[part]; n != 1 && !hasNoSideEffects(data.callPart(part)) {
			return ex.Newf("the template evaluates the %s %d times; it must evaluate it exactly once "+
				"because it can have side effects", data.describeCallPart(part), n)
		}
	}
	return nil
}

// collectCallPartUses returns, in source order, every use in expr of the
// receiver or of an argument of the wrapped call. A {{ . }} placeholder uses
// the receiver and every argument.
func collectCallPartUses(expr dst.Expr, data *callTemplateData) []callPartUse {
	var uses []callPartUse
	funcLitDepth := 0
	dstutil.Apply(
		expr,
		func(cursor *dstutil.Cursor) bool {
			switch node := cursor.Node().(type) {
			case *dst.FuncLit:
				funcLitDepth++
			case *dst.SelectorExpr:
				name, ok := placeholderSelName(node)
				if !ok {
					return true
				}
				for _, part := range data.placeholderCallParts(name) {
					uses = append(uses, callPartUse{part: part, inFuncLit: funcLitDepth > 0})
				}
				return false
			}
			return true
		},
		func(cursor *dstutil.Cursor) bool {
			if _, ok := cursor.Node().(*dst.FuncLit); ok {
				funcLitDepth--
			}
			return true
		},
	)
	return uses
}

// placeholderCallParts returns the call parts that the placeholder with
// selector name stands for.
func (d *callTemplateData) placeholderCallParts(name string) []int {
	if name == placeholderSel {
		parts := make([]int, 0, 1+len(d.callArgs))
		if d.callRecv != nil {
			parts = append(parts, callPartReceiver)
		}
		for i := range d.callArgs {
			parts = append(parts, i)
		}
		return parts
	}
	if part, ok := d.callPartOf(name); ok {
		return []int{part}
	}
	return nil
}

// placeholderSelName returns the selector name of node when node is a
// placeholder (_.<SEL>).
func placeholderSelName(node dst.Node) (string, bool) {
	sel, ok := node.(*dst.SelectorExpr)
	if !ok {
		return "", false
	}
	ident, ok := sel.X.(*dst.Ident)
	if !ok || ident.Name != toolast.IdentIgnore {
		return "", false
	}
	return sel.Sel.Name, true
}

// callPartOf returns the call part that the placeholder with selector name
// stands for: callPartReceiver for _.CALLRECV, or the argument index for
// _.CALLARG_<idx>. The result is false for any other name, and for a part
// that the wrapped call does not have.
func (d *callTemplateData) callPartOf(name string) (int, bool) {
	if name == callRecvPlaceholderSel {
		return callPartReceiver, d.callRecv != nil
	}
	idxText, ok := strings.CutPrefix(name, callArgPlaceholderSel)
	if !ok {
		return 0, false
	}
	idx, err := strconv.Atoi(idxText)
	if err != nil || idx < 0 || idx >= len(d.callArgs) {
		return 0, false
	}
	return idx, true
}

// callPart returns the receiver for callPartReceiver, or the argument at
// index part.
func (d *callTemplateData) callPart(part int) dst.Expr {
	if part == callPartReceiver {
		return d.callRecv
	}
	return d.callArgs[part]
}

// describeCallPart names a call part in error messages, e.g. "receiver log"
// or "argument 1 (zap.String("k", v))".
func (d *callTemplateData) describeCallPart(part int) string {
	text, err := toolast.RenderExpr(d.callPart(part))
	if err != nil {
		text = "?"
	}
	if part == callPartReceiver {
		return "receiver " + text
	}
	return "argument " + strconv.Itoa(part) + " (" + text + ")"
}

// hasNoSideEffects reports whether evaluating expr has no side effects: expr
// is a name, a literal, or a selector or parentheses around one of these.
func hasNoSideEffects(expr dst.Expr) bool {
	switch e := expr.(type) {
	case *dst.Ident, *dst.BasicLit:
		return true
	case *dst.SelectorExpr:
		return hasNoSideEffects(e.X)
	case *dst.ParenExpr:
		return hasNoSideEffects(e.X)
	default:
		return false
	}
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

// replaceCallPartPlaceholders replaces every _.CALLRECV and _.CALLARG_<idx>
// occurrence in node with its own copy (see callTemplateData.clone) of that
// part of the wrapped call, and returns the resulting expression plus the
// parts it swapped. This is used to inject the wrapped call's parts into the
// template-generated code after the alias rewrite, so their original code is
// never rewritten.
func (d *callTemplateData) replaceCallPartPlaceholders(node dst.Expr) (dst.Expr, map[int]bool) {
	swapped := make(map[int]bool)
	result := dstutil.Apply(
		node,
		func(cursor *dstutil.Cursor) bool {
			name, ok := placeholderSelName(cursor.Node())
			if !ok {
				return true
			}
			part, ok := d.callPartOf(name)
			if !ok {
				return true
			}
			cursor.Replace(d.clone(d.callPart(part)))
			swapped[part] = true
			return false
		},
		nil,
	)
	expr, _ := result.(dst.Expr)
	return expr, swapped
}

// replacePlaceholder replaces all occurrences of _.PLACEHOLDER_0 in the AST
// with the given node. This is used to inject the original call expression
// into the template-generated code.
func replacePlaceholder(node, replacement dst.Node) (dst.Node, bool) {
	replaced := false
	result := dstutil.Apply(
		node,
		func(cursor *dstutil.Cursor) bool {
			if name, ok := placeholderSelName(cursor.Node()); !ok || name != placeholderSel {
				return true
			}
			cursor.Replace(replacement)
			replaced = true
			return false
		},
		nil,
	)
	return result, replaced
}
