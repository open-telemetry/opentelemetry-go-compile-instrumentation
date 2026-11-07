// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
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

func TestRuleUsesContextChecksEveryPlatformDefinition(t *testing.T) {
	const unused = "package hooks\n\nimport \"go.opentelemetry.io/otelc/pkg/hook\"\n\n" +
		"func Before(ctx hook.HookContext, x int) {}\n"
	const used = "package hooks\n\nimport \"go.opentelemetry.io/otelc/pkg/hook\"\n\n" +
		"func Before(ctx hook.HookContext, x int) { ctx.SetParam(0, x+1) }\n"
	tests := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"one unused definition", map[string]string{"hooks.go": unused}, false},
		{
			"another platform uses the context",
			map[string]string{"a_windows.go": "//go:build windows\n\n" + unused, "z_linux.go": "//go:build linux\n\n" + used},
			true,
		},
		{
			"every platform leaves it unused",
			map[string]string{"a_windows.go": "//go:build windows\n\n" + unused, "z_linux.go": "//go:build linux\n\n" + unused},
			false,
		},
		{"hook not found", map[string]string{"other.go": "package hooks\n"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, src := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
			}
			ip := newTestPhase()
			r := &rule.InstFuncRule{Before: "Before", Path: "example.com/hooks", ResolvedPath: dir}
			got, err := ip.ruleUsesContext(r)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
