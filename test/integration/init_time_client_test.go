// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"net"
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

// A gRPC client created in a package-level variable gets its stats handler
// before the OpenTelemetry API has initialized, and must trace later calls.
func TestGRPCClientCreatedDuringInit(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "grpcinit", "go", "build", "-a")
	f := testutil.NewTestFixture(t)
	server := StartGRPCServer(t)

	output := f.Run("grpcinit", "-addr="+server.Addr)

	require.Contains(t, output, "Hello InitTest")
	require.NotContains(t, output, "failed to exec")
	span := f.RequireSingleSpan()
	host, _, err := net.SplitHostPort(server.Addr)
	require.NoError(t, err)
	testutil.RequireGRPCClientSemconv(t, span, host, "greeter.Greeter", "SayHello", 0)
}

// An HTTP request during another package's init() skips the client and server
// hooks. Their after hooks must not panic on the hook package's logger, and the
// request from main must be traced.
func TestHTTPUsedDuringInit(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "httpinit", "go", "build", "-a")
	f := testutil.NewTestFixture(t)

	output := f.Run("httpinit")

	require.NotContains(t, output, "failed to exec")
	f.WaitForSpans(2)
	spans := testutil.AllSpans(f.Traces())
	require.Len(t, spans, 2)
	testutil.RequireSpan(t, f.Traces(), testutil.IsClient)
	testutil.RequireSpan(t, f.Traces(), testutil.IsServer)
}

// MongoDB clients created in package-level variables get their command
// monitor before the OpenTelemetry API has initialized, and must trace later
// commands.
func TestMongoClientsCreatedDuringInit(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "mongoinit", "go", "build", "-a")
	f := testutil.NewTestFixture(t)
	addr := StartMockMongoServer(t)

	output := f.Run("mongoinit", "-uri=mongodb://"+addr)

	require.NotContains(t, output, "failed to exec")
	f.WaitForSpans(2)
	for _, collection := range []string{"users_v1", "users_v2"} {
		testutil.RequireSpan(t, f.Traces(),
			testutil.IsClient,
			testutil.HasAttribute("db.operation.name", "insert"),
			testutil.HasAttribute("db.collection.name", collection),
		)
	}
}
