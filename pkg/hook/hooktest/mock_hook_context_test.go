// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooktest

import (
	"testing"
)

type customState struct {
	name string
}

func TestMockHookContext_KeyDataSafety(t *testing.T) {
	t.Run("nil data map safety", func(t *testing.T) {
		m := NewMockHookContext()
		if m.GetData() != nil {
			t.Errorf("expected GetData() to be nil, got %v", m.GetData())
		}
		if m.HasKeyData("foo") {
			t.Errorf("expected HasKeyData(\"foo\") to be false")
		}
		if m.GetKeyData("foo") != nil {
			t.Errorf("expected GetKeyData(\"foo\") to be nil, got %v", m.GetKeyData("foo"))
		}
	})

	t.Run("non-map data type safety", func(t *testing.T) {
		m := NewMockHookContext()
		m.SetData(&customState{name: "test-state"})

		// Must not panic when data is a non-map type
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("unexpected panic during key data access on non-map data: %v", r)
			}
		}()

		if m.HasKeyData("foo") {
			t.Errorf("expected HasKeyData(\"foo\") to be false for non-map data")
		}
		if m.GetKeyData("foo") != nil {
			t.Errorf("expected GetKeyData(\"foo\") to be nil for non-map data")
		}

		// SetKeyData must safely re-initialize data to a map without panicking
		m.SetKeyData("k1", "v1")

		if !m.HasKeyData("k1") {
			t.Errorf("expected HasKeyData(\"k1\") to be true after SetKeyData")
		}
		if m.GetKeyData("k1") != "v1" {
			t.Errorf("expected GetKeyData(\"k1\") to be \"v1\", got %v", m.GetKeyData("k1"))
		}
	})

	t.Run("normal key-value data operations", func(t *testing.T) {
		m := NewMockHookContext()
		m.SetKeyData("span", "span-1")
		m.SetKeyData("trace", "trace-1")

		if !m.HasKeyData("span") {
			t.Errorf("expected HasKeyData(\"span\") to be true")
		}
		if m.GetKeyData("span") != "span-1" {
			t.Errorf("expected GetKeyData(\"span\") to be \"span-1\", got %v", m.GetKeyData("span"))
		}
		if m.GetKeyData("trace") != "trace-1" {
			t.Errorf("expected GetKeyData(\"trace\") to be \"trace-1\", got %v", m.GetKeyData("trace"))
		}
		if m.HasKeyData("nonexistent") {
			t.Errorf("expected HasKeyData(\"nonexistent\") to be false")
		}
	})
}
