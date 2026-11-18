// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"go.opentelemetry.io/otelc/test/testutil"
)

func TestDBRows(t *testing.T) {
	t.Parallel()
	testutil.Build(t, "", "dbrows", "go", "build", "-a", "-trimpath")

	for _, endpoint := range []string{"db", "conn", "tx", "stmt", "txstmt", "row"} {
		t.Run(endpoint, func(t *testing.T) {
			fixture := testutil.NewTestFixture(t)
			fixture.SetEnv("OTEL_GO_SQL_ROWS_SPAN_ENABLED", "true")
			fixture.Run("dbrows", "-endpoint="+endpoint)

			query := testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("SELECT"))
			rows := testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("sql.rows"))
			parent := testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("operation"))
			require.Equal(t, ptrace.SpanKindClient, query.Kind())
			require.Equal(t, ptrace.SpanKindInternal, rows.Kind())
			require.Equal(t, query.TraceID(), rows.TraceID())
			require.Equal(t, query.SpanID(), rows.ParentSpanID())
			require.GreaterOrEqual(t, rows.StartTimestamp(), query.EndTimestamp())
			require.LessOrEqual(t, rows.EndTimestamp(), parent.EndTimestamp())
			count := int64(3)
			if endpoint == "row" {
				count = 1
			} else {
				require.GreaterOrEqual(t, rows.EndTimestamp().AsTime().Sub(rows.StartTimestamp().AsTime()),
					30*time.Millisecond)
				between := testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("between-reads"))
				require.Equal(t, parent.SpanID(), between.ParentSpanID(),
					"rows must not become an implicit GLS parent")
			}
			testutil.RequireAttribute(t, rows, "db.response.returned_rows", count)
			testutil.RequireAttribute(t, rows, "db.query.text", "SELECT id FROM synthetic_rows")
			followup := testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("followup"))
			require.Equal(t, parent.SpanID(), followup.ParentSpanID())
			testutil.RequireNoDanglingParents(t, fixture.Traces())
		})
	}

	for _, scenario := range []struct {
		mode  string
		count int64
		err   bool
	}{
		{mode: "empty", count: 0},
		{mode: "early", count: 1},
		{mode: "scan-error", count: 1},
		{mode: "multiple", count: 3},
		{mode: "next-error", count: 1, err: true},
		{mode: "next-result-error", count: 2, err: true},
		{mode: "close-error", count: 3, err: true},
		{mode: "cancel", count: 1, err: true},
	} {
		t.Run(scenario.mode, func(t *testing.T) {
			fixture := testutil.NewTestFixture(t)
			fixture.SetEnv("OTEL_GO_SQL_ROWS_SPAN_ENABLED", "true")
			fixture.Run("dbrows", "-mode="+scenario.mode)
			rows := testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("sql.rows"))
			testutil.RequireAttribute(t, rows, "db.response.returned_rows", scenario.count)
			require.Equal(t, 1, countRowsSpans(fixture.Traces()), "must finish exactly once")
			if scenario.err {
				require.Equal(t, ptrace.StatusCodeError, rows.Status().Code())
				require.Positive(t, rows.Events().Len())
			} else {
				require.Equal(t, ptrace.StatusCodeUnset, rows.Status().Code())
			}
		})
	}

	for _, mode := range []string{"race", "cancel-before-return", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			fixture := testutil.NewTestFixture(t)
			fixture.SetEnv("OTEL_GO_SQL_ROWS_SPAN_ENABLED", "true")
			fixture.Run("dbrows", "-mode="+mode, "-repeat=64")
			require.Equal(t, 64, countRowsSpans(fixture.Traces()))
			querySpans := make(map[string]bool)
			for _, span := range testutil.AllSpans(fixture.Traces()) {
				if span.Name() == "SELECT" {
					querySpans[span.SpanID().String()] = true
				}
			}
			for _, span := range testutil.AllSpans(fixture.Traces()) {
				if span.Name() != "sql.rows" {
					continue
				}
				parent := span.ParentSpanID().String()
				require.True(t, querySpans[parent], "one rows span per query")
				delete(querySpans, parent)
				count, exists := span.Attributes().Get("db.response.returned_rows")
				require.True(t, exists)
				require.GreaterOrEqual(t, count.Int(), int64(0))
				require.LessOrEqual(t, count.Int(), int64(3))
			}
			require.Empty(t, querySpans, "no query lifecycle lost to a cancellation race")
		})
	}

	t.Run("NoCallerSpan", func(t *testing.T) {
		fixture := testutil.NewTestFixture(t)
		fixture.SetEnv("OTEL_GO_SQL_ROWS_SPAN_ENABLED", "true")
		fixture.Run("dbrows", "-mode=no-parent")
		query := testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("SELECT"))
		rows := testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("sql.rows"))
		require.Equal(t, query.TraceID(), rows.TraceID())
		require.Equal(t, query.SpanID(), rows.ParentSpanID())
		require.Len(t, testutil.AllSpans(fixture.Traces()), 2)
	})

	for _, mode := range []string{"query-error", "unclosed"} {
		t.Run(mode, func(t *testing.T) {
			fixture := testutil.NewTestFixture(t)
			fixture.SetEnv("OTEL_GO_SQL_ROWS_SPAN_ENABLED", "true")
			fixture.Run("dbrows", "-mode="+mode)
			require.Zero(t, countRowsSpans(fixture.Traces()))
			query := testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("SELECT"))
			if mode == "query-error" {
				require.Equal(t, ptrace.StatusCodeError, query.Status().Code())
			}
		})
	}

	for _, configuration := range []struct {
		name, enabled, disabled, sampler string
	}{
		{name: "Default"},
		{name: "False", enabled: "false"},
		{name: "Invalid", enabled: "invalid"},
		{name: "DatabaseDisabled", enabled: "true", disabled: "database"},
		{name: "NotSampled", enabled: "true", sampler: "always_off"},
	} {
		t.Run(configuration.name, func(t *testing.T) {
			fixture := testutil.NewTestFixture(t)
			fixture.SetEnv("OTEL_GO_SQL_ROWS_SPAN_ENABLED", configuration.enabled)
			fixture.SetEnv("OTEL_GO_DISABLED_INSTRUMENTATIONS", configuration.disabled)
			if configuration.sampler != "" {
				fixture.SetEnv("OTEL_TRACES_SAMPLER", configuration.sampler)
			}
			fixture.Run("dbrows", "-mode=early")
			require.Zero(t, countRowsSpans(fixture.Traces()))
			if configuration.sampler != "" {
				require.Empty(t, testutil.AllSpans(fixture.Traces()))
			} else {
				testutil.RequireSpan(t, fixture.Traces(), testutil.HasName("operation"))
			}
		})
	}
}

func countRowsSpans(traces ptrace.Traces) int {
	count := 0
	for _, span := range testutil.AllSpans(traces) {
		if span.Name() == "sql.rows" {
			count++
		}
	}
	return count
}
