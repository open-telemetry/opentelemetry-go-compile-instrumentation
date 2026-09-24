//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"testing"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestHttp(t *testing.T) {
	f := testutil.NewTestFixture(t)

	port := testutil.FreePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	url := "http://" + addr

	f.BuildAndStart("httpserver", fmt.Sprintf("-port=%d", port))
	testutil.WaitForTCP(t, addr)

	f.BuildAndRun("httpclient", "-addr", url, "-name", "test")

	f.RequireTraceCount(1)    // hello request
	f.RequireSpansPerTrace(2) // client + server per trace

	helloClientSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttributeContaining(string(semconv.URLFullKey), "/hello"),
	)
	testutil.RequireHTTPClientSemconv(
		t,
		helloClientSpan,
		"GET",
		url+"/hello?name=test",
		"127.0.0.1",
		200,
		int64(port),
		"1.1",
		"http",
	)

	helloServerSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsServer,
		testutil.HasAttribute(string(semconv.URLPathKey), "/hello"),
	)
	testutil.RequireHTTPServerSemconv(
		t,
		helloServerSpan,
		"GET",
		"/hello",
		"http",
		200,
		int64(port),
		"127.0.0.1",
		"Go-http-client/1.1",
		"1.1",
		"127.0.0.1",
	)
}
