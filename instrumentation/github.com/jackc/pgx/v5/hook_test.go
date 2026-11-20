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

func (m *mockCustomTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	m.queryStartCalled = true
	return ctx
}

func (m *mockCustomTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
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
	otelTracer = tr

	testCfg, _ := pgx.ParseConfig("postgres://testuser:testpass@localhost:5432/testdb")
	origConfigFunc := connConfigFunc
	connConfigFunc = func(conn *pgx.Conn) *pgx.ConnConfig {
		return testCfg
	}
	t.Cleanup(func() {
		connConfigFunc = origConfigFunc
	})

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

func TestTracer_TraceQuery(t *testing.T) {
	sr, tr := setupTestTracer(t)

	ctx := tr.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{
		SQL: "SELECT * FROM users WHERE id = $1",
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{
		Err: nil,
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, "SELECT testdb", span.Name())
	assert.Equal(t, codes.Unset, span.Status().Code)

	attrs := span.Attributes()
	var foundSQL, foundDB, foundHost, foundPort, foundSystem bool
	for _, a := range attrs {
		switch a.Key {
		case semconv.DBQueryTextKey:
			foundSQL = true
			assert.Equal(t, "SELECT * FROM users WHERE id = $1", a.Value.AsString())
		case semconv.DBNamespaceKey:
			foundDB = true
			assert.Equal(t, "testdb", a.Value.AsString())
		case semconv.ServerAddressKey:
			foundHost = true
			assert.Equal(t, "localhost", a.Value.AsString())
		case semconv.ServerPortKey:
			foundPort = true
			assert.Equal(t, int64(5432), a.Value.AsInt64())
		case semconv.DBSystemNameKey:
			foundSystem = true
			assert.Equal(t, semconv.DBSystemNamePostgreSQL.Value.AsString(), a.Value.AsString())
		}
	}

	assert.True(t, foundSQL)
	assert.True(t, foundDB)
	assert.True(t, foundHost)
	assert.True(t, foundPort)
	assert.True(t, foundSystem)
}

func TestTracer_TraceQuery_Error(t *testing.T) {
	sr, tr := setupTestTracer(t)

	queryErr := errors.New("connection reset by peer")
	ctx := tr.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{
		SQL: "INSERT INTO users (name) VALUES ($1)",
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{
		Err: queryErr,
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)

	span := spans[0]
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "connection reset by peer", span.Status().Description)
	require.NotEmpty(t, span.Events())
	assert.Equal(t, "exception", span.Events()[0].Name)
}

func TestTracer_TraceBatch(t *testing.T) {
	sr, tr := setupTestTracer(t)

	ctx := tr.TraceBatchStart(t.Context(), nil, pgx.TraceBatchStartData{})
	tr.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{})
	tr.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "BATCH testdb", spans[0].Name())
}

func TestTracer_TraceCopyFrom(t *testing.T) {
	sr, tr := setupTestTracer(t)

	ctx := tr.TraceCopyFromStart(t.Context(), nil, pgx.TraceCopyFromStartData{
		TableName: pgx.Identifier{"users"},
	})
	tr.TraceCopyFromEnd(ctx, nil, pgx.TraceCopyFromEndData{})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "COPY testdb", spans[0].Name())
}

func TestTracer_TracePrepare(t *testing.T) {
	sr, tr := setupTestTracer(t)

	ctx := tr.TracePrepareStart(t.Context(), nil, pgx.TracePrepareStartData{
		Name: "stmt1",
		SQL:  "SELECT 1",
	})
	tr.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "PREPARE testdb", spans[0].Name())
}

func TestChainTracer(t *testing.T) {
	_, tr := setupTestTracer(t)
	custom := &mockCustomTracer{}

	chained := chainTracer(custom, tr).(pgx.QueryTracer)
	require.NotNil(t, chained)

	ctx := chained.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	chained.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	assert.True(t, custom.queryStartCalled)
	assert.True(t, custom.queryEndCalled)
}

func TestHooks_InjectTracer(t *testing.T) {
	setupTestTracer(t)

	t.Run("BeforeConnectConfig", func(t *testing.T) {
		cfg, err := pgx.ParseConfig("postgres://localhost:5432/testdb")
		require.NoError(t, err)

		mockCtx := hooktest.NewMockHookContext(context.Background(), cfg)
		BeforeConnectConfig(mockCtx)

		assert.NotNil(t, cfg.Tracer)
	})

	t.Run("AfterConnect", func(t *testing.T) {
		mockCtx := hooktest.NewMockHookContext()
		mockCtx.SetReturnVal(0, nil)
		// Should safely no-op when conn is nil
		AfterConnect(mockCtx)
	})

	t.Run("BeforeNewWithConfig", func(t *testing.T) {
		poolCfg, err := pgxpool.ParseConfig("postgres://localhost:5432/testdb")
		require.NoError(t, err)

		mockCtx := hooktest.NewMockHookContext(context.Background(), poolCfg)
		BeforeNewWithConfig(mockCtx)

		assert.NotNil(t, poolCfg.ConnConfig.Tracer)
	})

	t.Run("AfterNewPool", func(t *testing.T) {
		mockCtx := hooktest.NewMockHookContext()
		mockCtx.SetReturnVal(0, nil)
		// Should safely no-op when pool is nil
		AfterNewPool(mockCtx)
	})
}
