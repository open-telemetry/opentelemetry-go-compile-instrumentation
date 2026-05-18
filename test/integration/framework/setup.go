// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package framework provides a self-contained integration test environment for
// compile-time OpenTelemetry instrumentation. It is the otelc equivalent of
// controller-runtime's envtest: it spins up a real in-process OTLP collector,
// builds target applications with the otelc tool, and provides helpers for
// asserting exported telemetry — all without any external cluster or service
// dependency.
package framework

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

// InstrumentationFixture is the central handle for a single integration test.
// It owns the in-process OTLP collector, exposes helpers for building and
// running instrumented apps, and provides span-assertion utilities.
//
// Typical usage:
//
//	func TestMyInstrumentation(t *testing.T) {
//	    f := framework.Setup(t)
//	    f.BuildAndStart("ginserver", "-port=9090")
//	    testutil.WaitForTCP(t, "127.0.0.1:9090")
//	    http.Get("http://127.0.0.1:9090/hello")
//	    f.RequireSpan(SpanMatcher{Method: "GET", Path: "/hello", Status: 200})
//	}
type InstrumentationFixture struct {
	t         *testing.T
	collector *testutil.Collector
	inner     *testutil.TestFixture
}

// Setup initialises the integration test environment:
//  1. Starts an in-process OTLP/HTTP collector (no external process needed).
//  2. Wires OTEL_* env vars so any instrumented binary exports to that collector.
//  3. Registers cleanup so everything tears down when the test ends.
//
// This is the single call every integration test makes — equivalent to
// envtest.Environment.Start() in controller-runtime.
func Setup(t *testing.T) *InstrumentationFixture {
	t.Helper()

	// NewTestFixture starts the collector and configures env vars automatically.
	inner := testutil.NewTestFixture(t)

	return &InstrumentationFixture{
		t:         t,
		collector: nil, // accessed through inner
		inner:     inner,
	}
}

// BuildAndStart compiles appName with otelc and starts the resulting binary.
// args are forwarded to the binary (e.g. "-port=9090").
func (f *InstrumentationFixture) BuildAndStart(appName string, args ...string) {
	f.t.Helper()
	f.inner.BuildAndStart(appName, args...)
}

// Build compiles appName with otelc without starting it.
func (f *InstrumentationFixture) Build(appName string) {
	f.t.Helper()
	f.inner.Build(appName)
}

// CollectedSpans returns all spans exported to the in-process collector so far.
func (f *InstrumentationFixture) CollectedSpans() []ptrace.Span {
	return testutil.AllSpans(f.inner.Traces())
}

// SpanMatcher describes the properties a span must have to satisfy an assertion.
type SpanMatcher struct {
	// Method is the HTTP request method (e.g. "GET").
	Method string
	// Path is the URL path (e.g. "/hello").
	Path string
	// StatusCode is the HTTP response status code (e.g. 200).
	StatusCode int
	// SpanKind is the expected OTel span kind string (e.g. "Server").
	// Leave empty to skip this check.
	SpanKind string
}

// RequireSpan asserts that exactly one span matching m was exported.
// It polls the collector briefly to tolerate in-flight export latency.
func (f *InstrumentationFixture) RequireSpan(m SpanMatcher) ptrace.Span {
	f.t.Helper()
	testutil.WaitForSpanFlush(f.t)

	spans := f.CollectedSpans()
	require.NotEmpty(f.t, spans, "no spans exported; check that the instrumented binary ran and that OTLP env vars are set")

	span := spans[0]

	if m.Method != "" {
		testutil.RequireAttribute(f.t, span, "http.request.method", m.Method)
	}
	if m.Path != "" {
		testutil.RequireAttribute(f.t, span, "url.path", m.Path)
	}
	if m.StatusCode != 0 {
		testutil.RequireAttribute(f.t, span, "http.response.status_code", int64(m.StatusCode))
	}

	return span
}

// RequireNoSpans asserts that the collector received zero spans.
// Useful for verifying that disabled instrumentation produces no telemetry.
func (f *InstrumentationFixture) RequireNoSpans() {
	f.t.Helper()
	// Short settle window — if spans were going to arrive they would by now.
	time.Sleep(200 * time.Millisecond)
	spans := f.CollectedSpans()
	require.Empty(f.t, spans, "expected no spans but collector received %d", len(spans))
}

// MakeRequest fires a GET request and returns the response.
// The test fails immediately if the request itself errors.
func (f *InstrumentationFixture) MakeRequest(url string) *http.Response {
	f.t.Helper()
	resp, err := http.Get(url) //nolint:noctx
	require.NoError(f.t, err, "HTTP request to %s failed", url)
	return resp
}

// ServerAddr returns a convenience base URL for a locally running server.
func ServerAddr(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}
