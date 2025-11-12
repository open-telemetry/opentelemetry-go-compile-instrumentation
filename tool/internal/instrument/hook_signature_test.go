// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
)

// Test the exact HTTP server hook scenario
func TestHTTPServerHookSignature(t *testing.T) {
	// Target function: func (sh *serverHandler) ServeHTTP(rw ResponseWriter, req *Request)
	targetCode := `
package http

type serverHandler struct{}
type ResponseWriter interface{}
type Request struct{}

func (sh *serverHandler) ServeHTTP(rw ResponseWriter, req *Request) {
	// implementation
}
`

	// Before hook: func BeforeServeHTTP(ictx HookContext, recv interface{}, w ResponseWriter, r *Request)
	hookBeforeCode := `
package nethttp

type HookContext interface{}
type ResponseWriter interface{}
type Request struct{}

func BeforeServeHTTP(ictx HookContext, recv interface{}, w ResponseWriter, r *Request) {
	// implementation
}
`

	// After hook: func AfterServeHTTP(ictx HookContext)
	hookAfterCode := `
package nethttp

type HookContext interface{}

func AfterServeHTTP(ictx HookContext) {
	// implementation
}
`

	// Parse target function
	parser := ast.NewAstParser()
	targetAST, err := parser.ParseSource(targetCode)
	require.NoError(t, err)
	targetFunc := ast.FindFuncDecl(targetAST, "ServeHTTP", "*serverHandler")
	require.NotNil(t, targetFunc)

	// Parse before hook
	beforeAST, err := parser.ParseSource(hookBeforeCode)
	require.NoError(t, err)
	beforeFunc := ast.FindFuncDeclWithoutRecv(beforeAST, "BeforeServeHTTP")
	require.NotNil(t, beforeFunc)

	// Parse after hook
	afterAST, err := parser.ParseSource(hookAfterCode)
	require.NoError(t, err)
	afterFunc := ast.FindFuncDeclWithoutRecv(afterAST, "AfterServeHTTP")
	require.NotNil(t, afterFunc)

	// Test: Extract traits from before hook
	t.Run("before_hook_traits", func(t *testing.T) {
		// Before hook has 4 params: HookContext, interface{}, ResponseWriter, Request
		require.Len(t, beforeFunc.Type.Params.List, 4)

		// Expected traits: [HookContext, interface{}, ResponseWriter, Request]
		// Trait[1] (recv) should be IsInterfaceAny=true
		assert.Equal(t, "ictx", beforeFunc.Type.Params.List[0].Names[0].Name)
		assert.Equal(t, "recv", beforeFunc.Type.Params.List[1].Names[0].Name)
		assert.Equal(t, "w", beforeFunc.Type.Params.List[2].Names[0].Name)
		assert.Equal(t, "r", beforeFunc.Type.Params.List[3].Names[0].Name)

		// Check if interface{} is detected
		isInterface := ast.IsInterfaceType(beforeFunc.Type.Params.List[1].Type)
		assert.True(t, isInterface, "recv parameter should be interface{}")
	})

	// Test: Extract traits from after hook
	t.Run("after_hook_traits", func(t *testing.T) {
		// After hook has 1 param: HookContext only
		require.Len(t, afterFunc.Type.Params.List, 1)
		assert.Equal(t, "ictx", afterFunc.Type.Params.List[0].Names[0].Name)
	})

	// Test: Build trampoline type for before hook
	t.Run("before_trampoline_params", func(t *testing.T) {
		ip := &InstrumentPhase{targetFunc: targetFunc}
		trampolineParams := ip.buildTrampolineType(true)

		// Should have receiver + 2 params = 3 total
		// receiver (*serverHandler), param0 (ResponseWriter), param1 (*Request)
		assert.Len(t, trampolineParams.List, 3,
			"before trampoline should have receiver + params")
	})

	// Test: Build trampoline type for after hook
	t.Run("after_trampoline_params", func(t *testing.T) {
		ip := &InstrumentPhase{targetFunc: targetFunc}
		trampolineParams := ip.buildTrampolineType(false)

		// ServeHTTP has no return values, so after trampoline should be empty
		assert.Empty(t, trampolineParams.List,
			"after trampoline for void function should have no params (only HookContext added later)")
	})

	// Test: Trampoline params match hook params after addHookContext
	t.Run("linkname_signature_for_before", func(t *testing.T) {
		ip := &InstrumentPhase{targetFunc: targetFunc}
		trampolineParams := ip.buildTrampolineType(true)
		addHookContext(trampolineParams)

		// After addHookContext: HookContext + receiver + param0 + param1 = 4 params
		assert.Len(t, trampolineParams.List, 4)

		// This should match beforeFunc which has 4 params
		assert.Len(t, beforeFunc.Type.Params.List, len(trampolineParams.List),
			"linkname signature should match hook function signature")
	})

	t.Run("linkname_signature_for_after", func(t *testing.T) {
		ip := &InstrumentPhase{targetFunc: targetFunc}
		trampolineParams := ip.buildTrampolineType(false)
		addHookContext(trampolineParams)

		// After addHookContext: HookContext only = 1 param
		assert.Len(t, trampolineParams.List, 1)

		// This should match afterFunc which has 1 param
		assert.Len(t, afterFunc.Type.Params.List, len(trampolineParams.List),
			"linkname signature should match hook function signature")
	})
}

// Test what happens when target has return values
func TestAfterHookWithReturnValues(t *testing.T) {
	targetCode := `
package foo

func DoSomething() (int, error) {
	return 0, nil
}
`

	hookAfterCode := `
package hooks

type HookContext interface{}

func AfterDoSomething(ictx HookContext, ret0 int, ret1 error) {
	// implementation
}
`

	parser := ast.NewAstParser()
	targetAST, err := parser.ParseSource(targetCode)
	require.NoError(t, err)
	targetFunc := ast.FindFuncDeclWithoutRecv(targetAST, "DoSomething")
	require.NotNil(t, targetFunc)

	afterAST, err := parser.ParseSource(hookAfterCode)
	require.NoError(t, err)
	afterFunc := ast.FindFuncDeclWithoutRecv(afterAST, "AfterDoSomething")
	require.NotNil(t, afterFunc)

	t.Run("after_hook_with_returns", func(t *testing.T) {
		// After hook has 3 params: HookContext + 2 return values
		require.Len(t, afterFunc.Type.Params.List, 3)
	})

	t.Run("after_trampoline_with_returns", func(t *testing.T) {
		ip := &InstrumentPhase{targetFunc: targetFunc}
		trampolineParams := ip.buildTrampolineType(false)

		// Should have 2 return values
		assert.Len(t, trampolineParams.List, 2)

		addHookContext(trampolineParams)
		// After addHookContext: 3 params (HookContext + 2 returns)
		assert.Len(t, trampolineParams.List, 3)

		// Should match hook signature
		assert.Len(t, afterFunc.Type.Params.List, len(trampolineParams.List))
	})
}
