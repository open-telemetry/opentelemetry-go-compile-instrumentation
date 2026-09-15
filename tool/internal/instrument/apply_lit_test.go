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

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// makeLitFile builds a minimal *dst.File whose single function returns the
// given composite literal.
func makeLitFile(lit *dst.CompositeLit) *dst.File {
	return &dst.File{
		Name: &dst.Ident{Name: "main"},
		Decls: []dst.Decl{
			&dst.FuncDecl{
				Name: &dst.Ident{Name: "f"},
				Type: &dst.FuncType{Params: &dst.FieldList{}},
				Body: &dst.BlockStmt{
					List: []dst.Stmt{
						&dst.ReturnStmt{Results: []dst.Expr{lit}},
					},
				},
			},
		},
	}
}

func transportLit(elts ...dst.Expr) *dst.CompositeLit {
	return &dst.CompositeLit{
		Type: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "http", Path: "net/http"},
			Sel: &dst.Ident{Name: "Transport"},
		},
		Elts: elts,
	}
}

func keyed(name, value string) *dst.KeyValueExpr {
	return &dst.KeyValueExpr{
		Key:   &dst.Ident{Name: name},
		Value: &dst.Ident{Name: value},
	}
}

func transportRule(fields ...*rule.InstLitField) *rule.InstLitRule {
	return &rule.InstLitRule{
		InstBaseRule:  rule.InstBaseRule{Name: "mark_internal"},
		StructLiteral: "net/http.Transport",
		ImportPath:    "net/http",
		TypeName:      "Transport",
		Field:         fields,
	}
}

// litKeys returns the literal's keyed elements as name/value pairs, so tests
// can assert on both content and order.
func litKeys(t *testing.T, lit *dst.CompositeLit) [][2]string {
	t.Helper()
	pairs := make([][2]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		require.True(t, ok, "element is not keyed")
		key, ok := kv.Key.(*dst.Ident)
		require.True(t, ok, "key is not an identifier")
		pairs = append(pairs, [2]string{key.Name, exprString(t, kv.Value)})
	}
	return pairs
}

func exprString(t *testing.T, expr dst.Expr) string {
	t.Helper()
	switch e := expr.(type) {
	case *dst.Ident:
		return e.Name
	case *dst.BasicLit:
		return e.Value
	default:
		t.Fatalf("unexpected expression type %T", expr)
		return ""
	}
}

// --- matchesLitRule tests ---

func TestMatchesLitRule(t *testing.T) {
	r := transportRule(&rule.InstLitField{Name: "Internal", Value: "true"})

	tests := []struct {
		name    string
		lit     *dst.CompositeLit
		aliases map[string]string
		want    bool
	}{
		{
			name: "qualified literal with resolved path",
			lit:  transportLit(),
			want: true,
		},
		{
			name: "qualified literal resolved through import alias",
			lit: &dst.CompositeLit{Type: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "nethttp"},
				Sel: &dst.Ident{Name: "Transport"},
			}},
			aliases: map[string]string{"nethttp": "net/http"},
			want:    true,
		},
		{
			name: "alias resolving to a different package",
			lit: &dst.CompositeLit{Type: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "http"},
				Sel: &dst.Ident{Name: "Transport"},
			}},
			aliases: map[string]string{"http": "example.com/http"},
			want:    false,
		},
		{
			name: "unresolvable alias",
			lit: &dst.CompositeLit{Type: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "unknown"},
				Sel: &dst.Ident{Name: "Transport"},
			}},
			want: false,
		},
		{
			name: "same package name, different type",
			lit: &dst.CompositeLit{Type: &dst.SelectorExpr{
				X:   &dst.Ident{Name: "http", Path: "net/http"},
				Sel: &dst.Ident{Name: "Server"},
			}},
			want: false,
		},
		{
			name: "unqualified literal is not matched",
			lit:  &dst.CompositeLit{Type: &dst.Ident{Name: "Transport"}},
			want: false,
		},
		{
			name: "elided type is not matched",
			lit:  &dst.CompositeLit{},
			want: false,
		},
		{
			name: "chained selector qualifier is not matched",
			lit: &dst.CompositeLit{Type: &dst.SelectorExpr{
				X: &dst.SelectorExpr{
					X:   &dst.Ident{Name: "outer"},
					Sel: &dst.Ident{Name: "http"},
				},
				Sel: &dst.Ident{Name: "Transport"},
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aliases := tt.aliases
			if aliases == nil {
				aliases = map[string]string{}
			}
			assert.Equal(t, tt.want, matchesLitRule(tt.lit, r, aliases))
		})
	}
}

// --- applyLitRule tests ---

func TestApplyLitRule_SetsFieldOnEmptyLiteral(t *testing.T) {
	lit := transportLit()
	file := makeLitFile(lit)
	r := transportRule(&rule.InstLitField{Name: "Internal", Value: "true"})

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	assert.Equal(t, [][2]string{{"Internal", "true"}}, litKeys(t, lit))
}

func TestApplyLitRule_PreservesExistingElements(t *testing.T) {
	lit := transportLit(keyed("MaxIdleConns", "100"))
	file := makeLitFile(lit)
	r := transportRule(&rule.InstLitField{Name: "Internal", Value: "true"})

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	// The new field is prepended; the literal's own element is untouched.
	assert.Equal(t, [][2]string{{"Internal", "true"}, {"MaxIdleConns", "100"}}, litKeys(t, lit))
}

func TestApplyLitRule_OverridesExistingFieldInPlace(t *testing.T) {
	lit := transportLit(keyed("MaxIdleConns", "100"), keyed("Internal", "false"))
	file := makeLitFile(lit)
	r := transportRule(&rule.InstLitField{Name: "Internal", Value: "true"})

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	// Overriding keeps the field where it was rather than moving it to the front.
	assert.Equal(t, [][2]string{{"MaxIdleConns", "100"}, {"Internal", "true"}}, litKeys(t, lit))
}

func TestApplyLitRule_SetsMultipleFields(t *testing.T) {
	lit := transportLit()
	file := makeLitFile(lit)
	r := transportRule(
		&rule.InstLitField{Name: "Internal", Value: "true"},
		&rule.InstLitField{Name: "MaxIdleConns", Value: "50"},
	)

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	assert.Equal(t, [][2]string{{"Internal", "true"}, {"MaxIdleConns", "50"}}, litKeys(t, lit))
}

func TestApplyLitRule_SkipsPositionalLiteral(t *testing.T) {
	// A literal mixing keyed and positional elements is invalid Go, so a
	// positional literal must be left exactly as written.
	lit := transportLit(&dst.BasicLit{Kind: token.INT, Value: "1"})
	file := makeLitFile(lit)
	r := transportRule(&rule.InstLitField{Name: "Internal", Value: "true"})

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	require.Len(t, lit.Elts, 1)
	assert.IsType(t, &dst.BasicLit{}, lit.Elts[0])
}

func TestApplyLitRule_NoMatchIsNoOp(t *testing.T) {
	lit := &dst.CompositeLit{
		Type: &dst.SelectorExpr{
			X:   &dst.Ident{Name: "http", Path: "net/http"},
			Sel: &dst.Ident{Name: "Server"},
		},
	}
	file := makeLitFile(lit)
	r := transportRule(&rule.InstLitField{Name: "Internal", Value: "true"})

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	assert.Empty(t, lit.Elts)
}

// --- per-field wrap tests ---

// wrapExprString renders a wrapped value, which is a call expression rather
// than the bare identifiers exprString handles.
func wrapExprString(t *testing.T, expr dst.Expr) string {
	t.Helper()
	call, ok := expr.(*dst.CallExpr)
	require.True(t, ok, "expected a call expression, got %T", expr)
	fun, ok := call.Fun.(*dst.Ident)
	require.True(t, ok, "expected a plain function identifier, got %T", call.Fun)
	require.Len(t, call.Args, 1)
	return fun.Name + "(" + exprString(t, call.Args[0]) + ")"
}

func TestApplyLitRule_WrapsExistingFieldValue(t *testing.T) {
	lit := transportLit(keyed("Proxy", "myProxy"))
	file := makeLitFile(lit)
	r := transportRule(&rule.InstLitField{Name: "Proxy", Wrap: "wrapProxy({{ . }})"})

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	require.Len(t, lit.Elts, 1)
	kv, ok := lit.Elts[0].(*dst.KeyValueExpr)
	require.True(t, ok)
	assert.Equal(t, "wrapProxy(myProxy)", wrapExprString(t, kv.Value))
}

func TestApplyLitRule_WrapOnlySkipsAbsentField(t *testing.T) {
	// wrap has nothing to substitute when the literal omits the field, so the
	// literal is left alone rather than wrapping an invented value.
	lit := transportLit()
	file := makeLitFile(lit)
	r := transportRule(&rule.InstLitField{Name: "Proxy", Wrap: "wrapProxy({{ . }})"})

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	assert.Empty(t, lit.Elts)
}

func TestApplyLitRule_ValueFillsAbsentFieldAlongsideWrap(t *testing.T) {
	lit := transportLit()
	file := makeLitFile(lit)
	r := transportRule(&rule.InstLitField{
		Name:  "Proxy",
		Wrap:  "wrapProxy({{ . }})",
		Value: "defaultProxy",
	})

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	assert.Equal(t, [][2]string{{"Proxy", "defaultProxy"}}, litKeys(t, lit))
}

func TestApplyLitRule_WrapTakesPrecedenceOverValueWhenPresent(t *testing.T) {
	lit := transportLit(keyed("Proxy", "myProxy"))
	file := makeLitFile(lit)
	r := transportRule(&rule.InstLitField{
		Name:  "Proxy",
		Wrap:  "wrapProxy({{ . }})",
		Value: "defaultProxy",
	})

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	require.Len(t, lit.Elts, 1)
	kv, ok := lit.Elts[0].(*dst.KeyValueExpr)
	require.True(t, ok)
	assert.Equal(t, "wrapProxy(myProxy)", wrapExprString(t, kv.Value))
}

// A literal of the rule's own type can sit inside the value another matched
// literal wraps. Both must come out instrumented, and the nested one must stay
// attached to the file rather than being replaced by an untouched copy.
func TestApplyLitRule_WrapInstrumentsNestedMatchedLiteral(t *testing.T) {
	inner := transportLit(keyed("MaxIdleConns", "100"))
	outer := transportLit(&dst.KeyValueExpr{
		Key:   &dst.Ident{Name: "Proxy"},
		Value: &dst.CallExpr{Fun: &dst.Ident{Name: "identity"}, Args: []dst.Expr{inner}},
	})
	file := makeLitFile(outer)
	r := transportRule(
		&rule.InstLitField{Name: "Proxy", Wrap: "wrapProxy({{ . }})"},
		&rule.InstLitField{Name: "Internal", Value: "true"},
	)

	ip := newTestPhase()
	require.NoError(t, ip.applyLitRule(context.Background(), r, file))

	// Read the literals back out of the file, not through the nodes handed in,
	// so a nested literal detached from the tree cannot pass this test.
	var inTree []*dst.CompositeLit
	dst.Inspect(file, func(node dst.Node) bool {
		if lit, ok := node.(*dst.CompositeLit); ok {
			inTree = append(inTree, lit)
		}
		return true
	})
	require.Len(t, inTree, 2)

	assert.Equal(t, []string{"Internal", "Proxy"}, keyNames(t, inTree[0]))
	assert.Equal(t, [][2]string{{"Internal", "true"}, {"MaxIdleConns", "100"}}, litKeys(t, inTree[1]))

	// The nested literal reached through the wrapped value is the same node, so
	// wrapping did not leave a stale copy behind.
	outerProxy := findKeyedElement(inTree[0], "Proxy")
	require.NotNil(t, outerProxy)
	wrapCall, ok := outerProxy.Value.(*dst.CallExpr)
	require.True(t, ok, "expected Proxy to be wrapped, got %T", outerProxy.Value)
	assert.Equal(t, "wrapProxy", wrapCall.Fun.(*dst.Ident).Name)
	identityCall, ok := wrapCall.Args[0].(*dst.CallExpr)
	require.True(t, ok, "expected the original identity call inside the wrap, got %T", wrapCall.Args[0])
	assert.Same(t, inTree[1], identityCall.Args[0])
}

// keyNames returns the names of the literal's keyed elements, ignoring values.
// Unlike litKeys it tolerates values that are not plain identifiers.
func keyNames(t *testing.T, lit *dst.CompositeLit) []string {
	t.Helper()
	names := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*dst.KeyValueExpr)
		require.True(t, ok, "element is not keyed")
		key, ok := kv.Key.(*dst.Ident)
		require.True(t, ok, "key is not an identifier")
		names = append(names, key.Name)
	}
	return names
}

func TestApplyLitRule_InvalidWrapTemplate(t *testing.T) {
	file := makeLitFile(transportLit(keyed("Proxy", "myProxy")))
	r := transportRule(&rule.InstLitField{Name: "Proxy", Wrap: "wrapProxy({{ . }}"})

	ip := newTestPhase()
	err := ip.applyLitRule(context.Background(), r, file)
	require.Error(t, err)
}

func TestApplyLitRule_InvalidValueExpression(t *testing.T) {
	file := makeLitFile(transportLit())
	r := transportRule(&rule.InstLitField{Name: "Internal", Value: "func("})

	ip := newTestPhase()
	err := ip.applyLitRule(context.Background(), r, file)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse value")
}
