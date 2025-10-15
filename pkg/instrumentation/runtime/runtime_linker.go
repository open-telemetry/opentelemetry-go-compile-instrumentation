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

type ContextSnapshoter interface {
	TakeSnapshot() interface{}
}

func propagateOtelContext(context interface{}) interface{} {
	if context == nil {
		return nil
	}
	if snapshoter, ok := context.(ContextSnapshoter); ok {
		return snapshoter.TakeSnapshot()
	}
	return context
}
