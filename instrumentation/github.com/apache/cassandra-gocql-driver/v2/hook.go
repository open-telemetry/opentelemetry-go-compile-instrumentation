// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package gocql

import (
	"context"
	"strings"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	gocqlsemconv "go.opentelemetry.io/otelc/instrumentation/github.com/apache/cassandra-gocql-driver/v2/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/apache/cassandra-gocql-driver/v2"
	instrumentationKey  = "GOCQL"
)

type gocqlEnabler struct{}

func (g gocqlEnabler) Enable() bool {
	return runtime.Instrumented(instrumentationKey)
}

var enabler = gocqlEnabler{}

type otelObserver struct {
	userQuery   gocql.QueryObserver
	userBatch   gocql.BatchObserver
	userConnect gocql.ConnectObserver
	tracer      trace.Tracer
}

func newOtelObserver(userQuery gocql.QueryObserver, userBatch gocql.BatchObserver, userConnect gocql.ConnectObserver) *otelObserver {
	return &otelObserver{
		userQuery:   userQuery,
		userBatch:   userBatch,
		userConnect: userConnect,
		tracer:      otel.GetTracerProvider().Tracer(instrumentationName),
	}
}

func (o *otelObserver) ObserveQuery(ctx context.Context, q gocql.ObservedQuery) {
	if enabler.Enable() {
		o.recordQuerySpan(ctx, q)
	}
	if o.userQuery != nil {
		o.userQuery.ObserveQuery(ctx, q)
	}
}

func (o *otelObserver) ObserveBatch(ctx context.Context, b gocql.ObservedBatch) {
	if enabler.Enable() {
		o.recordBatchSpan(ctx, b)
	}
	if o.userBatch != nil {
		o.userBatch.ObserveBatch(ctx, b)
	}
}

func (o *otelObserver) ObserveConnect(oc gocql.ObservedConnect) {
	if enabler.Enable() {
		o.recordConnectSpan(oc)
	}
	if o.userConnect != nil {
		o.userConnect.ObserveConnect(oc)
	}
}

func (o *otelObserver) recordQuerySpan(ctx context.Context, q gocql.ObservedQuery) {
	if ctx == nil {
		ctx = context.Background()
	}
	opName := parseOpName(q.Statement)
	spanName := opName
	if q.Keyspace != "" {
		spanName = q.Keyspace + "." + opName
	}

	startTime := q.Start
	if startTime.IsZero() {
		startTime = time.Now()
	}
	endTime := q.End
	if endTime.IsZero() {
		endTime = time.Now()
	}

	_, span := o.tracer.Start(ctx, spanName,
		trace.WithTimestamp(startTime),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End(trace.WithTimestamp(endTime))

	req := gocqlsemconv.QueryRequest{
		OpName:    opName,
		Statement: q.Statement,
		Keyspace:  q.Keyspace,
	}
	if q.Host != nil {
		req.Host = q.Host.ConnectAddress()
		req.Port = q.Host.Port()
	}
	span.SetAttributes(gocqlsemconv.QueryClientTraceAttrs(req)...)

	if q.Err != nil {
		span.RecordError(q.Err)
		span.SetStatus(codes.Error, q.Err.Error())
	}
}

func (o *otelObserver) recordBatchSpan(ctx context.Context, b gocql.ObservedBatch) {
	if ctx == nil {
		ctx = context.Background()
	}
	spanName := "BATCH"
	if b.Keyspace != "" {
		spanName = b.Keyspace + ".BATCH"
	}

	startTime := b.Start
	if startTime.IsZero() {
		startTime = time.Now()
	}
	endTime := b.End
	if endTime.IsZero() {
		endTime = time.Now()
	}

	_, span := o.tracer.Start(ctx, spanName,
		trace.WithTimestamp(startTime),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End(trace.WithTimestamp(endTime))

	req := gocqlsemconv.BatchRequest{
		Statement: strings.Join(b.Statements, "; "),
		BatchSize: len(b.Statements),
		Keyspace:  b.Keyspace,
	}
	if b.Host != nil {
		req.Host = b.Host.ConnectAddress()
		req.Port = b.Host.Port()
	}
	span.SetAttributes(gocqlsemconv.BatchClientTraceAttrs(req)...)

	if b.Err != nil {
		span.RecordError(b.Err)
		span.SetStatus(codes.Error, b.Err.Error())
	}
}

func (o *otelObserver) recordConnectSpan(oc gocql.ObservedConnect) {
	startTime := oc.Start
	if startTime.IsZero() {
		startTime = time.Now()
	}
	endTime := oc.End
	if endTime.IsZero() {
		endTime = time.Now()
	}

	_, span := o.tracer.Start(context.Background(), "CONNECT",
		trace.WithTimestamp(startTime),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End(trace.WithTimestamp(endTime))

	req := gocqlsemconv.ConnectRequest{}
	if oc.Host != nil {
		req.Host = oc.Host.ConnectAddress()
		req.Port = oc.Host.Port()
	}
	span.SetAttributes(gocqlsemconv.ConnectClientTraceAttrs(req)...)

	if oc.Err != nil {
		span.RecordError(oc.Err)
		span.SetStatus(codes.Error, oc.Err.Error())
	}
}

func parseOpName(stmt string) string {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" {
		return ""
	}
	fields := strings.Fields(stmt)
	if len(fields) > 0 {
		return strings.ToUpper(fields[0])
	}
	return ""
}

// BeforeNewSession is invoked before gocql.NewSession creates a session from a
// ClusterConfig. This is the single entry point for wrapping the observers:
// ClusterConfig.CreateSession() delegates to NewSession(*cfg), so every session,
// however it is constructed, passes through here with the caller's final
// observer configuration.
func BeforeNewSession(ictx hook.HookContext, cfg gocql.ClusterConfig) {
	if !enabler.Enable() {
		return
	}
	wrapClusterConfig(&cfg)
	ictx.SetParam(0, cfg)
}

func wrapClusterConfig(cfg *gocql.ClusterConfig) {
	if cfg == nil {
		return
	}
	if _, ok := cfg.QueryObserver.(*otelObserver); ok {
		return
	}
	obs := newOtelObserver(cfg.QueryObserver, cfg.BatchObserver, cfg.ConnectObserver)
	cfg.QueryObserver = obs
	cfg.BatchObserver = obs
	cfg.ConnectObserver = obs
}
