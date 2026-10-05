//go:build ignore

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package trace

import (
	"container/list"
	"log/slog"
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

// otelcLogger is reached by linkname, like the register funcs above, rather than
// by importing pkg/runtime: that package imports go.opentelemetry.io/otel/sdk/trace,
// so an import edge from this injected file closes a cycle and the build fails at
// link time with a pkg/runtime fingerprint mismatch.
//
// Call it at each use site rather than caching it in a package var. Without an
// import edge Go does not order pkg/runtime's init() before this package's, so a
// var initializer would run first, latch the slog.Default() fallback, and silently
// discard OTEL_LOG_LEVEL=debug output.
//
//go:linkname otelcLogger go.opentelemetry.io/otelc/pkg/runtime.Logger
func otelcLogger() *slog.Logger

const defaultGLSMaxSpans = 1000

// defaultMaxSpanStates bounds lifecycle bookkeeping. Evicted states are marked
// ended, so reaching the limit drops implicit propagation for the evicted span
// instead of retaining a stale parent for it.
const defaultMaxSpanStates = 100_000

var (
	otelGLSMaxSpans = defaultGLSMaxSpans
	maxSpanStates   = defaultMaxSpanStates
)

func init() {
	if parsed, ok := positiveIntEnv("OTEL_GLS_MAX_SPANS"); ok {
		otelGLSMaxSpans = parsed
	}
	if parsed, ok := positiveIntEnv("OTEL_GLS_MAX_SPAN_STATES"); ok {
		maxSpanStates = parsed
	}
	registerTraceAndSpanIDFunc(GetTraceAndSpanID)
	registerSpanFromGLSFunc(spanFromGLS)
}

func positiveIntEnv(name string) (int, bool) {
	v := os.Getenv(name)
	if v == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(v)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
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

// spanStateEntry pairs a span's ended flag with its position in the
// insertion-order eviction list.
type spanStateEntry struct {
	state *atomic.Bool
	elem  *list.Element
}

// spanStates bounds shared lifecycle bookkeeping across goroutines. order
// tracks insertion order (oldest at the front) so that once the map hits
// maxSpanStates, eviction always drops the oldest entry rather than an
// arbitrary one — Go map iteration order is randomized, so iterating the map
// itself to pick a victim can evict a span that is still genuinely in flight
// on an unrelated goroutine.
var spanStates = struct {
	sync.Mutex
	states map[spanKey]*spanStateEntry
	order  *list.List
}{
	states: make(map[spanKey]*spanStateEntry),
	order:  list.New(),
}

// evictOldestLocked drops the oldest tracked span state, marking it ended so
// any goroutine still holding it stops treating it as a live parent. Callers
// must hold spanStates.Mutex.
func evictOldestLocked() {
	front := spanStates.order.Front()
	if front == nil {
		return
	}
	key, _ := front.Value.(spanKey)
	if entry, ok := spanStates.states[key]; ok {
		entry.state.Store(true)
		delete(spanStates.states, key)
	}
	spanStates.order.Remove(front)
	otelcLogger().Debug("GLS span state tracker at capacity, evicting oldest entry",
		"limit", maxSpanStates)
}

func stateForSpan(span trace.Span) *atomic.Bool {
	sc := span.SpanContext()
	if !sc.IsValid() {
		return &atomic.Bool{}
	}
	key := spanKey{sc.TraceID(), sc.SpanID()}
	spanStates.Lock()
	defer spanStates.Unlock()
	if entry, ok := spanStates.states[key]; ok {
		return entry.state
	}
	if len(spanStates.states) >= maxSpanStates {
		evictOldestLocked()
	}
	state := &atomic.Bool{}
	elem := spanStates.order.PushBack(key)
	spanStates.states[key] = &spanStateEntry{state: state, elem: elem}
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
	if entry, ok := spanStates.states[key]; ok {
		entry.state.Store(true)
		delete(spanStates.states, key)
		spanStates.order.Remove(entry.elem)
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
			// The new span is dropped from GLS: it won't be reachable via
			// implicit propagation, and any span.SpanFromContext(context.Background())
			// lookup on this goroutine keeps returning whatever was already
			// on top of the stack until it compacts below the limit. Surface
			// that instead of dropping it silently.
			otelcLogger().Debug("GLS span stack at capacity, span not tracked for implicit propagation",
				"limit", otelGLSMaxSpans)
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
