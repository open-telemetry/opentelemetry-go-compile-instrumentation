//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

func GetTraceContextFromGLS() interface{} {
	return getg().m.curg.otel_trace_context
}

func GetBaggageContainerFromGLS() interface{} {
	return getg().m.curg.otel_baggage_container
}

func SetTraceContextToGLS(traceContext interface{}) {
	getg().m.curg.otel_trace_context = traceContext
}

func SetBaggageContainerToGLS(baggageContainer interface{}) {
	getg().m.curg.otel_baggage_container = baggageContainer
}

func IncrementSuppressCount() {
	getg().m.curg.otel_suppress_count++
}

func DecrementSuppressCount() {
	getg().m.curg.otel_suppress_count--
}

// IsSuppressed reports whether the current goroutine is inside an excluded
// call.
//
// The count does not survive a new goroutine. A goroutine started while the
// count is greater than zero starts at zero, so a hook running on that
// goroutine is not suppressed.
func IsSuppressed() bool {
	return getg().m.curg.otel_suppress_count > 0
}

type OtelContextCloner interface {
	Clone() interface{}
}

func propagateOtelContext(context interface{}) interface{} {
	if context == nil {
		return nil
	}
	if cloner, ok := context.(OtelContextCloner); ok {
		return cloner.Clone()
	}
	return context
}
