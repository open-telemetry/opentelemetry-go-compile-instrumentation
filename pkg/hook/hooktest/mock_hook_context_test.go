// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooktest_test

import (
	"testing"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func TestMockHookContext_ConstructorAndGetters(t *testing.T) {
	ctx := hooktest.NewMockHookContext("arg1", "arg2")

	if ctx.GetPackageName() != "mock" {
		t.Errorf("expected GetPackageName() to be 'mock', got %s", ctx.GetPackageName())
	}
	if ctx.GetFuncName() != "mockFunc" {
		t.Errorf("expected GetFuncName() to be 'mockFunc', got %s", ctx.GetFuncName())
	}
	if ctx.GetParamCount() != 2 {
		t.Errorf("expected initial ParamCount to be 2, got %d", ctx.GetParamCount())
	}
	if ctx.GetParam(0) != "arg1" || ctx.GetParam(1) != "arg2" {
		t.Errorf("unexpected initial parameters")
	}

	// SkipCall flags
	if ctx.IsSkipCall() {
		t.Error("expected IsSkipCall() to default to false")
	}
	ctx.SetSkipCall(true)
	if !ctx.IsSkipCall() {
		t.Error("expected IsSkipCall() to be true after SetSkipCall(true)")
	}
}

func TestMockHookContext_GenericData(t *testing.T) {
	ctx := hooktest.NewMockHookContext()

	if ctx.GetData() != nil {
		t.Errorf("expected initial GetData() to be nil, got %v", ctx.GetData())
	}

	ctx.SetData("test-state")
	if ctx.GetData() != "test-state" {
		t.Errorf("expected GetData() to return 'test-state', got %v", ctx.GetData())
	}
}

func TestMockHookContext_KeyDataOperations(t *testing.T) {
	ctx := hooktest.NewMockHookContext()

	// Lookups on nil/uninitialized map
	if ctx.HasKeyData("missingKey") {
		t.Error("expected HasKeyData('missingKey') to be false on nil map")
	}
	if ctx.GetKeyData("missingKey") != nil {
		t.Errorf("expected GetKeyData('missingKey') to be nil, got %v", ctx.GetKeyData("missingKey"))
	}

	// Setting and retrieving key-data (initializes map)
	ctx.SetKeyData("userKey", 12345)
	if !ctx.HasKeyData("userKey") {
		t.Error("expected HasKeyData('userKey') to be true after SetKeyData")
	}
	if ctx.GetKeyData("userKey") != 12345 {
		t.Errorf("expected GetKeyData('userKey') to be 12345, got %v", ctx.GetKeyData("userKey"))
	}
}

func TestMockHookContext_ParamOperationsAndBounds(t *testing.T) {
	ctx := hooktest.NewMockHookContext()

	if ctx.GetParamCount() != 0 {
		t.Errorf("expected initial GetParamCount() to be 0, got %d", ctx.GetParamCount())
	}

	// Out-of-bounds and negative index lookups
	if ctx.GetParam(-1) != nil {
		t.Errorf("expected GetParam(-1) to be nil")
	}
	if ctx.GetParam(0) != nil {
		t.Errorf("expected GetParam(0) to be nil")
	}
	if ctx.GetParam(5) != nil {
		t.Errorf("expected GetParam(5) to be nil")
	}

	// Setting param with slice auto-expansion
	ctx.SetParam(2, "paramValueAtIdx2")
	if ctx.GetParamCount() != 3 {
		t.Errorf("expected GetParamCount() to be 3 after setting index 2, got %d", ctx.GetParamCount())
	}
	if ctx.GetParam(2) != "paramValueAtIdx2" {
		t.Errorf("expected GetParam(2) to return 'paramValueAtIdx2', got %v", ctx.GetParam(2))
	}

	// Padded elements should be nil
	if ctx.GetParam(0) != nil || ctx.GetParam(1) != nil {
		t.Error("expected padded param elements at index 0 and 1 to be nil")
	}
}

func TestMockHookContext_ReturnValOperationsAndBounds(t *testing.T) {
	ctx := hooktest.NewMockHookContext()

	if ctx.GetReturnValCount() != 0 {
		t.Errorf("expected initial GetReturnValCount() to be 0, got %d", ctx.GetReturnValCount())
	}

	// Out-of-bounds and negative index lookups
	if ctx.GetReturnVal(-1) != nil {
		t.Errorf("expected GetReturnVal(-1) to be nil")
	}
	if ctx.GetReturnVal(0) != nil {
		t.Errorf("expected GetReturnVal(0) to be nil")
	}
	if ctx.GetReturnVal(5) != nil {
		t.Errorf("expected GetReturnVal(5) to be nil")
	}

	// Setting return value with slice auto-expansion
	ctx.SetReturnVal(1, "returnValueAtIdx1")
	if ctx.GetReturnValCount() != 2 {
		t.Errorf("expected GetReturnValCount() to be 2 after setting index 1, got %d", ctx.GetReturnValCount())
	}
	if ctx.GetReturnVal(1) != "returnValueAtIdx1" {
		t.Errorf("expected GetReturnVal(1) to return 'returnValueAtIdx1', got %v", ctx.GetReturnVal(1))
	}

	// Padded elements should be nil
	if ctx.GetReturnVal(0) != nil {
		t.Error("expected padded return element at index 0 to be nil")
	}
}
