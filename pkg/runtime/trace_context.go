// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

var traceAndSpanIdFunc = defaultTraceAndSpanId

func defaultTraceAndSpanId() (string, string) {
	return "", ""
}

// GetTraceAndSpanId returns the current trace ID and span ID from GLS.
// Returns empty strings if no trace context is available or OTel SDK trace
// instrumentation is not active.
func GetTraceAndSpanId() (string, string) {
	return traceAndSpanIdFunc()
}

// RegisterTraceAndSpanIdFunc sets the function used to retrieve trace and span IDs.
// Called by the injected OTel SDK trace instrumentation during init.
func RegisterTraceAndSpanIdFunc(f func() (string, string)) {
	traceAndSpanIdFunc = f
}
