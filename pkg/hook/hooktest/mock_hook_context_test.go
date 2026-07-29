// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooktest

import (
	"testing"
)

func TestMockHookContext_SkipCall(t *testing.T) {
	m := NewMockHookContext()
	if m.IsSkipCall() {
		t.Errorf("expected IsSkipCall() to be false, got true")
	}

	m.SetSkipCall(true)
	if !m.IsSkipCall() {
		t.Errorf("expected IsSkipCall() to be true, got false")
	}

	m.SetSkipCall(false)
	if m.IsSkipCall() {
		t.Errorf("expected IsSkipCall() to be false, got true")
	}
}

func TestMockHookContext_Data(t *testing.T) {
	m := NewMockHookContext()
	if m.GetData() != nil {
		t.Errorf("expected GetData() to be nil, got %v", m.GetData())
	}

	m.SetData("test-data")
	if m.GetData() != "test-data" {
		t.Errorf("expected GetData() to be 'test-data', got %v", m.GetData())
	}
}

func TestMockHookContext_KeyData(t *testing.T) {
	m := NewMockHookContext()

	// Initial checks on empty/nil data
	if m.HasKeyData("key1") {
		t.Errorf("expected HasKeyData('key1') to be false, got true")
	}
	if m.GetKeyData("key1") != nil {
		t.Errorf("expected GetKeyData('key1') to be nil, got %v", m.GetKeyData("key1"))
	}

	// Set first key (should initialize internal map)
	m.SetKeyData("key1", "val1")
	if !m.HasKeyData("key1") {
		t.Errorf("expected HasKeyData('key1') to be true, got false")
	}
	if m.GetKeyData("key1") != "val1" {
		t.Errorf("expected GetKeyData('key1') to be 'val1', got %v", m.GetKeyData("key1"))
	}

	// Query non-existent key
	if m.HasKeyData("key2") {
		t.Errorf("expected HasKeyData('key2') to be false, got true")
	}
	if m.GetKeyData("key2") != nil {
		t.Errorf("expected GetKeyData('key2') to be nil, got %v", m.GetKeyData("key2"))
	}

	// Set second key
	m.SetKeyData("key2", 42)
	if !m.HasKeyData("key2") {
		t.Errorf("expected HasKeyData('key2') to be true, got false")
	}
	if m.GetKeyData("key2") != 42 {
		t.Errorf("expected GetKeyData('key2') to be 42, got %v", m.GetKeyData("key2"))
	}
}

func TestMockHookContext_Params(t *testing.T) {
	m := NewMockHookContext("p0", "p1")
	if m.GetParamCount() != 2 {
		t.Errorf("expected GetParamCount() == 2, got %d", m.GetParamCount())
	}
	if m.GetParam(0) != "p0" {
		t.Errorf("expected GetParam(0) == 'p0', got %v", m.GetParam(0))
	}
	if m.GetParam(1) != "p1" {
		t.Errorf("expected GetParam(1) == 'p1', got %v", m.GetParam(1))
	}

	// Out of bounds and negative index
	if m.GetParam(-1) != nil {
		t.Errorf("expected GetParam(-1) == nil, got %v", m.GetParam(-1))
	}
	if m.GetParam(2) != nil {
		t.Errorf("expected GetParam(2) == nil, got %v", m.GetParam(2))
	}

	// Modify existing index
	m.SetParam(0, "p0-modified")
	if m.GetParam(0) != "p0-modified" {
		t.Errorf("expected GetParam(0) == 'p0-modified', got %v", m.GetParam(0))
	}

	// Expand params slice by setting an out-of-bounds index
	m.SetParam(4, "p4")
	if m.GetParamCount() != 5 {
		t.Errorf("expected GetParamCount() == 5, got %d", m.GetParamCount())
	}
	if m.GetParam(4) != "p4" {
		t.Errorf("expected GetParam(4) == 'p4', got %v", m.GetParam(4))
	}
	if m.GetParam(2) != nil {
		t.Errorf("expected GetParam(2) == nil, got %v", m.GetParam(2))
	}
	if m.GetParam(3) != nil {
		t.Errorf("expected GetParam(3) == nil, got %v", m.GetParam(3))
	}
}

func TestMockHookContext_ReturnVals(t *testing.T) {
	m := NewMockHookContext()
	if m.GetReturnValCount() != 0 {
		t.Errorf("expected GetReturnValCount() == 0, got %d", m.GetReturnValCount())
	}
	if m.GetReturnVal(0) != nil {
		t.Errorf("expected GetReturnVal(0) == nil, got %v", m.GetReturnVal(0))
	}
	if m.GetReturnVal(-1) != nil {
		t.Errorf("expected GetReturnVal(-1) == nil, got %v", m.GetReturnVal(-1))
	}

	// Set first return value
	m.SetReturnVal(0, "ret0")
	if m.GetReturnValCount() != 1 {
		t.Errorf("expected GetReturnValCount() == 1, got %d", m.GetReturnValCount())
	}
	if m.GetReturnVal(0) != "ret0" {
		t.Errorf("expected GetReturnVal(0) == 'ret0', got %v", m.GetReturnVal(0))
	}

	// Modify existing index
	m.SetReturnVal(0, "ret0-modified")
	if m.GetReturnVal(0) != "ret0-modified" {
		t.Errorf("expected GetReturnVal(0) == 'ret0-modified', got %v", m.GetReturnVal(0))
	}

	// Expand return values slice by setting an out-of-bounds index
	m.SetReturnVal(3, "ret3")
	if m.GetReturnValCount() != 4 {
		t.Errorf("expected GetReturnValCount() == 4, got %d", m.GetReturnValCount())
	}
	if m.GetReturnVal(3) != "ret3" {
		t.Errorf("expected GetReturnVal(3) == 'ret3', got %v", m.GetReturnVal(3))
	}
	if m.GetReturnVal(1) != nil {
		t.Errorf("expected GetReturnVal(1) == nil, got %v", m.GetReturnVal(1))
	}
	if m.GetReturnVal(2) != nil {
		t.Errorf("expected GetReturnVal(2) == nil, got %v", m.GetReturnVal(2))
	}
}

func TestMockHookContext_Names(t *testing.T) {
	m := NewMockHookContext()
	if m.GetFuncName() != "mockFunc" {
		t.Errorf("expected GetFuncName() == 'mockFunc', got %v", m.GetFuncName())
	}
	if m.GetPackageName() != "mock" {
		t.Errorf("expected GetPackageName() == 'mock', got %v", m.GetPackageName())
	}
}
