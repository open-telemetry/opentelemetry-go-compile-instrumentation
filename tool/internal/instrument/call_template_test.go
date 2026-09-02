// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"go/token"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCallTemplate_Success(t *testing.T) {
	text := "wrapper({{ . }})"

	tmpl, err := newCallTemplate(text)

	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, text, tmpl.String())
}

func TestNewCallTemplate_InvalidSyntax(t *testing.T) {
	text := "wrapper({{ .Field )" // Invalid template syntax - missing closing }}

	tmpl, err := newCallTemplate(text)

	require.Error(t, err)
	assert.Nil(t, tmpl)
	assert.Contains(t, err.Error(), "failed to parse template")
}

func TestNewCallTemplate_EmptyTemplate(t *testing.T) {
	text := ""

	tmpl, err := newCallTemplate(text)

	require.NoError(t, err)
	assert.NotNil(t, tmpl)
	assert.Equal(t, text, tmpl.String())
}

func TestCallTemplateData_FuncName(t *testing.T) {
	t.Run("no enclosing function errors", func(t *testing.T) {
		d := &callTemplateData{}

		_, err := d.FuncName()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no enclosing function is available")
	})

	t.Run("delegates to enclosing function", func(t *testing.T) {
		enclosing := parseFunc(t, "package main\nfunc Handler() {}")
		d := &callTemplateData{enclosing: newFuncTemplateData(enclosing, nil, nil, "")}

		name, err := d.FuncName()

		require.NoError(t, err)
		assert.Equal(t, "Handler", name)
	})
}

func TestCallTemplateData_FuncArgument(t *testing.T) {
	t.Run("no enclosing function errors", func(t *testing.T) {
		d := &callTemplateData{}

		_, err := d.FuncArgument(0)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no enclosing function is available")
	})

	t.Run("delegates to enclosing function", func(t *testing.T) {
		enclosing := parseFunc(t, "package main\nfunc Handler(name string) {}")
		d := &callTemplateData{enclosing: newFuncTemplateData(enclosing, nil, nil, "")}

		arg, err := d.FuncArgument(0)

		require.NoError(t, err)
		assert.Equal(t, "name", arg)
	})
}

func TestCallTemplateData_FuncReturn(t *testing.T) {
	t.Run("no enclosing function errors", func(t *testing.T) {
		d := &callTemplateData{}

		_, err := d.FuncReturn(0)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no enclosing function is available")
	})

	t.Run("delegates to enclosing function", func(t *testing.T) {
		enclosing := parseFunc(t, "package main\nfunc Handler() (err error) { return nil }")
		d := &callTemplateData{enclosing: newFuncTemplateData(enclosing, nil, nil, "")}

		ret, err := d.FuncReturn(0)

		require.NoError(t, err)
		assert.Equal(t, "err", ret)
	})
}

func TestCallTemplateData_FuncArgumentCount(t *testing.T) {
	t.Run("no enclosing function errors", func(t *testing.T) {
		d := &callTemplateData{}

		_, err := d.FuncArgumentCount()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no enclosing function is available")
	})

	t.Run("delegates to enclosing function", func(t *testing.T) {
		enclosing := parseFunc(t, "package main\nfunc Handler(a, b string) {}")
		d := &callTemplateData{enclosing: newFuncTemplateData(enclosing, nil, nil, "")}

		count, err := d.FuncArgumentCount()

		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})
}

func TestCallTemplateData_FuncReturnCount(t *testing.T) {
	t.Run("no enclosing function errors", func(t *testing.T) {
		d := &callTemplateData{}

		_, err := d.FuncReturnCount()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no enclosing function is available")
	})

	t.Run("delegates to enclosing function", func(t *testing.T) {
		enclosing := parseFunc(t, "package main\nfunc Handler() (int, error) { return 0, nil }")
		d := &callTemplateData{enclosing: newFuncTemplateData(enclosing, nil, nil, "")}

		count, err := d.FuncReturnCount()

		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})
}

func TestCallTemplateData_Receiver(t *testing.T) {
	t.Run("no enclosing function errors", func(t *testing.T) {
		d := &callTemplateData{}

		_, err := d.Receiver()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no enclosing function is available")
	})

	t.Run("enclosing function without receiver errors", func(t *testing.T) {
		enclosing := parseFunc(t, "package main\nfunc Handler() {}")
		d := &callTemplateData{enclosing: newFuncTemplateData(enclosing, nil, nil, "")}

		_, err := d.Receiver()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no receiver")
	})

	t.Run("delegates to enclosing function", func(t *testing.T) {
		enclosing := parseFunc(t, "package main\ntype T struct{}\nfunc (t T) Handler() {}")
		d := &callTemplateData{enclosing: newFuncTemplateData(enclosing, nil, nil, "")}

		recv, err := d.Receiver()

		require.NoError(t, err)
		assert.Equal(t, "t", recv)
	})
}

func TestCompileExpression_ReceiverWithEnclosingMethod(t *testing.T) {
	tmpl, err := newCallTemplate("traced({{ .Receiver }}, {{ . }})")
	require.NoError(t, err)

	enclosing := parseFunc(t, "package main\ntype T struct{}\nfunc (t T) Handler() {}")
	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	recvArg, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "t", recvArg.Name)
}

func TestCallTemplateData_CallArgumentCount(t *testing.T) {
	t.Run("not a call errors", func(t *testing.T) {
		d := &callTemplateData{}

		_, err := d.CallArgumentCount()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires the wrapped expression to be a function call")
	})

	t.Run("returns number of call arguments", func(t *testing.T) {
		d := &callTemplateData{
			isCall:   true,
			callArgs: []dst.Expr{&dst.Ident{Name: "a"}, &dst.Ident{Name: "b"}, &dst.Ident{Name: "c"}},
		}

		count, err := d.CallArgumentCount()

		require.NoError(t, err)
		assert.Equal(t, 3, count)
	})

	t.Run("zero arguments", func(t *testing.T) {
		d := &callTemplateData{isCall: true}

		count, err := d.CallArgumentCount()

		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestCompileExpression_FuncArgumentWithEnclosingFunc(t *testing.T) {
	tmpl, err := newCallTemplate("traced({{ .FuncArgument 0 }}, {{ . }})")
	require.NoError(t, err)

	enclosing := parseFunc(t, "package main\nfunc Handler(name string) {}")
	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	nameArg, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "name", nameArg.Name)
}

func TestCompileExpression_FuncTagWithoutEnclosingFuncErrors(t *testing.T) {
	tmpl, err := newCallTemplate("traced({{ .FuncName }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	_, err = tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no enclosing function is available")
}

func TestCompileExpression_FuncArgumentOfType_Found(t *testing.T) {
	tmpl, err := newCallTemplate(`traced({{ .FuncArgumentOfType "context.Context" }}, {{ . }})`)
	require.NoError(t, err)

	enclosing := parseFunc(t, "package main\nfunc Handler(ctx context.Context, name string) {}")
	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	argIdent, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "ctx", argIdent.Name)
}

func TestCompileExpression_FuncArgumentOfType_NotFound(t *testing.T) {
	tmpl, err := newCallTemplate(`wrap("{{ .FuncArgumentOfType "io.Reader" }}", {{ . }})`)
	require.NoError(t, err)

	enclosing := parseFunc(t, "package main\nfunc Handler(ctx context.Context, name string) {}")
	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	lit, ok := resultCall.Args[0].(*dst.BasicLit)
	require.True(t, ok, "expected *dst.BasicLit, got %T", resultCall.Args[0])
	assert.Equal(t, `""`, lit.Value)
}

func TestCompileExpression_FuncArgumentOfType_SkipsUnsupportedParamTypes(t *testing.T) {
	tmpl, err := newCallTemplate(`traced({{ .FuncArgumentOfType "context.Context" }}, {{ . }})`)
	require.NoError(t, err)

	// data []byte has a type shape (slice) that MatchesTypeName cannot
	// compare against a plain type-name filter
	enclosing := parseFunc(t, "package main\nfunc Handler(data []byte, ctx context.Context) {}")
	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	argIdent, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "ctx", argIdent.Name)
}

func TestCompileExpression_FuncArgumentOfType_NoEnclosingFuncErrors(t *testing.T) {
	tmpl, err := newCallTemplate(`traced({{ .FuncArgumentOfType "context.Context" }})`)
	require.NoError(t, err)

	originalCall := &dst.CallExpr{Fun: &dst.Ident{Name: "funcCall"}}

	_, err = tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no enclosing function is available")
}

func TestCompileExpression_CallArgumentIsWrappedCallNotEnclosingFunc(t *testing.T) {
	tmpl, err := newCallTemplate("traced({{ .CallArgument 0 }}, {{ .FuncArgument 0 }}, {{ . }})")
	require.NoError(t, err)

	enclosing := parseFunc(t, "package main\nfunc Handler(outerParam string) {}")
	originalCall := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "getValue"},
		Args: []dst.Expr{&dst.Ident{Name: "innerArg"}},
	}

	result, err := tmpl.compileExpression(originalCall, enclosing)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 3)

	callArg, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "innerArg", callArg.Name, "CallArgument must resolve to the wrapped call's own argument")

	funcArg, ok := resultCall.Args[1].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[1])
	assert.Equal(t, "outerParam", funcArg.Name, "FuncArgument must resolve to the enclosing function's parameter")
}

func TestCompileExpression_WithoutDotExpression(t *testing.T) {
	tmpl, err := newCallTemplate(`replacement({{ .CallArgumentCount }})`)
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "sideEffectingCall"},
		Args: []dst.Expr{&dst.Ident{Name: "a"}, &dst.Ident{Name: "b"}},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 1)
	countLit, ok := resultCall.Args[0].(*dst.BasicLit)
	require.True(t, ok, "expected *dst.BasicLit, got %T", resultCall.Args[0])
	assert.Equal(t, "2", countLit.Value,
		"CallArgumentCount must still resolve even without referencing the call itself")
}

func TestCompileExpression_PerBranchDotUsage(t *testing.T) {
	tmpl, err := newCallTemplate(
		`{{- if .CallArgumentCount -}}` +
			`rebuilt({{ .CallArgument 0 }})` +
			`{{- else -}}` +
			`wrapped({{ . }})` +
			`{{- end -}}`,
	)
	require.NoError(t, err)

	t.Run("branch without dot drops the original call", func(t *testing.T) {
		originalCall := &dst.CallExpr{
			Fun:  &dst.Ident{Name: "getValue"},
			Args: []dst.Expr{&dst.Ident{Name: "a"}},
		}

		result, resErr := tmpl.compileExpression(originalCall, nil)
		require.NoError(t, resErr)

		resultCall, ok := result.(*dst.CallExpr)
		require.True(t, ok, "expected *dst.CallExpr, got %T", result)
		fn, ok := resultCall.Fun.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, "rebuilt", fn.Name)
	})

	t.Run("branch with dot requires it to survive", func(t *testing.T) {
		originalCall := &dst.CallExpr{
			Fun: &dst.Ident{Name: "getValue"},
		}

		result, resErr := tmpl.compileExpression(originalCall, nil)
		require.NoError(t, resErr)

		resultCall, ok := result.(*dst.CallExpr)
		require.True(t, ok, "expected *dst.CallExpr, got %T", result)
		fn, ok := resultCall.Fun.(*dst.Ident)
		require.True(t, ok)
		assert.Equal(t, "wrapped", fn.Name)
		require.Len(t, resultCall.Args, 1)
		_, ok = resultCall.Args[0].(*dst.CallExpr)
		require.True(t, ok, "expected the original call to survive inside the wrapper")
	})
}

func TestCompileExpression_CallArgumentCount(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgumentCount }}, {{ . }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "getValue"},
		Args: []dst.Expr{&dst.Ident{Name: "a"}, &dst.Ident{Name: "b"}},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	countLit, ok := resultCall.Args[0].(*dst.BasicLit)
	require.True(t, ok, "expected *dst.BasicLit, got %T", resultCall.Args[0])
	assert.Equal(t, "2", countLit.Value)
}

func TestCompileExpression_CallArgumentOutOfRange(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgument 5 }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "f"},
		Args: []dst.Expr{&dst.Ident{Name: "a"}},
	}

	_, err = tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestCompileExpression_CallArgumentUnwrapsParens(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgument 0 }}, {{ .CallArgumentCount }}, {{ . }})")
	require.NoError(t, err)

	call := &dst.CallExpr{
		Fun:  &dst.Ident{Name: "f"},
		Args: []dst.Expr{&dst.Ident{Name: "a"}},
	}
	parenthesized := &dst.ParenExpr{X: call}

	result, err := tmpl.compileExpression(parenthesized, nil)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 3)
	arg, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", resultCall.Args[0])
	assert.Equal(t, "a", arg.Name)
	countLit, ok := resultCall.Args[1].(*dst.BasicLit)
	require.True(t, ok, "expected *dst.BasicLit, got %T", resultCall.Args[1])
	assert.Equal(t, "1", countLit.Value)
}

func TestCompileExpression_CallArgumentRequiresCallExpr(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgument 0 }})")
	require.NoError(t, err)

	nonCall := &dst.BasicLit{Kind: token.INT, Value: "5"}

	_, err = tmpl.compileExpression(nonCall, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "function call")
}

func TestCompileExpression_CallArgumentComplexExpression(t *testing.T) {
	tmpl, err := newCallTemplate("wrap({{ .CallArgument 0 }}, {{ . }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "f"},
		Args: []dst.Expr{
			&dst.BinaryExpr{X: &dst.Ident{Name: "a"}, Op: token.ADD, Y: &dst.Ident{Name: "b"}},
		},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)
	require.Len(t, resultCall.Args, 2)
	arg, ok := resultCall.Args[0].(*dst.BinaryExpr)
	require.True(t, ok, "expected *dst.BinaryExpr, got %T", resultCall.Args[0])
	xIdent, ok := arg.X.(*dst.Ident)
	require.True(t, ok, "expected *dst.Ident, got %T", arg.X)
	assert.Equal(t, "a", xIdent.Name)
}

func TestCompileExpression_SimpleWrapping(t *testing.T) {
	tmpl, err := newCallTemplate("wrapper({{ . }})")
	require.NoError(t, err)

	// Create a simple call expression: funcCall()
	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "funcCall"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr, got %T", result)

	// Verify the outer wrapper function
	wrapperIdent, ok := resultCall.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "wrapper", wrapperIdent.Name)

	// Verify the original call is inside
	require.Len(t, resultCall.Args, 1)
	innerCall, ok := resultCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	innerIdent, ok := innerCall.Fun.(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "funcCall", innerIdent.Name)
}

func TestCompileExpression_IIFE(t *testing.T) {
	tmpl, err := newCallTemplate("(func() int { return {{ . }} })()")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression (the IIFE invocation)
	_, ok := result.(*dst.CallExpr)
	require.True(t, ok, "expected *dst.CallExpr for IIFE, got %T", result)
}

func TestCompileExpression_MultiplePlaceholders(t *testing.T) {
	// Template with multiple {{ . }} occurrences
	tmpl, err := newCallTemplate("combine({{ . }}, {{ . }})")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a call expression
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)

	// Verify both arguments are present
	assert.Len(t, resultCall.Args, 2)
}

func TestCompileExpression_InvalidGoSyntax(t *testing.T) {
	// Template that parses fine but produces invalid Go syntax
	tmpl, err := newCallTemplate("func {{ . }}") // "func" keyword without proper syntax
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "failed to parse generated code")
}

func TestCompileExpression_ComplexNestedExpression(t *testing.T) {
	tmpl, err := newCallTemplate("outer(middle({{ . }}))")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "inner"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify nested structure: outer(middle(inner()))
	outerCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "outer", outerCall.Fun.(*dst.Ident).Name)

	require.Len(t, outerCall.Args, 1)
	middleCall, ok := outerCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "middle", middleCall.Fun.(*dst.Ident).Name)

	require.Len(t, middleCall.Args, 1)
	innerCall, ok := middleCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "inner", innerCall.Fun.(*dst.Ident).Name)
}

func TestCompileExpression_WithBinaryExpression(t *testing.T) {
	tmpl, err := newCallTemplate("{{ . }} + 1")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getValue"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a binary expression
	binaryExpr, ok := result.(*dst.BinaryExpr)
	require.True(t, ok, "expected *dst.BinaryExpr, got %T", result)

	// Verify the left side is our call
	leftCall, ok := binaryExpr.X.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "getValue", leftCall.Fun.(*dst.Ident).Name)
}

func TestCompileExpression_SelectorExpression(t *testing.T) {
	tmpl, err := newCallTemplate("{{ . }}.Field")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "getStruct"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify it's a selector expression
	selExpr, ok := result.(*dst.SelectorExpr)
	require.True(t, ok, "expected *dst.SelectorExpr, got %T", result)
	assert.Equal(t, "Field", selExpr.Sel.Name)

	// Verify X is our call
	call, ok := selExpr.X.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "getStruct", call.Fun.(*dst.Ident).Name)
}

func TestCompileExpression_EmptyResult(t *testing.T) {
	// Template that produces nothing (empty expression)
	tmpl, err := newCallTemplate("")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	// Should error because the function body is empty
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "function body is empty")
}

func TestCompileExpression_PlaceholderNotReplaced(t *testing.T) {
	tmpl, err := newCallTemplate(`wrapper("{{ . }}")`)
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "did not contain expected placeholder expression")
}

func TestCompileExpression_MultipleStatements(t *testing.T) {
	tmpl, err := newCallTemplate("first(); {{ . }}")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "single expression statement")
}

func TestCompileExpression_NonExpressionStatement(t *testing.T) {
	// Template that produces a non-expression statement
	// This is tricky - we need something that parses as a statement but not as an expression
	tmpl, err := newCallTemplate("return")
	require.NoError(t, err)

	originalCall := &dst.CallExpr{
		Fun: &dst.Ident{Name: "test"},
	}

	result, err := tmpl.compileExpression(originalCall, nil)

	// Should error because it's not an expression statement
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "expected expression statement")
}

func TestReplacePlaceholder_SingleOccurrence(t *testing.T) {
	// Create AST with _.PLACEHOLDER_0
	astWithPlaceholder := &dst.CallExpr{
		Fun: &dst.Ident{Name: "wrapper"},
		Args: []dst.Expr{
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "_"},
				Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
			},
		},
	}

	// Create replacement node
	replacement := &dst.CallExpr{
		Fun: &dst.Ident{Name: "originalCall"},
	}

	// Replace
	result, replaced := replacePlaceholder(astWithPlaceholder, replacement)

	// Verify
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.True(t, replaced)
	assert.Equal(t, "wrapper", resultCall.Fun.(*dst.Ident).Name)

	require.Len(t, resultCall.Args, 1)
	replacedCall, ok := resultCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "originalCall", replacedCall.Fun.(*dst.Ident).Name)
}

func TestReplacePlaceholder_MultipleOccurrences(t *testing.T) {
	// Create AST with two _.PLACEHOLDER_0 occurrences
	astWithPlaceholders := &dst.CallExpr{
		Fun: &dst.Ident{Name: "combine"},
		Args: []dst.Expr{
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "_"},
				Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
			},
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "_"},
				Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
			},
		},
	}

	replacement := &dst.Ident{Name: "value"}

	result, replaced := replacePlaceholder(astWithPlaceholders, replacement)

	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.True(t, replaced)
	require.Len(t, resultCall.Args, 2)

	// Both should be replaced
	arg1, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "value", arg1.Name)

	arg2, ok := resultCall.Args[1].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "value", arg2.Name)
}

func TestReplacePlaceholder_NoPlaceholders(t *testing.T) {
	// Create AST without placeholders
	astWithoutPlaceholder := &dst.CallExpr{
		Fun: &dst.Ident{Name: "simpleCall"},
		Args: []dst.Expr{
			&dst.Ident{Name: "arg1"},
		},
	}

	replacement := &dst.Ident{Name: "shouldNotAppear"}

	result, replaced := replacePlaceholder(astWithoutPlaceholder, replacement)

	// Verify AST is unchanged
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.False(t, replaced)
	assert.Equal(t, "simpleCall", resultCall.Fun.(*dst.Ident).Name)

	require.Len(t, resultCall.Args, 1)
	arg, ok := resultCall.Args[0].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "arg1", arg.Name)
}

func TestReplacePlaceholder_NestedStructure(t *testing.T) {
	// Create nested AST with placeholder deep inside
	astWithNested := &dst.CallExpr{
		Fun: &dst.Ident{Name: "outer"},
		Args: []dst.Expr{
			&dst.CallExpr{
				Fun: &dst.Ident{Name: "middle"},
				Args: []dst.Expr{
					&dst.SelectorExpr{
						X:   &dst.Ident{Name: "_"},
						Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
					},
				},
			},
		},
	}

	replacement := &dst.Ident{Name: "innerValue"}

	result, replaced := replacePlaceholder(astWithNested, replacement)

	// Navigate to the nested location
	outerCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.True(t, replaced)

	middleCall, ok := outerCall.Args[0].(*dst.CallExpr)
	require.True(t, ok)

	innerValue, ok := middleCall.Args[0].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "innerValue", innerValue.Name)
}

func TestReplacePlaceholder_WrongSelectorPrefix(t *testing.T) {
	// Create selector that looks like placeholder but has wrong prefix (x.PLACEHOLDER_0)
	astWithWrongPrefix := &dst.CallExpr{
		Fun: &dst.Ident{Name: "wrapper"},
		Args: []dst.Expr{
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "x"}, // Not "_"
				Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
			},
		},
	}

	replacement := &dst.Ident{Name: "shouldNotReplace"}

	result, replaced := replacePlaceholder(astWithWrongPrefix, replacement)

	// Verify not replaced
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.False(t, replaced)

	selector, ok := resultCall.Args[0].(*dst.SelectorExpr)
	require.True(t, ok)
	assert.Equal(t, "x", selector.X.(*dst.Ident).Name)
	assert.Equal(t, "PLACEHOLDER_0", selector.Sel.Name)
}

func TestReplacePlaceholder_WrongSelectorName(t *testing.T) {
	// Create selector with right prefix but wrong name (_.OTHER)
	astWithWrongName := &dst.CallExpr{
		Fun: &dst.Ident{Name: "wrapper"},
		Args: []dst.Expr{
			&dst.SelectorExpr{
				X:   &dst.Ident{Name: "_"},
				Sel: &dst.Ident{Name: "OTHER"}, // Not "PLACEHOLDER_0"
			},
		},
	}

	replacement := &dst.Ident{Name: "shouldNotReplace"}

	result, replaced := replacePlaceholder(astWithWrongName, replacement)

	// Verify not replaced
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.False(t, replaced)

	selector, ok := resultCall.Args[0].(*dst.SelectorExpr)
	require.True(t, ok)
	assert.Equal(t, "_", selector.X.(*dst.Ident).Name)
	assert.Equal(t, "OTHER", selector.Sel.Name)
}

func TestReplacePlaceholder_ComplexAST(t *testing.T) {
	// Create complex AST with binary expressions, function calls, etc.
	astComplex := &dst.BinaryExpr{
		Op: 0, // placeholder for operator
		X: &dst.CallExpr{
			Fun: &dst.Ident{Name: "left"},
			Args: []dst.Expr{
				&dst.SelectorExpr{
					X:   &dst.Ident{Name: "_"},
					Sel: &dst.Ident{Name: "PLACEHOLDER_0"},
				},
			},
		},
		Y: &dst.CallExpr{
			Fun: &dst.Ident{Name: "right"},
			Args: []dst.Expr{
				&dst.BasicLit{Value: "42"},
			},
		},
	}

	replacement := &dst.Ident{Name: "replacedValue"}

	result, replaced := replacePlaceholder(astComplex, replacement)

	// Verify structure
	binaryExpr, ok := result.(*dst.BinaryExpr)
	require.True(t, ok)
	assert.True(t, replaced)

	leftCall, ok := binaryExpr.X.(*dst.CallExpr)
	require.True(t, ok)

	replacedIdent, ok := leftCall.Args[0].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "replacedValue", replacedIdent.Name)

	// Right side should be unchanged
	rightCall, ok := binaryExpr.Y.(*dst.CallExpr)
	require.True(t, ok)
	assert.Equal(t, "right", rightCall.Fun.(*dst.Ident).Name)
}

func TestReplacePlaceholder_NonSelectorNode(t *testing.T) {
	// Create AST with non-selector nodes (should be ignored by replacer)
	astWithLiteral := &dst.CallExpr{
		Fun: &dst.Ident{Name: "wrapper"},
		Args: []dst.Expr{
			&dst.BasicLit{Value: "\"string\""},
			&dst.Ident{Name: "ident"},
		},
	}

	replacement := &dst.Ident{Name: "shouldNotAppear"}

	result, replaced := replacePlaceholder(astWithLiteral, replacement)

	// Verify unchanged
	resultCall, ok := result.(*dst.CallExpr)
	require.True(t, ok)
	assert.False(t, replaced)
	require.Len(t, resultCall.Args, 2)

	lit, ok := resultCall.Args[0].(*dst.BasicLit)
	require.True(t, ok)
	assert.Equal(t, "\"string\"", lit.Value)

	ident, ok := resultCall.Args[1].(*dst.Ident)
	require.True(t, ok)
	assert.Equal(t, "ident", ident.Name)
}

func TestCompileExpression_UnknownTemplateTag(t *testing.T) {
	// text/template rejects an unrecognized bare identifier like "something"
	// at parse time (as an undefined function call), so newCallTemplate is
	// where the error now surfaces rather than compileExpression.
	_, err := newCallTemplate("wrapper({{ something }})")
	require.Error(t, err)
}

func TestParseGoExpression_NonExpressionStatement(t *testing.T) {
	_, err := parseGoExpression("return")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not parse as an expression statement")
}

func TestParseGoExpression_EmptyBody(t *testing.T) {
	_, err := parseGoExpression("")
	require.Error(t, err)
}

func TestParseGoTypeExpression_NoType(t *testing.T) {
	// "var _ = 1" has no explicit type on the value spec, so the parsed shape
	// must be rejected rather than silently producing a nil type.
	_, err := parseGoTypeExpression("= 1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected spec shape")
}
