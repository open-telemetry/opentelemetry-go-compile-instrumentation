// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package clickhouse

import (
	"context"
	"errors"
	"testing"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
)

type mockHookContext struct {
	hook.HookContext
	data   any
	params map[int]any
}

func newMockHookContext() *mockHookContext {
	return &mockHookContext{params: map[int]any{}}
}

func (m *mockHookContext) SetData(v any)           { m.data = v }
func (m *mockHookContext) GetData() any            { return m.data }
func (m *mockHookContext) SetParam(idx int, v any) { m.params[idx] = v }
func (m *mockHookContext) GetParam(idx int) any    { return m.params[idx] }

// fakeConn and fakeBatch stand in for the unexported *clickhouse and *batch
// receivers; the accessors that accessors.go installs at build time are
// replaced by ones that read them.
type fakeConn struct{ opt *ch.Options }

type fakeBatch struct{ ctx context.Context }

func setup(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prevTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)

	prev := accessors
	setAccessors(
		func(c any) *ch.Options {
			if fc, ok := c.(*fakeConn); ok {
				return fc.opt
			}
			return nil
		},
		func(b any) context.Context {
			if fb, ok := b.(*fakeBatch); ok {
				return fb.ctx
			}
			return nil
		},
	)
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		accessors = prev
	})
	return sr
}

func newConn() *fakeConn {
	return &fakeConn{opt: &ch.Options{
		Addr: []string{"ch.example:9000"},
		Auth: ch.Auth{Database: "analytics"},
	}}
}

func attrs(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	m := map[attribute.Key]attribute.Value{}
	for _, kv := range s.Attributes() {
		m[kv.Key] = kv.Value
	}
	return m
}

func parentContext(t *testing.T) (context.Context, trace.SpanContext) {
	t.Helper()
	ctx, span := otel.Tracer("test").Start(context.Background(), "parent")
	t.Cleanup(func() { span.End() })
	return ctx, span.SpanContext()
}

func TestQuery(t *testing.T) {
	sr := setup(t)
	ctx, parent := parentContext(t)

	ictx := newMockHookContext()
	BeforeQuery(ictx, newConn(), ctx, "SELECT count() FROM events WHERE id = ?", 1)
	AfterQuery(ictx, nil, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, "SELECT analytics", s.Name())
	assert.Equal(t, trace.SpanKindClient, s.SpanKind())
	assert.Equal(t, parent.SpanID(), s.Parent().SpanID())
	assert.Equal(t, codes.Unset, s.Status().Code)

	a := attrs(s)
	assert.Equal(t, "clickhouse", a["db.system.name"].AsString())
	assert.Equal(t, "SELECT", a["db.operation.name"].AsString())
	assert.Equal(t, "SELECT count() FROM events WHERE id = ?", a["db.query.text"].AsString())
	assert.Equal(t, "analytics", a["db.namespace"].AsString())
	assert.Equal(t, "ch.example", a["server.address"].AsString())
	assert.Equal(t, int64(9000), a["server.port"].AsInt64())
}

func TestQueryServerException(t *testing.T) {
	sr := setup(t)

	ictx := newMockHookContext()
	BeforeQuery(ictx, newConn(), context.Background(), "SELECT * FROM missing")
	AfterQuery(ictx, nil, &ch.Exception{Code: 60, Message: "Table analytics.missing does not exist"})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	s := spans[0]
	assert.Equal(t, codes.Error, s.Status().Code)
	a := attrs(s)
	assert.Equal(t, "60", a["error.type"].AsString())
	assert.Equal(t, "60", a["db.response.status_code"].AsString())
	require.Len(t, s.Events(), 1)
	assert.Equal(t, "exception", s.Events()[0].Name)
}

func TestExecClientError(t *testing.T) {
	sr := setup(t)

	ictx := newMockHookContext()
	BeforeExec(ictx, newConn(), context.Background(), "CREATE TABLE t (x UInt8) ENGINE = Memory")
	AfterExec(ictx, errors.New("acquire conn timeout"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "CREATE analytics", spans[0].Name())
	a := attrs(spans[0])
	assert.Equal(t, "_OTHER", a["error.type"].AsString())
	_, hasCode := a["db.response.status_code"]
	assert.False(t, hasCode)
}

type errRow struct{ err error }

func (r errRow) Err() error         { return r.err }
func (errRow) Scan(...any) error    { return nil }
func (errRow) ScanStruct(any) error { return nil }

func TestQueryRowRecordsRowError(t *testing.T) {
	sr := setup(t)

	ictx := newMockHookContext()
	BeforeQueryRow(ictx, newConn(), context.Background(), "SELECT 1")
	AfterQueryRow(ictx, errRow{err: errors.New("boom")})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestSelectSuppressesNestedQuery(t *testing.T) {
	sr := setup(t)
	ctx, parent := parentContext(t)
	conn := newConn()

	sel := newMockHookContext()
	BeforeSelect(sel, conn, ctx, &[]string{}, "SELECT name FROM users")
	selectCtx, ok := sel.GetParam(ctxParamIndex).(context.Context)
	require.True(t, ok, "BeforeSelect must replace the context parameter")

	// Select calls Query with the context it was given.
	q := newMockHookContext()
	BeforeQuery(q, conn, selectCtx, "SELECT name FROM users")
	AfterQuery(q, nil, nil)
	AfterSelect(sel, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1, "only the Select span is recorded")
	assert.Equal(t, "SELECT analytics", spans[0].Name())
	assert.Equal(t, parent.SpanID(), spans[0].Parent().SpanID())
}

func TestBatchOneSpanPerSend(t *testing.T) {
	sr := setup(t)
	ctx, parent := parentContext(t)

	prep := newMockHookContext()
	BeforePrepareBatch(prep, newConn(), ctx, "INSERT INTO events")
	batchCtx, ok := prep.GetParam(ctxParamIndex).(context.Context)
	require.True(t, ok, "BeforePrepareBatch must replace the context parameter")
	AfterPrepareBatch(prep, nil, nil)

	b := &fakeBatch{ctx: batchCtx}
	send := newMockHookContext()
	BeforeBatchSend(send, b)
	AfterBatchSend(send, nil)

	spans := sr.Ended()
	require.Len(t, spans, 2)
	for _, s := range spans {
		assert.Equal(t, "INSERT analytics", s.Name())
		assert.Equal(t, trace.SpanKindClient, s.SpanKind())
		assert.Equal(t, parent.SpanID(), s.Parent().SpanID(), "PrepareBatch and Send are siblings")
		assert.Equal(t, "INSERT INTO events", attrs(s)["db.query.text"].AsString())
		assert.Equal(t, "ch.example", attrs(s)["server.address"].AsString())
	}
}

func TestBatchNotPreparedOnNativeConnIsNotTraced(t *testing.T) {
	sr := setup(t)

	// A batch prepared through database/sql keeps a context without the mark.
	send := newMockHookContext()
	BeforeBatchSend(send, &fakeBatch{ctx: context.Background()})
	AfterBatchSend(send, nil)

	// Receivers the accessor does not know are ignored too.
	other := newMockHookContext()
	BeforeBatchSend(other, struct{}{})
	AfterBatchSend(other, nil)

	assert.Empty(t, sr.Ended())
}

func TestPingAndAsyncInsert(t *testing.T) {
	sr := setup(t)
	conn := newConn()

	ping := newMockHookContext()
	BeforePing(ping, conn, context.Background())
	AfterPing(ping, nil)

	ins := newMockHookContext()
	BeforeAsyncInsert(ins, conn, context.Background(), "INSERT INTO events VALUES (1)", true)
	AfterAsyncInsert(ins, nil)

	spans := sr.Ended()
	require.Len(t, spans, 2)
	assert.Equal(t, "PING analytics", spans[0].Name())
	_, hasQuery := attrs(spans[0])["db.query.text"]
	assert.False(t, hasQuery)
	assert.Equal(t, "INSERT analytics", spans[1].Name())
}

func TestUnknownConnHasNoServerAttributes(t *testing.T) {
	sr := setup(t)

	ictx := newMockHookContext()
	BeforeExec(ictx, struct{}{}, nil, "SELECT 1")
	AfterExec(ictx, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "SELECT", spans[0].Name())
	a := attrs(spans[0])
	_, hasAddr := a["server.address"]
	assert.False(t, hasAddr)
	_, hasNS := a["db.namespace"]
	assert.False(t, hasNS)
}

func TestDisabled(t *testing.T) {
	sr := setup(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "clickhouse")
	conn := newConn()

	q := newMockHookContext()
	BeforeQuery(q, conn, context.Background(), "SELECT 1")
	AfterQuery(q, nil, nil)

	prep := newMockHookContext()
	BeforePrepareBatch(prep, conn, context.Background(), "INSERT INTO events")
	AfterPrepareBatch(prep, nil, nil)
	_, replaced := prep.GetParam(ctxParamIndex).(context.Context)
	assert.False(t, replaced, "a disabled instrumentation leaves the context alone")

	assert.Empty(t, sr.Ended())
}
