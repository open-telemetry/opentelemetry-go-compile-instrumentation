// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// A database call during another package's init() runs the database/sql hooks
// before the OpenTelemetry API has initialized. The call is not traced, but
// later calls must still be.
func TestDBClientUsedDuringInit(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "dbinit", "go", "build", "-a", "-trimpath")
	f := testutil.NewTestFixture(t)

	f.Run("dbinit")

	f.WaitForSpans(2)
	var names []string
	for _, span := range testutil.AllSpans(f.Traces()) {
		names = append(names, span.Name())
	}
	require.ElementsMatch(t, []string{"PING", "INSERT"}, names)
}

// go-redis runs its hooks outside any trampoline, so a command during another
// package's init() must not panic in the instrumentation and crash the app.
func TestRedisClientUsedDuringInit(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "redisinit", "go", "build", "-a")
	f := testutil.NewTestFixture(t)

	f.Run("redisinit")

	span := f.RequireSingleSpan()
	require.Equal(t, "ping", span.Name())
}
