//go:build e2e

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"

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

	f := testutil.NewTestFixture(t)

	// Default mongoclient -version is 2 (current mongo-driver / semconv).
	output := f.BuildAndRun("mongoclient", "-uri="+uri, "-version=2")
	require.Contains(t, output, "MongoDB operations completed successfully")

	insertSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "insert"),
	)

	testutil.RequireAttribute(t, insertSpan, "db.system.name", "mongodb")
	testutil.RequireAttribute(t, insertSpan, "db.operation.name", "insert")
	testutil.RequireAttribute(t, insertSpan, "db.namespace", "testdb")
	testutil.RequireAttribute(t, insertSpan, "db.collection.name", "users")
	// Do not hardcode 127.0.0.1: on Linux CI Docker often binds to the bridge
	// address (e.g. 172.17.0.1). Assert the peer attribute is present instead.
	testutil.RequireAttributeExists(t, insertSpan, "network.peer.address")
	require.NotEmpty(t, testutil.Attrs(insertSpan)["network.peer.address"])
	testutil.RequireAttribute(t, insertSpan, "network.transport", "tcp")
}

func startMongoContainer(t *testing.T) string {
	t.Helper()

	ctx := t.Context()
	mongoContainer, err := mongodb.Run(ctx, "mongo:7")
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, mongoContainer)

	uri, err := mongoContainer.ConnectionString(ctx)
	require.NoError(t, err)
	return uri
}
