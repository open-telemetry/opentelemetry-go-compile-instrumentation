//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestHttp(t *testing.T) {
	f := testutil.NewTestFixture(t)

	f.BuildAndStart("httpserver")
	time.Sleep(time.Second)

	f.BuildAndRun("httpclient", "-addr", "http://127.0.0.1:8080", "-name", "test")

	f.RequireTraceCount(1)    // hello request
	f.RequireSpansPerTrace(2) // client + server per trace

	helloClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttributeContaining(string(semconv.URLFullKey), "/hello"),
	)
	testutil.RequireHTTPClientSemconv(t, helloClientSpan, "GET", "http://127.0.0.1:8080/hello?name=test", "127.0.0.1", 200, 8080, "1.1", "http")

	helloServerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.URLPathKey), "/hello"),
	)
	testutil.RequireHTTPServerSemconv(t, helloServerSpan, "GET", "/hello", "http", 200, 8080, "127.0.0.1", "Go-http-client/1.1", "1.1", "127.0.0.1")
}
