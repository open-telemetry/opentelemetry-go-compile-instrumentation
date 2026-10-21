// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/cassandra"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestCassandraClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cassandra testcontainer not supported on windows")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	t.Parallel()
	testutil.Build(t, "", "cassandraclient", "go", "build", "-a")

	addr := startCassandraContainer(t)

	f := testutil.NewTestFixture(t)

	output := f.Run("cassandraclient", "-addr="+addr, "-keyspace=testks")
	require.Contains(t, output, "Cassandra operations completed successfully")

	spans := testutil.AllSpans(f.Traces())
	require.GreaterOrEqual(t, len(spans), 3, "expected at least 3 spans (CONNECT, INSERT/SELECT, BATCH)")

	// Connection telemetry: gocql's ConnectObserver fires for every host dial.
	connectSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "CONNECT"),
	)
	require.Equal(t, ptrace.SpanKindClient, connectSpan.Kind())
	testutil.RequireAttribute(t, connectSpan, "db.system.name", "cassandra")
	require.NotEmpty(t, testutil.Attrs(connectSpan)["server.address"])

	// Query telemetry: the INSERT executed against the instrumented session.
	insertSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "INSERT"),
	)
	require.Equal(t, "INSERT testks", insertSpan.Name())
	insertAttrs := testutil.Attrs(insertSpan)
	testutil.RequireAttribute(t, insertSpan, "db.system.name", "cassandra")
	testutil.RequireAttribute(t, insertSpan, "db.namespace", "testks")
	require.Contains(t, insertAttrs["db.query.text"], "INSERT INTO users")
	require.NotEmpty(t, insertAttrs["server.address"])

	// Query telemetry: the SELECT read back through the same session.
	selectSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "SELECT"),
	)
	require.Equal(t, "SELECT testks", selectSpan.Name())
	require.Contains(t, testutil.Attrs(selectSpan)["db.query.text"], "SELECT name FROM users")

	// Batch telemetry: both batch statements are joined into one span.
	batchSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute("db.operation.name", "BATCH"),
	)
	require.Equal(t, "BATCH testks", batchSpan.Name())
	testutil.RequireAttribute(t, batchSpan, "db.system.name", "cassandra")
	testutil.RequireAttribute(t, batchSpan, "db.namespace", "testks")
	require.Contains(t, testutil.Attrs(batchSpan)["db.query.text"], "INSERT INTO users")
}

func startCassandraContainer(t *testing.T) string {
	t.Helper()

	ctr, err := cassandra.Run(t.Context(), "cassandra:4.1.3")
	require.NoError(t, err)
	testcontainers.CleanupContainer(t, ctr)

	addr, err := ctr.ConnectionHost(t.Context())
	require.NoError(t, err)

	return addr
}
