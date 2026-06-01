// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package framework_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/integration/framework"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

// TestGinBootstrap verifies that otelc can build a Gin application and that
// the resulting binary exports at least one span to the in-process collector.
// This is the "does the whole pipeline work at all?" smoke test.
func TestGinBootstrap(t *testing.T) {
	f := framework.Setup(t)

	const port = 9080
	f.BuildAndStart("ginserver", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	resp := f.MakeRequest(framework.ServerAddr(port) + "/hello?name=world")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	spans := f.CollectedSpans()
	testutil.WaitForSpanFlush(t)
	assert.NotEmpty(t, spans, "otelc bootstrap: instrumented binary must export spans")
}

// TestGinServerSpanAttributes verifies that a GET /hello span carries all
// required HTTP server semantic-convention attributes.
func TestGinServerSpanAttributes(t *testing.T) {
	f := framework.Setup(t)

	const port = 9081
	f.BuildAndStart("ginserver", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	resp := f.MakeRequest(framework.ServerAddr(port) + "/hello?name=otel")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	span := f.RequireSpan(framework.SpanMatcher{
		Method:     http.MethodGet,
		Path:       "/hello",
		StatusCode: http.StatusOK,
	})

	// Validate every required HTTP server semconv attribute.
	testutil.RequireHTTPServerSemconv(
		t,
		span,
		http.MethodGet,
		"/hello",
		"http",
		int64(http.StatusOK),
		int64(port),
		"127.0.0.1",
		"Go-http-client/1.1",
		"1.1",
		"127.0.0.1",
	)
}

// TestGinTracePropagation verifies that an incoming W3C traceparent header is
// extracted and the new server span is linked to the parent trace.
func TestGinTracePropagation(t *testing.T) {
	f := framework.Setup(t)

	const port = 9082
	f.BuildAndStart("ginserver", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	// Send a request with a pre-existing traceparent.
	parentTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	parentSpanID := "00f067aa0bb902b7"
	traceparent := fmt.Sprintf("00-%s-%s-01", parentTraceID, parentSpanID)

	req, err := http.NewRequest(http.MethodGet, framework.ServerAddr(port)+"/hello", nil)
	require.NoError(t, err)
	req.Header.Set("traceparent", traceparent)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	testutil.WaitForSpanFlush(t)
	spans := f.CollectedSpans()
	require.NotEmpty(t, spans)

	span := spans[0]
	// The exported span must share the same trace ID as the injected parent.
	assert.Equal(t, parentTraceID, span.TraceID().String(),
		"server span must be part of the incoming trace")
}

// TestGinErrorSpanStatus verifies that a 500 response causes the span status
// to be set to Error per the HTTP semantic-convention specification.
func TestGinErrorSpanStatus(t *testing.T) {
	f := framework.Setup(t)

	const port = 9083
	f.BuildAndStart("ginserver", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	resp := f.MakeRequest(framework.ServerAddr(port) + "/error")
	defer resp.Body.Close()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	span := f.RequireSpan(framework.SpanMatcher{
		Method:     http.MethodGet,
		Path:       "/error",
		StatusCode: http.StatusInternalServerError,
	})

	assert.Equal(t, "Error", span.Status().Code().String(),
		"5xx response must set span status to Error")
}
