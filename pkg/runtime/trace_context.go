// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

var traceAndSpanIdFunc = defaultTraceAndSpanId

func defaultTraceAndSpanId() (string, string) {
	return "", ""
}

func GetTraceAndSpanId() (string, string) {
	return traceAndSpanIdFunc()
}

func RegisterTraceAndSpanIdFunc(f func() (string, string)) {
	traceAndSpanIdFunc = f
}
