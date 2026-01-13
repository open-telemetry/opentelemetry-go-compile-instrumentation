//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"testing"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestHttp(t *testing.T) {
	f := app.NewE2EFixture(t)

	f.BuildApp("httpserver")
	f.StartApp("httpserver")
	time.Sleep(time.Second)

	f.BuildApp("httpclient")
	f.RunApp("httpclient", "-name", "test")

	f.RequireTraceCount(1)    // hello request
	f.RequireSpansPerTrace(2) // client + server per trace

	helloClientSpan := app.RequireSpan(t, f.Traces(),
		app.IsClient,
		app.HasAttributeContaining(string(semconv.URLFullKey), "/hello"),
	)
	app.RequireHTTPClientSemconv(t, helloClientSpan, "GET", "http://localhost:8080/hello?name=test", "localhost", 200)

	helloServerSpan := app.RequireSpan(t, f.Traces(),
		app.IsServer,
		app.HasAttribute(string(semconv.URLPathKey), "/hello"),
	)
	app.RequireHTTPServerSemconv(t, helloServerSpan, "GET", "/hello", "http", 200)
}
