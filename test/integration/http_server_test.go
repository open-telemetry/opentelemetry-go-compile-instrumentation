// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/app"
)

func TestHTTPServer(t *testing.T) {
	testCases := []struct {
		name   string
		scheme string
		port   int
		path   string
		method string
	}{
		{
			name:   "basic",
			scheme: "http",
			port:   8081,
			path:   "/hello",
			method: "GET",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := app.NewE2EFixture(t)

			f.BuildApp("httpserver")
			f.StartApp("httpserver", fmt.Sprintf("-port=%d", tc.port))
			time.Sleep(time.Second)

			url := fmt.Sprintf("%s://localhost:%d%s?name=test", tc.scheme, tc.port, tc.path)
			resp, err := http.Get(url)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			time.Sleep(100 * time.Millisecond)

			span := f.RequireSingleSpan()
			app.RequireHTTPServerSemconv(t, span, tc.method, tc.path, tc.scheme, 200)
		})
	}
}
