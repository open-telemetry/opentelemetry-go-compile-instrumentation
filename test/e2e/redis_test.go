// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package test

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

func TestRedis(t *testing.T) {
	f := testutil.NewTestFixture(t)

	// Start miniredis server
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)

	f.BuildAndRun("redisclient", "-addr", s.Addr())

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
		s.Addr(),
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
		s.Addr(),
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
		s.Addr(),
		"del testkey",
	)
}
