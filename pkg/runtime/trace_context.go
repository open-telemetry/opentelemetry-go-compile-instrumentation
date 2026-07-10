// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"go.opentelemetry.io/otel/trace"
)

var (
	traceAndSpanIDFunc = defaultTraceAndSpanID
	spanFromGLSFunc    = defaultSpanFromGLS
)

func defaultTraceAndSpanID() (string, string) {
	return "", ""
}

func defaultSpanFromGLS() trace.Span {
	return nil
}

func GetTraceAndSpanID() (string, string) {
	return traceAndSpanIDFunc()
}

func RegisterTraceAndSpanIDFunc(f func() (string, string)) {
	traceAndSpanIDFunc = f
}

func GetSpanFromGLS() trace.Span {
	return spanFromGLSFunc()
}

func RegisterSpanFromGLSFunc(f func() trace.Span) {
	spanFromGLSFunc = f
}
