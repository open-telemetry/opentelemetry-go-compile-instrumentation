// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package gocql

import (
	"context"
	"strings"
	"time"

	"github.com/gocql/gocql"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/gocql/gocql"
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

	attrs := []attribute.KeyValue{
		semconv.DBSystemNameCassandra,
		semconv.DBOperationName(opName),
		semconv.DBQueryText(q.Statement),
	}
	if q.Keyspace != "" {
		attrs = append(attrs, semconv.DBNamespace(q.Keyspace))
		attrs = append(attrs, attribute.String("db.cassandra.keyspace", q.Keyspace))
	}
	if q.Host != nil {
		peer := q.Host.Peer()
		if len(peer) > 0 {
			attrs = append(attrs, semconv.ServerAddress(peer.String()))
		}
		if port := q.Host.Port(); port > 0 {
			attrs = append(attrs, semconv.ServerPort(port))
		}
	}

	span.SetAttributes(attrs...)

	if q.Err != nil {
		span.RecordError(q.Err)
		span.SetStatus(codes.Error, q.Err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
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

	stmtText := strings.Join(b.Statements, "; ")

	attrs := []attribute.KeyValue{
		semconv.DBSystemNameCassandra,
		semconv.DBOperationName("BATCH"),
		semconv.DBQueryText(stmtText),
	}
	if b.Keyspace != "" {
		attrs = append(attrs, semconv.DBNamespace(b.Keyspace))
		attrs = append(attrs, attribute.String("db.cassandra.keyspace", b.Keyspace))
	}
	if b.Host != nil {
		peer := b.Host.Peer()
		if len(peer) > 0 {
			attrs = append(attrs, semconv.ServerAddress(peer.String()))
		}
		if port := b.Host.Port(); port > 0 {
			attrs = append(attrs, semconv.ServerPort(port))
		}
	}

	span.SetAttributes(attrs...)

	if b.Err != nil {
		span.RecordError(b.Err)
		span.SetStatus(codes.Error, b.Err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
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

	attrs := []attribute.KeyValue{
		semconv.DBSystemNameCassandra,
		semconv.DBOperationName("CONNECT"),
	}
	if oc.Host != nil {
		peer := oc.Host.Peer()
		if len(peer) > 0 {
			attrs = append(attrs, semconv.ServerAddress(peer.String()))
		}
		if port := oc.Host.Port(); port > 0 {
			attrs = append(attrs, semconv.ServerPort(port))
		}
	}

	span.SetAttributes(attrs...)

	if oc.Err != nil {
		span.RecordError(oc.Err)
		span.SetStatus(codes.Error, oc.Err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
}

func parseOpName(stmt string) string {
	stmt = strings.TrimSpace(stmt)
	if stmt == "" {
		return "SELECT"
	}
	fields := strings.Fields(stmt)
	if len(fields) > 0 {
		return strings.ToUpper(fields[0])
	}
	return "SELECT"
}

// AfterNewCluster is invoked after gocql.NewCluster returns a *ClusterConfig.
// It wraps observers on the returned ClusterConfig for pre-session configuration.
func AfterNewCluster(ictx hook.HookContext, cluster *gocql.ClusterConfig) {
	if cluster == nil || !enabler.Enable() {
		return
	}
	wrapClusterConfig(cluster)
}

// BeforeNewSession is invoked before gocql.NewSession creates a session from ClusterConfig.
// It ensures observers are wrapped even if NewSession is called directly with a custom ClusterConfig.
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
