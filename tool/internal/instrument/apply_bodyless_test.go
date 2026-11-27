// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

// bodylessSource declares Asm the way a package does for a routine written in
// assembly: a signature and no body.
const bodylessSource = `package main

//otelc:span
func Asm(x int) int

func Plain() {}
`

func TestApplyFuncRule_BodylessFunctionIsAnError(t *testing.T) {
	root, err := ast.NewAstParser().ParseSource(bodylessSource)
	require.NoError(t, err)

	r := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "hook_asm"},
		Func:         "Asm",
		Before:       "BeforeAsm",
		After:        "AfterAsm",
		Path:         "example.com/hooks",
	}
	funcDecl, found, err := ast.FindFuncDecl(root, r, map[string]string{})
	require.NoError(t, err)
	require.True(t, found)

	modified, err := newTestPhase().applyFuncRule(context.Background(), r, root, funcDecl, found)

	require.Error(t, err)
	assert.False(t, modified)
	assert.Contains(t, err.Error(), `rule "hook_asm" cannot instrument function Asm`)
	assert.Contains(t, err.Error(), "without a body")
}

func TestApplyFuncRule_BodylessFunctionWithIgnoreDirectiveIsSkipped(t *testing.T) {
	root, err := ast.NewAstParser().ParseSource(`package main

//otelc:ignore
func Asm(x int) int
`)
	require.NoError(t, err)

	r := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "hook_asm"},
		Func:         "Asm",
		Before:       "BeforeAsm",
		Path:         "example.com/hooks",
	}
	funcDecl, found, err := ast.FindFuncDecl(root, r, map[string]string{})
	require.NoError(t, err)
	require.True(t, found)

	modified, err := newTestPhase().applyFuncRule(context.Background(), r, root, funcDecl, found)

	require.NoError(t, err)
	assert.False(t, modified)
}

func TestApplyRawRule_BodylessFunctionIsAnError(t *testing.T) {
	root, err := ast.NewAstParser().ParseSource(bodylessSource)
	require.NoError(t, err)

	r := &rule.InstRawRule{
		InstBaseRule: rule.InstBaseRule{Name: "raw_asm"},
		Func:         "Asm",
		Raw:          `println("enter")`,
	}

	err = newTestPhase().applyRawRule(context.Background(), r, root)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `rule "raw_asm" cannot instrument function Asm`)
}

func TestApplyDirectiveRule_BodylessFunctionIsAnError(t *testing.T) {
	root, err := ast.NewAstParser().ParseSource(bodylessSource)
	require.NoError(t, err)

	r := &rule.InstDirectiveRule{
		InstBaseRule: rule.InstBaseRule{Name: "span_directive"},
		Directive:    "otelc:span",
		Template:     `println("span start: {{.FuncName}}")`,
	}

	modified, err := newTestPhase().applyDirectiveRule(context.Background(), r, root)

	require.Error(t, err)
	assert.False(t, modified)
	assert.Contains(t, err.Error(), `rule "span_directive" cannot instrument function Asm`)
}
