//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace

import (
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	_ "unsafe"

	trace "go.opentelemetry.io/otel/trace"
)

//go:linkname registerTraceAndSpanIDFunc go.opentelemetry.io/otelc/pkg/runtime.RegisterTraceAndSpanIDFunc
func registerTraceAndSpanIDFunc(f func() (string, string))

//go:linkname registerSpanFromGLSFunc go.opentelemetry.io/otelc/pkg/runtime.RegisterSpanFromGLSFunc
func registerSpanFromGLSFunc(f func() trace.Span)

const defaultGLSMaxSpans = 1000

// maxSpanStates bounds lifecycle bookkeeping. Evicted states are marked ended,
// so reaching the limit drops implicit propagation instead of retaining a stale parent.
const maxSpanStates = 100_000

var otelGLSMaxSpans = defaultGLSMaxSpans

func init() {
	ms := os.Getenv("OTEL_GLS_MAX_SPANS")
	if ms != "" {
		if parsed, err := strconv.Atoi(ms); err == nil && parsed > 0 {
			otelGLSMaxSpans = parsed
		}
	}
	registerTraceAndSpanIDFunc(GetTraceAndSpanID)
	registerSpanFromGLSFunc(spanFromGLS)
}

type traceContext struct {
	sw  *spanWrapper
	n   int
	lcs trace.Span
}

type spanWrapper struct {
	span  trace.Span
	prev  *spanWrapper
	ended *atomic.Bool
}

type spanKey struct {
	traceID trace.TraceID
	spanID  trace.SpanID
}

var spanStates = struct {
	sync.Mutex
	states map[spanKey]*atomic.Bool
}{states: make(map[spanKey]*atomic.Bool)}

func stateForSpan(span trace.Span) *atomic.Bool {
	sc := span.SpanContext()
	if !sc.IsValid() {
		return &atomic.Bool{}
	}
	key := spanKey{sc.TraceID(), sc.SpanID()}
	spanStates.Lock()
	defer spanStates.Unlock()
	if state, ok := spanStates.states[key]; ok {
		return state
	}
	if len(spanStates.states) >= maxSpanStates {
		for key, state := range spanStates.states {
			state.Store(true)
			delete(spanStates.states, key)
			break
		}
	}
	state := &atomic.Bool{}
	spanStates.states[key] = state
	return state
}

func markSpanEnded(span trace.Span) {
	sc := span.SpanContext()
	if !sc.IsValid() {
		return
	}
	key := spanKey{sc.TraceID(), sc.SpanID()}
	spanStates.Lock()
	defer spanStates.Unlock()
	if state, ok := spanStates.states[key]; ok {
		state.Store(true)
		delete(spanStates.states, key)
	}
}

func (tc *traceContext) compact() {
	addr := &tc.sw
	for *addr != nil {
		if (*addr).ended.Load() {
			*addr = (*addr).prev
			tc.n--
			continue
		}
		addr = &(*addr).prev
	}
	tc.lcs = nil
	for cur := tc.sw; cur != nil; cur = cur.prev {
		tc.lcs = cur.span
	}
}

func (tc *traceContext) add(span trace.Span) bool {
	if tc.n >= otelGLSMaxSpans {
		tc.compact()
		if tc.n >= otelGLSMaxSpans {
			return false
		}
	}
	wrapper := &spanWrapper{span, tc.sw, stateForSpan(span)}
	if tc.n == 0 {
		tc.lcs = span
	}
	tc.sw = wrapper
	tc.n++
	return true
}

// tail must be called only on the current goroutine's own context: it mutates the
// stack, whose list fields are unsynchronized. Only ended flags are shared.
//
//go:norace
func (tc *traceContext) tail() trace.Span {
	for tc.sw != nil && tc.sw.ended.Load() {
		tc.sw = tc.sw.prev
		tc.n--
	}
	if tc.sw == nil {
		tc.lcs = nil
		return nil
	}
	return tc.sw.span
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
			cur.ended.Store(true)
			*addr = cur.prev
			tc.n--
			if cur.prev == nil {
				tc.lcs = nil
				for remaining := tc.sw; remaining != nil; remaining = remaining.prev {
					tc.lcs = remaining.span
				}
			}
			break
		}
		addr = &cur.prev
		cur = cur.prev
	}
}

func (tc *traceContext) clear() {
	tc.sw = nil
	tc.n = 0
	tc.lcs = nil
	runtime.SetBaggageContainerToGLS(nil)
}

//go:norace
func (tc *traceContext) Clone() interface{} {
	last := tc.tail()
	if last == nil {
		return &traceContext{nil, 0, nil}
	}
	sw := &spanWrapper{last, nil, tc.sw.ended}
	return &traceContext{sw, 1, nil}
}

func GetTraceContext() trace.SpanContext {
	t := getOrInitTraceContext()
	if span := t.tail(); span != nil {
		return span.SpanContext()
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

func GetTraceAndSpanID() (string, string) {
	tc := runtime.GetTraceContextFromGLS()
	if tc == nil {
		return "", ""
	}
	span := tc.(*traceContext).tail()
	if span == nil {
		return "", ""
	}
	ctx := span.SpanContext()
	return ctx.TraceID().String(), ctx.SpanID().String()
}

func traceContextDelSpan(span trace.Span) {
	markSpanEnded(span)
	if ctx := runtime.GetTraceContextFromGLS(); ctx != nil {
		ctx.(*traceContext).del(span)
	}
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
