// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestDBClientTxParent checks where commit and rollback spans land when the
// SDK trace instrumentation propagates spans through goroutine-local storage.
// With no parent they are children of the begin span; otherwise they are its
// siblings under the parent, whether that parent was passed in the BeginTx
// context or only active in goroutine-local storage.
func TestDBClientTxParent(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "dbclienttx", "go", "build", "-a", "-trimpath")

	for _, parent := range []string{"none", "explicit", "implicit", "implicit-ended"} {
		for _, begin := range []string{"db", "conn"} {
			for _, end := range []string{"commit", "rollback"} {
				t.Run(parent+"/"+begin+"/"+end, func(t *testing.T) {
					f := testutil.NewTestFixture(t)
					f.Run("dbclienttx", "-parent="+parent, "-begin="+begin, "-end="+end)

					f.RequireTraceCount(1)
					beginSpan := testutil.RequireSpan(t, f.Traces(),
						testutil.IsClient,
						testutil.HasAttribute("db.operation.name", "START"),
					)
					endSpan := testutil.RequireSpan(t, f.Traces(),
						testutil.IsClient,
						testutil.HasAttribute("db.operation.name", strings.ToUpper(end)),
					)
					require.Equal(t, beginSpan.TraceID(), endSpan.TraceID(),
						"%s span must share the transaction's trace", end)

					if parent == "none" {
						f.RequireSpansPerTrace(2)
						require.True(t, beginSpan.ParentSpanID().IsEmpty(), "begin span must be the trace root")
						require.Equal(t, beginSpan.SpanID(), endSpan.ParentSpanID(),
							"%s span parent must be the begin span", end)
						return
					}

					f.RequireSpansPerTrace(3)
					parentSpan := testutil.RequireSpan(t, f.Traces(), testutil.HasName("tx-parent"))
					require.True(t, parentSpan.ParentSpanID().IsEmpty(), "tx-parent must be the trace root")
					require.Equal(
						t,
						parentSpan.TraceID(),
						beginSpan.TraceID(),
						"begin span must be in tx-parent's trace",
					)
					require.Equal(
						t,
						parentSpan.SpanID(),
						beginSpan.ParentSpanID(),
						"begin span parent must be tx-parent",
					)
					require.Equal(t, parentSpan.SpanID(), endSpan.ParentSpanID(),
						"%s span parent must be tx-parent", end)
				})
			}
		}
	}
}
