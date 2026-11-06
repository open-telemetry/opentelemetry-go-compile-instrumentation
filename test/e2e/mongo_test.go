//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"net"
	"net/url"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestMongo verifies MongoDB client spans against a real mongod process
// (testcontainers), rather than the mock wire-protocol server used by the
// integration suite. That exercises real driver/server fidelity and
// non-loopback peer addresses, which the loopback mock cannot produce.
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

			var insertSpans []ptrace.Span
			for _, s := range testutil.AllSpans(f.Traces()) {
				if testutil.IsClient(s) && testutil.HasAttribute("db.operation.name", "insert")(s) {
					insertSpans = append(insertSpans, s)
				}
			}
			require.Len(t, insertSpans, 1, "expected exactly one insert span")
			insertSpan := insertSpans[0]

			testutil.RequireMongoClientSemconv(t, insertSpan, "insert", "testdb", "users")
			assertMongoPeerNetworkAttrs(t, insertSpan, uri)
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
	// otelmongo v1 emits network.peer.address as "host:port"; v2 emits the bare
	// host. The host comes verbatim from the URI, so compare the host part.
	if host, _, err := net.SplitHostPort(peerAddr); err == nil {
		peerAddr = host
	}
	require.Equal(t, uriHost, peerAddr,
		"network.peer.address %q must match URI host %q", peerAddr, uriHost)

	uriPort, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	port, ok := testutil.Attrs(insertSpan)["network.peer.port"].(int64)
	require.True(t, ok, "network.peer.port must be an int64")
	require.Equal(t, int64(uriPort), port)
}

func startMongoContainer(t *testing.T) string {
	t.Helper()

	ctx := t.Context()
	mongoContainer, err := mongodb.Run(ctx, "mongo:7.0")
	testcontainers.CleanupContainer(t, mongoContainer)
	require.NoError(t, err)

	uri, err := mongoContainer.ConnectionString(ctx)
	require.NoError(t, err)
	return uri
}
