// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

func GetTraceContextFromGLS() any {
	return getg().m.curg.otel_trace_context
}

func GetBaggageContainerFromGLS() any {
	return getg().m.curg.otel_baggage_container
}

func SetTraceContextToGLS(traceContext any) {
	getg().m.curg.otel_trace_context = traceContext
}

func SetBaggageContainerToGLS(baggageContainer any) {
	getg().m.curg.otel_baggage_container = baggageContainer
}

type OtelContextCloner interface {
	Clone() any
}

func propagateOtelContext(context any) any {
	if context == nil {
		return nil
	}
	if cloner, ok := context.(OtelContextCloner); ok {
		return cloner.Clone()
	}
	return context
}
