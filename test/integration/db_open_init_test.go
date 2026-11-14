// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"

	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"go.opentelemetry.io/otelc/test/testutil"
)

// A database opened in a package-level variable runs the sql.Open hooks before
// the database/sql instrumentation package has initialized. Its spans must
// still carry the connection attributes, the same as a database opened in main.
func TestDBOpenedDuringInit(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "dbopeninit", "go", "build", "-a")
	f := testutil.NewTestFixture(t)

	output := f.Run("dbopeninit")

	require.NotContains(t, output, "failed to exec")
	f.WaitForSpans(2)
	pingSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBOperationNameKey), "PING"),
	)
	testutil.RequireDBClientSemconv(t, pingSpan, "PING", "ping", "unknown", 0, "testdb")
	execSpan := testutil.RequireSpan(t, f.Traces(),
		testutil.IsClient,
		testutil.HasAttribute(string(semconv.DBOperationNameKey), "INSERT"),
	)
	testutil.RequireDBClientSemconv(t, execSpan,
		"INSERT", "INSERT INTO users (name) VALUES ('x')", "unknown", 0, "testdb")
}
