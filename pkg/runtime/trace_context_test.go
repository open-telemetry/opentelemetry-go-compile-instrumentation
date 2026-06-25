// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultTraceAndSpanID(t *testing.T) {
	traceAndSpanIDFunc = defaultTraceAndSpanID
	traceID, spanID := GetTraceAndSpanID()
	assert.Equal(t, "", traceID)
	assert.Equal(t, "", spanID)
}

func TestRegisterTraceAndSpanIDFunc(t *testing.T) {
	original := traceAndSpanIDFunc
	defer func() { traceAndSpanIDFunc = original }()

	RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123", "def456"
	})

	traceID, spanID := GetTraceAndSpanID()
	assert.Equal(t, "abc123", traceID)
	assert.Equal(t, "def456", spanID)
}

func TestRegisterTraceAndSpanIDFunc_TraceOnly(t *testing.T) {
	original := traceAndSpanIDFunc
	defer func() { traceAndSpanIDFunc = original }()

	RegisterTraceAndSpanIDFunc(func() (string, string) {
		return "abc123", ""
	})

	traceID, spanID := GetTraceAndSpanID()
	assert.Equal(t, "abc123", traceID)
	assert.Equal(t, "", spanID)
}
