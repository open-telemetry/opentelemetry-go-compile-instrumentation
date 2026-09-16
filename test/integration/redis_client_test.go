// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestRedisClient(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "redisclient", "go", "build", "-a")

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

			output := f.Run("redisclient", "-addr="+server.Addr())
			require.Contains(t, output, "testvalue")

			spans := testutil.AllSpans(f.Traces())
			require.GreaterOrEqual(t, len(spans), 4, "expected at least 4 spans (SET, GET, CONN GET, DEL)")

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

			getCount := 0
			for _, span := range spans {
				if testutil.IsClient(span) && testutil.HasAttribute("db.operation.name", "get")(span) {
					getCount++
					testutil.RequireRedisClientSemconv(
						t,
						span,
						"get",
						server.Addr(),
						"get testkey",
					)
				}
			}
			require.Equal(t, 2, getCount, "client GET and Conn GET should each produce one span")

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
