// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestGinServer(t *testing.T) {
	testCases := []struct {
		name       string
		port       int
		path       string
		method     string
		statusCode int
	}{
		{
			name:       "GET hello returns 200",
			port:       8084,
			path:       "/hello",
			method:     http.MethodGet,
			statusCode: http.StatusOK,
		},
		{
			name:       "GET error returns 500",
			port:       8085,
			path:       "/error",
			method:     http.MethodGet,
			statusCode: http.StatusInternalServerError,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)

			f.BuildAndStart("ginserver", fmt.Sprintf("-port=%d", tc.port))
			testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", tc.port))

			url := fmt.Sprintf("http://127.0.0.1:%d%s?name=test", tc.port, tc.path)
			resp, err := http.Get(url) //nolint:noctx
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, tc.statusCode, resp.StatusCode)
			testutil.WaitForSpanFlush(t)

			span := f.RequireSingleSpan()
			testutil.RequireHTTPServerSemconv(
				t,
				span,
				tc.method,
				tc.path,
				"http",
				int64(tc.statusCode),
				int64(tc.port),
				"127.0.0.1",
				"Go-http-client/1.1",
				"1.1",
				"127.0.0.1",
			)
		})
	}
}
