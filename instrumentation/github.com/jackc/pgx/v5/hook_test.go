// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pgx

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

type mockCustomTracer struct {
	queryStartCalled bool
	queryEndCalled   bool
}

func (m *mockCustomTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	_ pgx.TraceQueryStartData,
) context.Context {
	m.queryStartCalled = true
	return ctx
}

func (m *mockCustomTracer) TraceQueryEnd(
	_ context.Context,
	_ *pgx.Conn,
	_ pgx.TraceQueryEndData,
) {
	m.queryEndCalled = true
}

func setupTestTracer(t *testing.T) (*tracetest.SpanRecorder, *tracer) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
	})

	tr := &tracer{
		tr: tp.Tracer(instrumentationName),
	}

	return sr, tr
}

func TestOperationName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"SELECT * FROM users", "SELECT"},
		{"  insert into table", "INSERT"},
		{"UPDATE foo SET a=1", "UPDATE"},
		{"DELETE FROM bar", "DELETE"},
		{"", ""},
		{"   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, operationName(tt.input))
		})
	}
}

func TestConfigAttributes(t *testing.T) {
	cfg, err := pgx.ParseConfig("postgres://myuser:mypass@example.com:5433/sampledb")
	require.NoError(t, err)

	attrs := configAttributes(cfg)
	attrMap := make(map[string]any, len(attrs))
	for _, a := range attrs {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}

	assert.Equal(t, "postgresql", attrMap[string(semconv.DBSystemNameKey)])
	assert.Equal(t, "sampledb", attrMap[string(semconv.DBNamespaceKey)])
	assert.Equal(t, "myuser", attrMap["db.user"])
	assert.Equal(t, "example.com", attrMap[string(semconv.ServerAddressKey)])
	assert.Equal(t, int64(5433), attrMap[string(semconv.ServerPortKey)])
}

func TestTracer_TraceQuery(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "pgx")
	sr, tr := setupTestTracer(t)

	ctx := context.Background()
	ctx = tr.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{
		SQL: "SELECT * FROM users WHERE id = $1",
	})

	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{
		Err: nil,
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "SELECT", span.Name())
	assert.Equal(t, codes.Unset, span.Status().Code)

	attrMap := make(map[string]any)
	for _, a := range span.Attributes() {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}

	assert.Equal(t, "postgresql", attrMap[string(semconv.DBSystemNameKey)])
	assert.Equal(t, "SELECT", attrMap[string(semconv.DBOperationNameKey)])
	assert.Equal(t, "SELECT * FROM users WHERE id = $1", attrMap[string(semconv.DBQueryTextKey)])
}

func TestTracer_TraceQuery_Error(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "pgx")
	sr, tr := setupTestTracer(t)

	ctx := context.Background()
	ctx = tr.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{
		SQL: "INVALID SQL STATEMENT",
	})

	testErr := errors.New("syntax error")
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{
		Err: testErr,
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "syntax error", span.Status().Description)
	require.Len(t, span.Events(), 1)
	assert.Equal(t, "exception", span.Events()[0].Name)
}

func TestTracer_TraceBatch(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "pgx")
	sr, tr := setupTestTracer(t)

	ctx := context.Background()
	ctx = tr.TraceBatchStart(ctx, nil, pgx.TraceBatchStartData{})
	tr.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{
		SQL: "INSERT INTO tbl VALUES (1)",
	})
	tr.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "BATCH", spans[0].Name())
}

func TestTracer_TraceCopyFrom(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "pgx")
	sr, tr := setupTestTracer(t)

	ctx := context.Background()
	ctx = tr.TraceCopyFromStart(ctx, nil, pgx.TraceCopyFromStartData{
		TableName: pgx.Identifier{"public", "users"},
	})
	tr.TraceCopyFromEnd(ctx, nil, pgx.TraceCopyFromEndData{})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "COPY", spans[0].Name())
}

func TestTracer_TracePrepare(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "pgx")
	sr, tr := setupTestTracer(t)

	ctx := context.Background()
	ctx = tr.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{
		Name: "stmt1",
		SQL:  "SELECT * FROM items WHERE id = $1",
	})
	tr.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "PREPARE", spans[0].Name())
}

func TestChainTracer(t *testing.T) {
	custom := &mockCustomTracer{}
	otelTr := newTracer()

	chained := chainTracer(custom, otelTr)
	require.NotNil(t, chained)

	ctx := context.Background()
	ctx = chained.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	chained.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	assert.True(t, custom.queryStartCalled)
	assert.True(t, custom.queryEndCalled)
}

func TestHooks_InjectTracer(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "pgx")

	t.Run("BeforeConnectConfig", func(t *testing.T) {
		cfg := &pgx.ConnConfig{}
		hctx := hooktest.NewMockHookContext(context.Background(), cfg)
		BeforeConnectConfig(hctx)
		assert.NotNil(t, cfg.Tracer)
	})

	t.Run("AfterConnect", func(t *testing.T) {
		hctx := hooktest.NewMockHookContext()
		hctx.SetReturnVal(0, (*pgx.Conn)(nil))
		AfterConnect(hctx)
	})

	t.Run("BeforeNewWithConfig", func(t *testing.T) {
		pcfg := &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}
		hctx := hooktest.NewMockHookContext(context.Background(), pcfg)
		BeforeNewWithConfig(hctx)
		assert.NotNil(t, pcfg.ConnConfig.Tracer)
	})

	t.Run("AfterNewPool", func(t *testing.T) {
		hctx := hooktest.NewMockHookContext()
		hctx.SetReturnVal(0, (*pgxpool.Pool)(nil))
		AfterNewPool(hctx)
	})
}
