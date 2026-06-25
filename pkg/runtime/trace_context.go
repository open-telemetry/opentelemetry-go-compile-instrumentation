// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

var traceAndSpanIDFunc = defaultTraceAndSpanID

func defaultTraceAndSpanID() (string, string) {
	return "", ""
}

func GetTraceAndSpanID() (string, string) {
	return traceAndSpanIDFunc()
}

func RegisterTraceAndSpanIDFunc(f func() (string, string)) {
	traceAndSpanIDFunc = f
}
