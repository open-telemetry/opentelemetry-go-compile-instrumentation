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
			f := testutil.NewTestFixture(t)

			f.BuildAndStart("httpserver", fmt.Sprintf("-port=%d", tc.port))
			testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", tc.port))

			url := fmt.Sprintf("%s://127.0.0.1:%d%s?name=test", tc.scheme, tc.port, tc.path)
			resp, err := http.Get(url)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			testutil.WaitForSpanFlush(t)

			span := f.RequireSingleSpan()
			testutil.RequireHTTPServerSemconv(
				t,
				span,
				tc.method,
				tc.path,
				tc.scheme,
				200,
				int64(tc.port),
				"127.0.0.1",
				"Go-http-client/1.1",
				"1.1",
				"127.0.0.1",
			)
		})
	}
}
