// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package insttest provides test helpers for instrumentation hook testing.
package hooktest

// MockHookContext is a shared test implementation of hook.HookContext.
// The data field mirrors HookContextImpl
type MockHookContext struct {
	Params      []interface{}
	ReturnVals  []interface{}
	SkipCall    bool
	FuncName    string
	PackageName string
	data        interface{}
}

// NewMockHookContext creates a new MockHookContext with optional initial params.
func NewMockHookContext(params ...interface{}) *MockHookContext {
	return &MockHookContext{
		Params:      params,
		FuncName:    "mockFunc",
		PackageName: "mock",
	}
}

func (m *MockHookContext) SetSkipCall(skip bool) { m.SkipCall = skip }
func (m *MockHookContext) IsSkipCall() bool      { return m.SkipCall }

func (m *MockHookContext) SetData(data interface{}) { m.data = data }
func (m *MockHookContext) GetData() interface{}     { return m.data }

func (m *MockHookContext) SetKeyData(key string, val interface{}) {
	dataMap, ok := m.data.(map[string]interface{})
	if !ok || dataMap == nil {
		dataMap = make(map[string]interface{})
		m.data = dataMap
	}
	dataMap[key] = val
}

func (m *MockHookContext) GetKeyData(key string) interface{} {
	if dataMap, ok := m.data.(map[string]interface{}); ok {
		return dataMap[key]
	}
	return nil
}

func (m *MockHookContext) HasKeyData(key string) bool {
	if dataMap, ok := m.data.(map[string]interface{}); ok {
		_, ok := dataMap[key]
		return ok
	}
	return false
}

func (m *MockHookContext) GetParamCount() int { return len(m.Params) }
func (m *MockHookContext) GetParam(idx int) interface{} {
	if idx < 0 || idx >= len(m.Params) {
		return nil
	}
	return m.Params[idx]
}

func (m *MockHookContext) SetParam(idx int, val interface{}) {
	for len(m.Params) <= idx {
		m.Params = append(m.Params, nil)
	}
	m.Params[idx] = val
}

func (m *MockHookContext) GetReturnValCount() int { return len(m.ReturnVals) }
func (m *MockHookContext) GetReturnVal(idx int) interface{} {
	if idx < 0 || idx >= len(m.ReturnVals) {
		return nil
	}
	return m.ReturnVals[idx]
}

func (m *MockHookContext) SetReturnVal(idx int, val interface{}) {
	for len(m.ReturnVals) <= idx {
		m.ReturnVals = append(m.ReturnVals, nil)
	}
	m.ReturnVals[idx] = val
}

func (m *MockHookContext) GetFuncName() string    { return m.FuncName }
func (m *MockHookContext) GetPackageName() string { return m.PackageName }
