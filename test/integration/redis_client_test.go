// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestRedisClient(t *testing.T) {
	testCases := []struct {
		name string
	}{
		{
			name: "basic",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := testutil.NewTestFixture(t)
			server := StartRedisServer(t)

			output := f.BuildAndRun("redisclient", "-addr="+server.Addr())
			require.Contains(t, output, "testvalue")

			spans := testutil.AllSpans(f.Traces())
			require.GreaterOrEqual(t, len(spans), 3, "expected at least 3 spans (SET, GET, DEL)")

			// Verify SET span
			setSpan := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasAttribute("db.operation.name", "set"),
			)
			testutil.RequireRedisClientSemconv(
				t,
				setSpan,
				"set",
				server.Addr(),
				"set testkey testvalue",
			)

			// Verify GET span
			getSpan := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasAttribute("db.operation.name", "get"),
			)
			testutil.RequireRedisClientSemconv(
				t,
				getSpan,
				"get",
				server.Addr(),
				"get testkey",
			)

			// Verify DEL span
			delSpan := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasAttribute("db.operation.name", "del"),
			)
			testutil.RequireRedisClientSemconv(
				t,
				delSpan,
				"del",
				server.Addr(),
				"del testkey",
			)
		})
	}
}

// StartRedisServer creates and starts a miniredis server for testing.
// The server is automatically closed when the test completes.
func StartRedisServer(t *testing.T) *miniredis.Miniredis {
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}
