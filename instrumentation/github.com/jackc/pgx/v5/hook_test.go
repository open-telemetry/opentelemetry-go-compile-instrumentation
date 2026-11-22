// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pgx

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
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

func TestSanitizeSQL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "string literal",
			input: "SELECT * FROM users WHERE name = 'John Doe'",
			want:  "SELECT * FROM users WHERE name = ?",
		},
		{
			name:  "string literal with escaped quotes",
			input: "SELECT 'It''s a secret'",
			want:  "SELECT ?",
		},
		{
			name:  "integer literal",
			input: "SELECT * FROM users WHERE id = 123",
			want:  "SELECT * FROM users WHERE id = ?",
		},
		{
			name:  "decimal literal",
			input: "SELECT * FROM products WHERE price > 49.99",
			want:  "SELECT * FROM products WHERE price > ?",
		},
		{
			name:  "preserves parameter placeholders",
			input: "SELECT * FROM users WHERE id = $1 AND tenant = $2",
			want:  "SELECT * FROM users WHERE id = $1 AND tenant = $2",
		},
		{
			name:  "preserves column names with numbers",
			input: "SELECT col1, field_2 FROM table1",
			want:  "SELECT col1, field_2 FROM table1",
		},
		{
			name:  "multiple mixed literals",
			input: "INSERT INTO users (name, age, secret) VALUES ('Alice', 30, 's3cr3t')",
			want:  "INSERT INTO users (name, age, secret) VALUES (?, ?, ?)",
		},
		{
			name:  "dollar-quoted string",
			input: "INSERT INTO secrets VALUES ($$supersecret$$)",
			want:  "INSERT INTO secrets VALUES (?)",
		},
		{
			name:  "preserves line comments",
			input: "-- get user\nSELECT * FROM users WHERE id = 1",
			want:  "-- get user\nSELECT * FROM users WHERE id = ?",
		},
		{
			name:  "preserves block comments",
			input: "/* query */ SELECT * FROM users WHERE id = 1",
			want:  "/* query */ SELECT * FROM users WHERE id = ?",
		},
		{
			name:  "empty query",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeSQL(tt.input))
		})
	}
}

func TestSanitizeSQL_OptInRawQuery(t *testing.T) {
	t.Setenv(rawQueryEnv, "true")
	query := "SELECT * FROM users WHERE secret = 'confidential' AND pin = 1234"
	assert.Equal(t, query, sanitizeSQL(query))
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

func TestTracer_TraceQuery_LiteralSanitization(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "pgx")
	sr, tr := setupTestTracer(t)

	ctx := context.Background()
	ctx = tr.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{
		SQL: "SELECT * FROM users WHERE email = 'sensitive@example.com' AND role_id = 42",
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
	// Ensure literal strings and numbers are sanitized out of query text
	assert.Equal(t, "SELECT * FROM users WHERE email = ? AND role_id = ?", attrMap[string(semconv.DBQueryTextKey)])
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

	// Verify idempotency (chaining again with otelTr does not duplicate)
	chainedAgain := chainTracer(chained, otelTr)
	assert.Same(t, chained, chainedAgain)

	ctx := context.Background()
	ctx = chained.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	chained.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	assert.True(t, custom.queryStartCalled)
	assert.True(t, custom.queryEndCalled)
}

func handleMockPostgresConn(c net.Conn) {
	defer c.Close()
	buf := make([]byte, 1024)
	n, err := c.Read(buf)
	if err != nil || n < 8 {
		return
	}

	// SSLRequest code: 80877103 (0x04d2162f)
	if n == 8 && binary.BigEndian.Uint32(buf[4:8]) == 80877103 {
		// Deny SSL with 'N'
		if _, writeErr := c.Write([]byte{'N'}); writeErr != nil {
			return
		}
		// Read subsequent StartupMessage
		n, err = c.Read(buf)
		if err != nil || n == 0 {
			return
		}
	}

	// Reply AuthenticationOk + ReadyForQuery
	authOk := []byte{'R', 0, 0, 0, 8, 0, 0, 0, 0}
	ready := []byte{'Z', 0, 0, 0, 5, 'I'}
	if _, writeErr := c.Write(append(authOk, ready...)); writeErr != nil {
		return
	}

	for {
		n, err = c.Read(buf)
		if err != nil || n == 0 {
			return
		}
		if buf[0] == 'X' { // Terminate
			return
		}
		if buf[0] == 'Q' { // Query
			cmdComplete := []byte{'C', 0, 0, 0, 13, 'S', 'E', 'L', 'E', 'C', 'T', ' ', '1', 0}
			if _, writeErr := c.Write(append(cmdComplete, ready...)); writeErr != nil {
				return
			}
		}
	}
}

func TestConnect_MockServer(t *testing.T) {
	cfg, err := pgx.ParseConfig("postgres://testuser:testpass@localhost:5432/testdb?sslmode=disable")
	require.NoError(t, err)

	cfg.DialFunc = func(_ context.Context, _, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		go handleMockPostgresConn(server)
		return client, nil
	}

	// Simulate BeforeConnect hook setting the tracer
	hctx := hooktest.NewMockHookContext(context.Background(), cfg)
	BeforeConnect(hctx)
	require.NotNil(t, cfg.Tracer)

	conn, err := pgx.ConnectConfig(context.Background(), cfg)
	require.NoError(t, err)
	defer conn.Close(context.Background())

	assert.NotNil(t, conn)
}

func TestHooks_InjectTracer(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "pgx")

	t.Run("BeforeConnect", func(t *testing.T) {
		cfg := &pgx.ConnConfig{}
		hctx := hooktest.NewMockHookContext(context.Background(), cfg)
		BeforeConnect(hctx)
		assert.NotNil(t, cfg.Tracer)
	})

	t.Run("BeforeConnectConfig", func(t *testing.T) {
		cfg := &pgx.ConnConfig{}
		hctx := hooktest.NewMockHookContext(context.Background(), cfg)
		BeforeConnectConfig(hctx)
		assert.NotNil(t, cfg.Tracer)
	})

	t.Run("BeforeNewWithConfig", func(t *testing.T) {
		pcfg := &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}
		hctx := hooktest.NewMockHookContext(context.Background(), pcfg)
		BeforeNewWithConfig(hctx)
		assert.NotNil(t, pcfg.ConnConfig.Tracer)
	})
}
