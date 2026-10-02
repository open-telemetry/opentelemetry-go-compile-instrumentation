//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"net"
	"net/url"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestMongo verifies MongoDB client spans against a real MongoDB process
// (testcontainers), rather than the in-process mock used by the integration
// suite. That exercises peer / network attributes under real TCP conditions.
func TestMongo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mongodb testcontainer not supported on windows")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	uri := startMongoContainer(t)

	for _, version := range []string{"1", "2"} {
		t.Run("driver_v"+version, func(t *testing.T) {
			f := testutil.NewTestFixture(t)

			output := f.BuildAndRun("mongoclient", "-uri="+uri, "-version="+version)
			require.Contains(t, output, "MongoDB operations completed successfully")

			insertSpan := testutil.RequireSpan(t, f.Traces(),
				testutil.IsClient,
				testutil.HasAttribute("db.operation.name", "insert"),
			)

			testutil.RequireAttribute(t, insertSpan, "db.system.name", "mongodb")
			testutil.RequireAttribute(t, insertSpan, "db.operation.name", "insert")
			testutil.RequireAttribute(t, insertSpan, "db.namespace", "testdb")
			testutil.RequireAttribute(t, insertSpan, "db.collection.name", "users")
			assertMongoPeerNetworkAttrs(t, insertSpan, uri)
			testutil.RequireAttribute(t, insertSpan, "network.transport", "tcp")
		})
	}
}

// assertMongoPeerNetworkAttrs checks peer metadata under real TCP.
// otelmongo emits network.peer.address / network.peer.port (not server.*).
func assertMongoPeerNetworkAttrs(t *testing.T, insertSpan ptrace.Span, uri string) {
	t.Helper()

	parsed, err := url.Parse(uri)
	require.NoError(t, err)
	uriHost := parsed.Hostname()

	peerAddr, ok := testutil.Attrs(insertSpan)["network.peer.address"].(string)
	require.True(t, ok, "network.peer.address must be a string")
	require.NotEmpty(t, peerAddr)
	// Do not require equality with the URI host: on Linux CI Docker may publish
	// localhost while the TCP peer is the bridge address. Accept a valid IP or
	// an exact match with the URI host.
	require.True(t, net.ParseIP(peerAddr) != nil || peerAddr == uriHost,
		"network.peer.address %q must be an IP or match URI host %q", peerAddr, uriHost)

	testutil.RequireAttributeExists(t, insertSpan, "network.peer.port")
	switch port := testutil.Attrs(insertSpan)["network.peer.port"].(type) {
	case int64:
		require.NotZero(t, port)
	case int:
		require.NotZero(t, port)
	default:
		t.Fatalf("network.peer.port has unexpected type %T (%v)", port, port)
	}
}

func startMongoContainer(t *testing.T) string {
	t.Helper()

	ctx := t.Context()
	mongoContainer, err := mongodb.Run(ctx, "mongo:7.0")
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, mongoContainer)

	uri, err := mongoContainer.ConnectionString(ctx)
	require.NoError(t, err)
	return uri
}
