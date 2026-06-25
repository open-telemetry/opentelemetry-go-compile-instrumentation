// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultTraceAndSpanId(t *testing.T) {
	traceAndSpanIdFunc = defaultTraceAndSpanId
	traceId, spanId := GetTraceAndSpanId()
	assert.Equal(t, "", traceId)
	assert.Equal(t, "", spanId)
}

func TestRegisterTraceAndSpanIdFunc(t *testing.T) {
	original := traceAndSpanIdFunc
	defer func() { traceAndSpanIdFunc = original }()

	RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "abc123", "def456"
	})

	traceId, spanId := GetTraceAndSpanId()
	assert.Equal(t, "abc123", traceId)
	assert.Equal(t, "def456", spanId)
}

func TestRegisterTraceAndSpanIdFunc_TraceOnly(t *testing.T) {
	original := traceAndSpanIdFunc
	defer func() { traceAndSpanIdFunc = original }()

	RegisterTraceAndSpanIdFunc(func() (string, string) {
		return "abc123", ""
	})

	traceId, spanId := GetTraceAndSpanId()
	assert.Equal(t, "abc123", traceId)
	assert.Equal(t, "", spanId)
}
