// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestHTTPServerInstrumentation(t *testing.T) {
	f := app.NewE2EFixture(t)

	f.BuildApp("httpserver")
	f.StartApp("httpserver", "-port=8081")
	time.Sleep(time.Second)

	resp, err := http.Get("http://localhost:8081/hello?name=test")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	time.Sleep(100 * time.Millisecond)

	span := f.RequireSingleSpan()
	app.RequireHTTPServerSemconv(t, span, "GET", "/hello", "http", 200)
}
