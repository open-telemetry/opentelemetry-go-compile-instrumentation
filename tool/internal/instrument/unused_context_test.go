// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHookUsesContext(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         bool
	}{
		{"empty", "func hook(ctx Context) {}", false},
		{"unnamed", "func hook(Context) {}", false},
		{"blank", "func hook(_ Context) {}", false},
		{"typed argument", "func hook(ctx Context, n int) { consume(n) }", false},
		{"method", "func hook(ctx Context) { ctx.GetParam(0) }", true},
		{"method value", "func hook(ctx Context) { f := ctx.SetSkipCall; f(true) }", true},
		{"pass", "func hook(ctx Context) { consume(ctx) }", true},
		{"store", "func hook(ctx Context) { saved = ctx }", true},
		{"discard", "func hook(ctx Context) { _ = ctx }", true},
		{"closure", "func hook(ctx Context) { f := func() { consume(ctx) }; f() }", true},
		{"shadow", "func hook(ctx Context) { { ctx := 1; consume(ctx) } }", true},
		{"bodyless", "func hook(ctx Context)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hookUsesContext(parseFunc(t, "package test\n"+tc.source)))
		})
	}
}

func TestOmitTrampolineContext(t *testing.T) {
	before := parseFunc(t, `package test
 func before() (hookContext *Context, skip bool) {
 defer recoverHook()
 hookContext = &Context{}
 hookContext.params = []interface{}{}
 if enabled { hook(hookContext, *arg) }
 return hookContext, hookContext.skip
 }`)
	omitTrampolineContext(before, true)
	assert.Len(t, before.Body.List, 3)
	// Compare the complete remaining body, including the panic guard and typed arg.
	expected := parseFunc(t, `package test
 func before() (hookContext *Context, skip bool) {
 defer recoverHook()
 if enabled { hook(nil, *arg) }
 return nil, false
 }`)
	assert.Equal(t, expected.Body, before.Body)
	after := parseFunc(t, `package test
 func after(hookContext Context) {
 defer recoverHook()
 enabled = true
 hookContext.(*Context).returnVals = []interface{}{}
 if enabled { hook(hookContext, *result) }
 }`)
	omitTrampolineContext(after, false)
	expectedAfter := parseFunc(t, `package test
 func after(hookContext Context) {
 defer recoverHook()
 enabled = true
 if enabled { hook(nil, *result) }
 }`)
	assert.Equal(t, expectedAfter.Body, after.Body)
}
