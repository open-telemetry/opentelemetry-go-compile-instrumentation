// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestHTTPClientInstrumentation(t *testing.T) {
	f := app.NewE2EFixture(t)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"message":"Hello from test server"}`)
	}))
	defer testServer.Close()

	f.BuildApp("httpclient")
	f.RunApp("httpclient", "-addr="+testServer.URL, "-name=world")

	span := f.RequireSingleSpan()
	app.RequireHTTPClientSemconv(t, span, "GET", testServer.URL+"/hello?name=world", "127.0.0.1", 200)
}
