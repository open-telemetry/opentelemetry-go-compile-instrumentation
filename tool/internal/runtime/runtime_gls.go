//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// API Stability Policy for runtime_gls.go:
// - Additive-only: New functions can be appended.
// - No hard removals: Never delete existing function signatures
//   (GetTraceContextFromGLS, SetTraceContextToGLS, GetBaggageContainerFromGLS,
//   SetBaggageContainerToGLS, SuppressHooks, UnsuppressHooks, HooksSuppressed).
// - Soft deprecation: If a signature ever needs to change in the future,
//   introduce a V2 variant and keep the old function forever as a forwarding
//   stub (zero overhead, stripped by the linker's dead-code elimination if unused).

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

// OtelContextCloner is an optional interface that objects stored in goroutine-local
// storage (GLS) can implement to customize how their state is cloned when propagated
// to newly spawned goroutines in runtime.newproc1.
//
// When a new goroutine is created, runtime.newproc1 calls propagateGoroutineContext on
// the parent goroutine's otel_trace_context and otel_baggage_container. If the value
// implements OtelContextCloner, Clone() is invoked to create an isolated snapshot for
// the child goroutine. This avoids data races and state pollution between concurrent
// goroutines mutating their respective span or baggage stacks.
type OtelContextCloner interface {
	Clone() interface{}
}

// propagateGoroutineContext clones or copies a GLS value across a goroutine boundary.
func propagateGoroutineContext(val interface{}) interface{} {
	if val == nil {
		return nil
	}
	if cloner, ok := val.(OtelContextCloner); ok {
		return cloner.Clone()
	}
	return val
}
