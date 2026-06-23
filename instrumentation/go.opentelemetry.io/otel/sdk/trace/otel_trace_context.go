//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace

import (
	"os"
	"runtime"
	"strconv"

	trace "go.opentelemetry.io/otel/trace"
)

const defaultGLSMaxSpans = 1000

var otelGLSMaxSpans = defaultGLSMaxSpans

func init() {
	ms := os.Getenv("OTEL_GLS_MAX_SPANS")
	if ms != "" {
		if parsed, err := strconv.Atoi(ms); err == nil && parsed > 0 {
			otelGLSMaxSpans = parsed
		}
	}
}

type traceContext struct {
	sw  *spanWrapper
	n   int
	lcs trace.Span
}

type spanWrapper struct {
	span trace.Span
	prev *spanWrapper
}

func (tc *traceContext) size() int {
	return tc.n
}

func (tc *traceContext) add(span trace.Span) bool {
	if tc.n > 0 {
		if tc.n >= otelGLSMaxSpans {
			return false
		}
	}
	wrapper := &spanWrapper{span, tc.sw}
	if tc.n == 0 {
		tc.lcs = span
	}
	tc.sw = wrapper
	tc.n++
	return true
}

//go:norace
func (tc *traceContext) tail() trace.Span {
	if tc.n == 0 {
		return nil
	} else {
		return tc.sw.span
	}
}

func (tc *traceContext) localRootSpan() trace.Span {
	if tc.n == 0 {
		return nil
	} else {
		return tc.lcs
	}
}

func (tc *traceContext) del(span trace.Span) {
	if tc.n == 0 {
		return
	}
	addr := &tc.sw
	cur := tc.sw
	for cur != nil {
		sc1 := cur.span.SpanContext()
		sc2 := span.SpanContext()
		if sc1.TraceID() == sc2.TraceID() && sc1.SpanID() == sc2.SpanID() {
			*addr = cur.prev
			tc.n--
			break
		}
		addr = &cur.prev
		cur = cur.prev
	}
}

func (tc *traceContext) clear() {
	tc.sw = nil
	tc.n = 0
	runtime.SetBaggageContainerToGLS(nil)
}

//go:norace
func (tc *traceContext) Clone() interface{} {
	if tc.n == 0 {
		return &traceContext{nil, 0, nil}
	}
	last := tc.tail()
	sw := &spanWrapper{last, nil}
	return &traceContext{sw, 1, nil}
}

func GetTraceContext() trace.SpanContext {
	t := getOrInitTraceContext()
	if t.size() != 0 {
		return t.tail().SpanContext()
	}
	return trace.SpanContext{}
}

func getOrInitTraceContext() *traceContext {
	tc := runtime.GetTraceContextFromGLS()
	if tc == nil {
		newTc := &traceContext{nil, 0, nil}
		setTraceContext(newTc)
		return newTc
	} else {
		return tc.(*traceContext)
	}
}

func setTraceContext(tc *traceContext) {
	runtime.SetTraceContextToGLS(tc)
}

func traceContextAddSpan(span trace.Span) {
	tc := getOrInitTraceContext()
	if tc.add(span) {
		setTraceContext(tc)
	}
}

func GetTraceAndSpanId() (string, string) {
	tc := runtime.GetTraceContextFromGLS()
	if tc == nil || tc.(*traceContext).tail() == nil {
		return "", ""
	}
	ctx := tc.(*traceContext).tail().SpanContext()
	return ctx.TraceID().String(), ctx.SpanID().String()
}

func traceContextDelSpan(span trace.Span) {
	ctx := getOrInitTraceContext()
	ctx.del(span)
}

func ClearTraceContext() {
	getOrInitTraceContext().clear()
}

func spanFromGLS() trace.Span {
	gls := runtime.GetTraceContextFromGLS()
	if gls == nil {
		return nil
	}
	return gls.(*traceContext).tail()
}
