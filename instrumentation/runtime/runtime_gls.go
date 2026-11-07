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

// SuppressHooks turns off Before and After hooks on the current goroutine,
// for calls made while the count stays above zero.
func SuppressHooks() {
	getg().m.curg.otel_ignored_call_count++
}

// UnsuppressHooks reverses one call to SuppressHooks.
func UnsuppressHooks() {
	getg().m.curg.otel_ignored_call_count--
}

// HooksSuppressed reports whether the current goroutine is inside a call
// bracketed by SuppressHooks.
//
// A goroutine started while the count is above zero starts at zero, so a
// hook running on that new goroutine is not suppressed.
func HooksSuppressed() bool {
	return getg().m.curg.otel_ignored_call_count > 0
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
