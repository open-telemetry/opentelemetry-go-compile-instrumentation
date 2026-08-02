// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooktest

import (
	"reflect"
	"testing"
)

type customState struct {
	field string
}

func TestMockHookContext_KeyData_NonMapData(t *testing.T) {
	mock := NewMockHookContext()

	// 1. Initial state (nil data)
	if mock.GetKeyData("foo") != nil {
		t.Errorf("expected GetKeyData to return nil for uninitialized data")
	}
	if mock.HasKeyData("foo") {
		t.Errorf("expected HasKeyData to return false for uninitialized data")
	}

	// 2. Set custom non-map data (struct)
	state := customState{field: "bar"}
	mock.SetData(state)
	if !reflect.DeepEqual(mock.GetData(), state) {
		t.Errorf("expected GetData to return stored customState")
	}

	// Calling GetKeyData / HasKeyData on struct data should not panic
	if mock.GetKeyData("foo") != nil {
		t.Errorf("expected GetKeyData to return nil when data is a struct")
	}
	if mock.HasKeyData("foo") {
		t.Errorf("expected HasKeyData to return false when data is a struct")
	}

	// Calling SetKeyData should gracefully re-initialize data map
	mock.SetKeyData("k1", "v1")
	if !mock.HasKeyData("k1") {
		t.Errorf("expected HasKeyData('k1') to return true after SetKeyData")
	}
	if mock.GetKeyData("k1") != "v1" {
		t.Errorf("expected GetKeyData('k1') to return 'v1'")
	}

	// 3. Set primitive non-map data (string)
	mock.SetData("primitive string")
	if mock.GetKeyData("k1") != nil {
		t.Errorf("expected GetKeyData to return nil when data is a string")
	}
	if mock.HasKeyData("k1") {
		t.Errorf("expected HasKeyData to return false when data is a string")
	}
}
