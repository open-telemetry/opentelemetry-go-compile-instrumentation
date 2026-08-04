// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package main provides a test application that verifies OTel SDK instrumentation
// works correctly with GLS-based span propagation. It starts an HTTP server,
// sends a request to itself, and inside the handler verifies that
// trace.SpanFromContext(context.Background()) returns a valid span from GLS.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var port = flag.String("port", "8989", "The server port")

var (
	workerTasks = make(chan func())
	startWorker sync.Once
	spanToEnd   = make(chan trace.Span)
	spanEnded   = make(chan struct{})
)

// submit runs task on a single long-lived worker goroutine, created lazily on
// first use. The goroutine inherits a GLS clone from whoever calls submit
// first (the /otel handler, while its span is active), and that clone is then
// reused across requests. This models real-world worker pools, where a
// goroutine outlives the request whose GLS it inherited.
func submit(task func()) {
	startWorker.Do(func() {
		go func() {
			for task := range workerTasks {
				task()
			}
		}()
	})
	workerTasks <- task
}

// otelHandler verifies cross-goroutine propagation: the worker's GLS clone
// snapshots the active span, so SpanFromContext must return it in the task.
// The span is ended on a separate goroutine (see main) so it is only flagged
// ended, never popped from the worker's clone.
func otelHandler(w http.ResponseWriter, r *http.Request) {
	_, span := otel.Tracer("handler").Start(context.Background(), "cross-goroutine-span")
	done := make(chan struct{})
	submit(func() {
		defer close(done)
		span := trace.SpanFromContext(context.Background())
		sc := span.SpanContext()

		if sc.IsValid() {
			fmt.Printf("OTEL_SDK_TEST: span valid, traceID=%s spanID=%s\n",
				sc.TraceID().String(), sc.SpanID().String())
		} else {
			fmt.Println("OTEL_SDK_TEST: span invalid")
		}
	})
	<-done
	spanToEnd <- span
	<-spanEnded

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// workerHandler reuses the worker after the /otel span ended. Its GLS clone
// still holds that span, so this checks SpanFromContext skips ended entries
// (stale span=false) and worker-span starts as root, not as its child.
func workerHandler(w http.ResponseWriter, r *http.Request) {
	done := make(chan struct{})
	submit(func() {
		defer close(done)
		stale := trace.SpanFromContext(context.Background()).SpanContext().IsValid()
		fmt.Printf("OTEL_SDK_WORKER: stale span=%t\n", stale)
		_, span := otel.Tracer("worker").Start(context.Background(), "worker-span")
		span.End()
	})
	<-done

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// compactHandler verifies ended spans below the active span don't consume the
// GLS limit. With OTEL_GLS_MAX_SPANS=3 the stack [server-span, lower, top] is
// full; ending lower on another goroutine only flags it, so replacement-span
// hits the limit and must compact the stack to be admitted (admitted=true).
func compactHandler(w http.ResponseWriter, r *http.Request) {
	_, lower := otel.Tracer("compact").Start(context.Background(), "lower-span")
	_, top := otel.Tracer("compact").Start(context.Background(), "top-span")
	done := make(chan struct{})
	go func() {
		lower.End()
		close(done)
	}()
	<-done

	_, replacement := otel.Tracer("compact").Start(context.Background(), "replacement-span")
	active := trace.SpanFromContext(context.Background()).SpanContext()
	fmt.Printf("OTEL_SDK_COMPACT: admitted=%t\n", active.SpanID() == replacement.SpanContext().SpanID())
	replacement.End()
	top.End()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// liveCapHandler exercises the per-goroutine OTEL_GLS_MAX_SPANS cap when the
// stack is full of genuinely live (unended) spans, unlike compactHandler
// where ended spans below the top get compacted away to make room.
//
// The handler goroutine does not start empty: the net/http server
// instrumentation has already pushed its own span for this request. So the cap
// has to be OTEL_GLS_MAX_SPANS=2 for this scenario to be about "live-second" —
// the server span plus "live-first" fill the stack, and "live-second" is the
// one that can never be admitted. Implicit lookups must then keep returning
// "live-first" (stale but observable) rather than something undefined, and the
// drop must be logged instead of silent.
//
// At OTEL_GLS_MAX_SPANS=1 the server span alone already fills the stack, so
// both spans below are dropped and the lookup returns the server span instead.
func liveCapHandler(w http.ResponseWriter, r *http.Request) {
	_, first := otel.Tracer("livecap").Start(context.Background(), "live-first")
	_, second := otel.Tracer("livecap").Start(context.Background(), "live-second")

	active := trace.SpanFromContext(context.Background())
	staleParent := active.SpanContext().IsValid() && active.SpanContext().SpanID() == first.SpanContext().SpanID()
	fmt.Printf("OTEL_SDK_LIVECAP: stale-parent-is-first=%t\n", staleParent)

	second.End()
	first.End()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// evictionHandler exercises the shared spanStates eviction bound
// (OTEL_GLS_MAX_SPAN_STATES=2). "span-a" is started first on this goroutine
// and deliberately never ended here, standing in for a span that is still
// genuinely in flight elsewhere. "span-b" and "span-c" are then started on a
// second goroutine, which pushes the shared state map over the limit and
// forces an eviction. Eviction must always drop the oldest entry
// ("span-a") deterministically; it must never touch "span-b", which is
// younger and still live.
func evictionHandler(w http.ResponseWriter, r *http.Request) {
	_, spanA := otel.Tracer("eviction").Start(context.Background(), "span-a")
	spanAValidBefore := trace.SpanFromContext(context.Background()).SpanContext().IsValid()
	fmt.Printf("OTEL_SDK_EVICT: span-a valid before eviction=%t\n", spanAValidBefore && spanA.SpanContext().IsValid())

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, spanB := otel.Tracer("eviction").Start(context.Background(), "span-b")
		_, spanC := otel.Tracer("eviction").Start(context.Background(), "span-c")
		spanC.End()

		// span-c ended and was spliced out, so the local stack's tail is
		// span-b, unless span-b was wrongly evicted alongside span-a.
		survivor := trace.SpanFromContext(context.Background())
		spanBSurvived := survivor.SpanContext().IsValid() && survivor.SpanContext().SpanID() == spanB.SpanContext().SpanID()
		fmt.Printf("OTEL_SDK_EVICT: span-b survived=%t\n", spanBSurvived)
		spanB.End()
	}()
	<-done

	// span-a was never ended by this handler, but the eviction triggered by
	// span-b/span-c above should have marked it ended for every goroutine
	// still holding it, including this one.
	afterEviction := trace.SpanFromContext(context.Background())
	fmt.Printf("OTEL_SDK_EVICT: span-a evicted=%t\n", !afterEviction.SpanContext().IsValid())

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// requestedPaths returns the endpoints to exercise, controlled by the
// OTELSDK_PATHS env var (comma-separated). It defaults to the original
// scenario set. livecap and evict tune OTEL_GLS_MAX_SPANS /
// OTEL_GLS_MAX_SPAN_STATES to values that would break the other scenarios in
// the same process, so tests that need them run this binary with a separate
// OTELSDK_PATHS selection instead of mixing them into one run.
func requestedPaths() []string {
	if v := os.Getenv("OTELSDK_PATHS"); v != "" {
		return strings.Split(v, ",")
	}
	return []string{"otel", "worker", "compact"}
}

func main() {
	flag.Parse()
	addr := fmt.Sprintf(":%s", *port)

	// Each endpoint exercises one GLS span-lifecycle scenario; the order in
	// which they are requested below matters (/worker checks the aftermath of
	// /otel). See the doc comment on each handler for details.
	http.HandleFunc("/otel", otelHandler)       // cross-goroutine propagation via GLS clone
	http.HandleFunc("/worker", workerHandler)   // ended span is invisible on a reused goroutine
	http.HandleFunc("/compact", compactHandler) // ended spans don't count against OTEL_GLS_MAX_SPANS
	http.HandleFunc("/livecap", liveCapHandler) // live spans past OTEL_GLS_MAX_SPANS are dropped, not admitted
	http.HandleFunc("/evict", evictionHandler)  // spanStates eviction always drops the oldest entry

	// End the /otel span on a goroutine that never held it in GLS, so removal
	// happens only through the shared ended flag (see otelHandler).
	go func() {
		span := <-spanToEnd
		span.End()
		close(spanEnded)
	}()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	go func() {
		if err := http.Serve(ln, nil); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	for _, path := range requestedPaths() {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s/%s", *port, path))
		if err != nil {
			log.Fatalf("request to %s failed: %v", path, err)
		}
		resp.Body.Close()
	}

	// Give time for span export
	time.Sleep(1 * time.Second)
}
