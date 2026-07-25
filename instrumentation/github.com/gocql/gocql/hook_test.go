// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package gocql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"go.opentelemetry.io/otelc/pkg/hook"
)

type mockHookContext struct {
	hook.HookContext
	params map[int]any
}

func newMockHookContext() *mockHookContext {
	return &mockHookContext{params: make(map[int]any)}
}

func (m *mockHookContext) SetParam(idx int, val any) {
	m.params[idx] = val
}

func (m *mockHookContext) GetParam(idx int) any {
	return m.params[idx]
}

type dummyUserQueryObserver struct {
	called bool
	query  gocql.ObservedQuery
}

func (d *dummyUserQueryObserver) ObserveQuery(ctx context.Context, q gocql.ObservedQuery) {
	d.called = true
	d.query = q
}

type dummyUserBatchObserver struct {
	called bool
	batch  gocql.ObservedBatch
}

func (d *dummyUserBatchObserver) ObserveBatch(ctx context.Context, b gocql.ObservedBatch) {
	d.called = true
	d.batch = b
}

type dummyUserConnectObserver struct {
	called bool
	conn   gocql.ObservedConnect
}

func (d *dummyUserConnectObserver) ObserveConnect(oc gocql.ObservedConnect) {
	d.called = true
	d.conn = oc
}

func TestAfterNewCluster(t *testing.T) {
	ictx := newMockHookContext()
	cluster := gocql.NewCluster("127.0.0.1")

	AfterNewCluster(ictx, cluster)

	require.NotNil(t, cluster.QueryObserver)
	require.NotNil(t, cluster.BatchObserver)
	require.NotNil(t, cluster.ConnectObserver)

	_, isOtel := cluster.QueryObserver.(*otelObserver)
	assert.True(t, isOtel)
}

func TestBeforeNewSession(t *testing.T) {
	ictx := newMockHookContext()
	cluster := gocql.NewCluster("127.0.0.1")

	BeforeNewSession(ictx, *cluster)

	param0, ok := ictx.GetParam(0).(gocql.ClusterConfig)
	require.True(t, ok)
	require.NotNil(t, param0.QueryObserver)

	_, isOtel := param0.QueryObserver.(*otelObserver)
	assert.True(t, isOtel)
}

func TestOtelObserver_ObserveQuery(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	userObs := &dummyUserQueryObserver{}
	obs := newOtelObserver(userObs, nil, nil)
	obs.tracer = tp.Tracer(instrumentationName)

	now := time.Now()
	q := gocql.ObservedQuery{
		Keyspace:  "my_keyspace",
		Statement: "SELECT * FROM users WHERE id = 1",
		Start:     now,
		End:       now.Add(10 * time.Millisecond),
	}

	obs.ObserveQuery(context.Background(), q)

	assert.True(t, userObs.called)
	assert.Equal(t, "SELECT * FROM users WHERE id = 1", userObs.query.Statement)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, "my_keyspace.SELECT", span.Name())

	attrMap := make(map[string]string)
	for _, a := range span.Attributes() {
		attrMap[string(a.Key)] = a.Value.AsString()
	}

	assert.Equal(t, "cassandra", attrMap["db.system.name"])
	assert.Equal(t, "SELECT", attrMap["db.operation.name"])
	assert.Equal(t, "my_keyspace", attrMap["db.namespace"])
	assert.Equal(t, "SELECT * FROM users WHERE id = 1", attrMap["db.query.text"])
}

func TestOtelObserver_ObserveQueryWithError(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	obs := newOtelObserver(nil, nil, nil)
	obs.tracer = tp.Tracer(instrumentationName)

	queryErr := errors.New("cassandra connection error")
	q := gocql.ObservedQuery{
		Keyspace:  "test_ks",
		Statement: "INSERT INTO logs (id) VALUES (1)",
		Err:       queryErr,
		Start:     time.Now(),
		End:       time.Now().Add(5 * time.Millisecond),
	}

	obs.ObserveQuery(context.Background(), q)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "cassandra connection error", span.Status().Description)
}

func TestOtelObserver_ObserveBatch(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	userBatch := &dummyUserBatchObserver{}
	obs := newOtelObserver(nil, userBatch, nil)
	obs.tracer = tp.Tracer(instrumentationName)

	b := gocql.ObservedBatch{
		Keyspace:   "batch_ks",
		Statements: []string{"INSERT INTO a VALUES (1)", "UPDATE b SET x = 2"},
		Start:      time.Now(),
		End:        time.Now().Add(15 * time.Millisecond),
	}

	obs.ObserveBatch(context.Background(), b)

	assert.True(t, userBatch.called)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, "batch_ks.BATCH", span.Name())

	attrMap := make(map[string]string)
	for _, a := range span.Attributes() {
		attrMap[string(a.Key)] = a.Value.AsString()
	}

	assert.Equal(t, "cassandra", attrMap["db.system.name"])
	assert.Equal(t, "BATCH", attrMap["db.operation.name"])
	assert.Equal(t, "batch_ks", attrMap["db.namespace"])
	assert.Equal(t, "INSERT INTO a VALUES (1); UPDATE b SET x = 2", attrMap["db.query.text"])
}

func TestOtelObserver_ObserveBatchWithError(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	obs := newOtelObserver(nil, nil, nil)
	obs.tracer = tp.Tracer(instrumentationName)

	batchErr := errors.New("batch execution failed")
	b := gocql.ObservedBatch{
		Keyspace:   "batch_ks",
		Statements: []string{"INSERT INTO a VALUES (1)"},
		Err:        batchErr,
		Start:      time.Now(),
		End:        time.Now().Add(5 * time.Millisecond),
	}

	obs.ObserveBatch(context.Background(), b)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "batch execution failed", span.Status().Description)
}

func TestOtelObserver_ObserveConnect(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	userConn := &dummyUserConnectObserver{}
	obs := newOtelObserver(nil, nil, userConn)
	obs.tracer = tp.Tracer(instrumentationName)

	oc := gocql.ObservedConnect{
		Start: time.Now(),
		End:   time.Now().Add(2 * time.Millisecond),
	}

	obs.ObserveConnect(oc)
	assert.True(t, userConn.called)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, "CONNECT", span.Name())

	attrMap := make(map[string]string)
	for _, a := range span.Attributes() {
		attrMap[string(a.Key)] = a.Value.AsString()
	}

	assert.Equal(t, "cassandra", attrMap["db.system.name"])
	assert.Equal(t, "CONNECT", attrMap["db.operation.name"])
	assert.Equal(t, codes.Ok, span.Status().Code)
}

func TestOtelObserver_ObserveConnectWithError(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	obs := newOtelObserver(nil, nil, nil)
	obs.tracer = tp.Tracer(instrumentationName)

	connectErr := errors.New("dial tcp: connection refused")
	oc := gocql.ObservedConnect{
		Err:   connectErr,
		Start: time.Now(),
		End:   time.Now().Add(2 * time.Millisecond),
	}

	obs.ObserveConnect(oc)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "dial tcp: connection refused", span.Status().Description)
}
