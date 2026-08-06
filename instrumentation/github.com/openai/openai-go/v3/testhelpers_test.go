// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

// Helper functions for attribute assertions.

func assertAttribute(t *testing.T, attrs []attribute.KeyValue, key, expected string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			if attr.Value.Type() == attribute.BOOL {
				assert.Equal(t, expected, boolString(attr.Value.AsBool()))
				return
			}
			assert.Equal(t, expected, attr.Value.AsString())
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func assertInt64Attribute(t *testing.T, attrs []attribute.KeyValue, key string, expected int64) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			assert.Equal(t, expected, attr.Value.AsInt64())
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func assertFloat64Attribute(t *testing.T, attrs []attribute.KeyValue, key string, expected float64) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			assert.InDelta(t, expected, attr.Value.AsFloat64(), 0.001)
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}

func assertSliceAttribute(t *testing.T, attrs []attribute.KeyValue, key string, expected []string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			assert.Equal(t, expected, attr.Value.AsStringSlice())
			return
		}
	}
	t.Errorf("attribute %q not found", key)
}
