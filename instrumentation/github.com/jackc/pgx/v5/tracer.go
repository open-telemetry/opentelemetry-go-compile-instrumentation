// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pgx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/jackc/pgx/v5"
	instrumentationKey  = "PGX"
)

type tracer struct {
	tr trace.Tracer
}

func newTracer() *tracer {
	return &tracer{
		tr: otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(runtime.ModuleVersion()),
		),
	}
}

// operationName returns the uppercased first SQL token (e.g. SELECT, INSERT, UPDATE).
func operationName(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return ""
	}
	if i := strings.IndexFunc(trimmed, unicode.IsSpace); i != -1 {
		trimmed = trimmed[:i]
	}
	return strings.ToUpper(trimmed)
}

// connConfigFunc allows extracting connection configuration, mockable for unit testing.
var connConfigFunc = func(conn *pgx.Conn) *pgx.ConnConfig {
	if conn == nil {
		return nil
	}
	return conn.Config()
}

func (t *tracer) connAttributes(conn *pgx.Conn) []attribute.KeyValue {
	cfg := connConfigFunc(conn)
	if cfg == nil {
		return []attribute.KeyValue{semconv.DBSystemNamePostgreSQL}
	}

	attrs := make([]attribute.KeyValue, 0, 6)
	attrs = append(attrs, semconv.DBSystemNamePostgreSQL)

	if cfg.Database != "" {
		attrs = append(attrs, semconv.DBNamespace(cfg.Database))
	}
	if cfg.User != "" {
		attrs = append(attrs, attribute.String("db.user", cfg.User))
	}
	if cfg.Host != "" {
		host := cfg.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		attrs = append(attrs, semconv.ServerAddress(host))
		attrs = append(attrs, semconv.NetworkTransportTCP)
	}
	if cfg.Port > 0 {
		attrs = append(attrs, semconv.ServerPort(int(cfg.Port)))
	}

	return attrs
}

func (t *tracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !enabler.Enable() {
		return ctx
	}

	initInstrumentation()

	cfg := connConfigFunc(conn)
	op := operationName(data.SQL)
	spanName := op
	if spanName == "" {
		spanName = "DB Query"
	}
	if cfg != nil && cfg.Database != "" {
		spanName = fmt.Sprintf("%s %s", spanName, cfg.Database)
	}

	attrs := t.connAttributes(conn)
	if data.SQL != "" {
		attrs = append(attrs, semconv.DBQueryText(data.SQL))
	}
	if op != "" {
		attrs = append(attrs, semconv.DBOperationName(op))
	}

	ctx, _ = t.tr.Start(
		ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx
}

func (t *tracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if !enabler.Enable() {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}

	span.End()
}

func (t *tracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	if !enabler.Enable() {
		return ctx
	}

	initInstrumentation()

	cfg := connConfigFunc(conn)
	spanName := "BATCH"
	if cfg != nil && cfg.Database != "" {
		spanName = fmt.Sprintf("BATCH %s", cfg.Database)
	}

	attrs := t.connAttributes(conn)
	attrs = append(attrs, semconv.DBOperationName("BATCH"))

	ctx, _ = t.tr.Start(
		ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx
}

func (t *tracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}
}

func (t *tracer) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	if !enabler.Enable() {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}

	span.End()
}

func (t *tracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	if !enabler.Enable() {
		return ctx
	}

	initInstrumentation()

	cfg := connConfigFunc(conn)
	spanName := "COPY"
	if cfg != nil && cfg.Database != "" {
		spanName = fmt.Sprintf("COPY %s", cfg.Database)
	}

	attrs := t.connAttributes(conn)
	attrs = append(attrs, semconv.DBOperationName("COPY"))
	if len(data.TableName) > 0 {
		attrs = append(attrs, attribute.String("db.collection.name", data.TableName.Sanitize()))
	}

	ctx, _ = t.tr.Start(
		ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx
}

func (t *tracer) TraceCopyFromEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromEndData) {
	if !enabler.Enable() {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}

	span.End()
}

func (t *tracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	if !enabler.Enable() {
		return ctx
	}

	initInstrumentation()

	cfg := connConfigFunc(conn)
	spanName := "PREPARE"
	if cfg != nil && cfg.Database != "" {
		spanName = fmt.Sprintf("PREPARE %s", cfg.Database)
	}

	attrs := t.connAttributes(conn)
	attrs = append(attrs, semconv.DBOperationName("PREPARE"))
	if data.SQL != "" {
		attrs = append(attrs, semconv.DBQueryText(data.SQL))
	}
	if data.Name != "" {
		attrs = append(attrs, attribute.String("db.prepared_statement.name", data.Name))
	}

	ctx, _ = t.tr.Start(
		ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx
}

func (t *tracer) TracePrepareEnd(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareEndData) {
	if !enabler.Enable() {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}

	span.End()
}

func (t *tracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	return ctx
}

func (t *tracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
}

// chainedTracer chains multiple tracers so custom user tracers continue to fire.
type chainedTracer struct {
	tracers []any
}

func chainTracer(existing any, otelTracer any) any {
	if existing == nil {
		return otelTracer
	}
	if ct, ok := existing.(*chainedTracer); ok {
		return &chainedTracer{tracers: append(ct.tracers, otelTracer)}
	}
	return &chainedTracer{tracers: []any{existing, otelTracer}}
}

func (c *chainedTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for _, tr := range c.tracers {
		if qt, ok := tr.(pgx.QueryTracer); ok {
			ctx = qt.TraceQueryStart(ctx, conn, data)
		}
	}
	return ctx
}

func (c *chainedTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	for _, tr := range c.tracers {
		if qt, ok := tr.(pgx.QueryTracer); ok {
			qt.TraceQueryEnd(ctx, conn, data)
		}
	}
}

func (c *chainedTracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	for _, tr := range c.tracers {
		if bt, ok := tr.(pgx.BatchTracer); ok {
			ctx = bt.TraceBatchStart(ctx, conn, data)
		}
	}
	return ctx
}

func (c *chainedTracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	for _, tr := range c.tracers {
		if bt, ok := tr.(pgx.BatchTracer); ok {
			bt.TraceBatchQuery(ctx, conn, data)
		}
	}
}

func (c *chainedTracer) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	for _, tr := range c.tracers {
		if bt, ok := tr.(pgx.BatchTracer); ok {
			bt.TraceBatchEnd(ctx, conn, data)
		}
	}
}

func (c *chainedTracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	for _, tr := range c.tracers {
		if ct, ok := tr.(pgx.CopyFromTracer); ok {
			ctx = ct.TraceCopyFromStart(ctx, conn, data)
		}
	}
	return ctx
}

func (c *chainedTracer) TraceCopyFromEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromEndData) {
	for _, tr := range c.tracers {
		if ct, ok := tr.(pgx.CopyFromTracer); ok {
			ct.TraceCopyFromEnd(ctx, conn, data)
		}
	}
}

func (c *chainedTracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	for _, tr := range c.tracers {
		if pt, ok := tr.(pgx.PrepareTracer); ok {
			ctx = pt.TracePrepareStart(ctx, conn, data)
		}
	}
	return ctx
}

func (c *chainedTracer) TracePrepareEnd(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareEndData) {
	for _, tr := range c.tracers {
		if pt, ok := tr.(pgx.PrepareTracer); ok {
			pt.TracePrepareEnd(ctx, conn, data)
		}
	}
}

func (c *chainedTracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	for _, tr := range c.tracers {
		if cnt, ok := tr.(pgx.ConnectTracer); ok {
			ctx = cnt.TraceConnectStart(ctx, data)
		}
	}
	return ctx
}

func (c *chainedTracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	for _, tr := range c.tracers {
		if cnt, ok := tr.(pgx.ConnectTracer); ok {
			cnt.TraceConnectEnd(ctx, data)
		}
	}
}
