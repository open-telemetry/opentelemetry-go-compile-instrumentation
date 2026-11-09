// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package clickhouse traces the native API of
// github.com/ClickHouse/clickhouse-go/v2.
//
// A driver.Conn returned by clickhouse.Open does not go through
// database/sql. Each call on it (Query, QueryRow, Select, Exec, AsyncInsert,
// Ping, PrepareBatch) and each Send of a batch it prepared gets one CLIENT
// span, whose parent is the span in the call's context.
package clickhouse

import (
	"context"
	"errors"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	chsemconv "go.opentelemetry.io/otelc/instrumentation/github.com/ClickHouse/clickhouse-go/v2/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

const (
	instrumentationName = "go.opentelemetry.io/otelc/instrumentation/github.com/ClickHouse/clickhouse-go/v2"
	instrumentationKey  = "CLICKHOUSE"

	// ctxParamIndex is the index of the context parameter of the hooked
	// *clickhouse methods; index 0 is the receiver.
	ctxParamIndex = 1
)

func enabled() bool { return runtime.Instrumented(instrumentationKey) }

// receiverAccessors read the unexported fields of the hooked receivers.
type receiverAccessors struct {
	options      func(conn any) *ch.Options
	batchContext func(batch any) context.Context
}

// accessors is set once, at init, by accessors.go, which is added to package
// clickhouse at compile time. Until then it reads nothing.
//
//nolint:gochecknoglobals // set through go:linkname, see setAccessors
var accessors = receiverAccessors{
	options:      func(any) *ch.Options { return nil },
	batchContext: func(any) context.Context { return nil },
}

// setAccessors is called through go:linkname by accessors.go.
func setAccessors(options func(any) *ch.Options, batchContext func(any) context.Context) {
	if options != nil {
		accessors.options = options
	}
	if batchContext != nil {
		accessors.batchContext = batchContext
	}
}

func tracer() trace.Tracer {
	return otel.GetTracerProvider().Tracer(instrumentationName,
		trace.WithInstrumentationVersion(runtime.ModuleVersion()))
}

// inSelectKey marks the context Select passes to the Query it calls, so that
// Query is not traced inside the Select span.
type inSelectKey struct{}

// batchKey carries, in the context of a batch prepared on a native conn, what
// its Send span needs. Only marked batches are traced at Send, so batches
// prepared through database/sql (clickhouse.OpenDB) are not traced twice.
type batchKey struct{}

type batchInfo struct {
	server chsemconv.Server
	query  string
}

// BeforeQuery runs before (*clickhouse).Query.
//
//nolint:revive // a hook takes the hooked method's receiver before its context
func BeforeQuery(ictx hook.HookContext, conn any, ctx context.Context, query string, _ ...any) {
	if ctx != nil && ctx.Value(inSelectKey{}) != nil {
		return
	}
	start(ctx, ictx, conn, query)
}

// AfterQuery ends the Query span. The span ends when Query returns, before
// the caller reads the rows.
func AfterQuery(ictx hook.HookContext, _ driver.Rows, err error) { finish(ictx, err) }

// BeforeQueryRow runs before (*clickhouse).QueryRow.
//
//nolint:revive // a hook takes the hooked method's receiver before its context
func BeforeQueryRow(ictx hook.HookContext, conn any, ctx context.Context, query string, _ ...any) {
	start(ctx, ictx, conn, query)
}

// AfterQueryRow ends the QueryRow span with the row's error.
func AfterQueryRow(ictx hook.HookContext, row driver.Row) {
	var err error
	if row != nil {
		err = row.Err()
	}
	finish(ictx, err)
}

// BeforeSelect runs before (*clickhouse).Select. It passes the Select span
// and a marker in the context, so the Query that Select calls is not traced.
//
//nolint:revive // a hook takes the hooked method's receiver before its context
func BeforeSelect(ictx hook.HookContext, conn any, ctx context.Context, _ any, query string, _ ...any) {
	if !enabled() {
		return
	}
	ctx, span := startSpan(ctx, chsemconv.OperationName(query), query, serverOf(conn))
	ictx.SetData(span)
	ictx.SetParam(ctxParamIndex, context.WithValue(ctx, inSelectKey{}, true))
}

// AfterSelect ends the Select span.
func AfterSelect(ictx hook.HookContext, err error) { finish(ictx, err) }

// BeforeExec runs before (*clickhouse).Exec.
//
//nolint:revive // a hook takes the hooked method's receiver before its context
func BeforeExec(ictx hook.HookContext, conn any, ctx context.Context, query string, _ ...any) {
	start(ctx, ictx, conn, query)
}

// AfterExec ends the Exec span.
func AfterExec(ictx hook.HookContext, err error) { finish(ictx, err) }

// BeforeAsyncInsert runs before (*clickhouse).AsyncInsert.
//
//nolint:revive // a hook takes the hooked method's receiver before its context
func BeforeAsyncInsert(ictx hook.HookContext, conn any, ctx context.Context, query string, _ bool, _ ...any) {
	start(ctx, ictx, conn, query)
}

// AfterAsyncInsert ends the AsyncInsert span.
func AfterAsyncInsert(ictx hook.HookContext, err error) { finish(ictx, err) }

// BeforePing runs before (*clickhouse).Ping.
//
//nolint:revive // a hook takes the hooked method's receiver before its context
func BeforePing(ictx hook.HookContext, conn any, ctx context.Context) {
	if !enabled() {
		return
	}
	_, span := startSpan(ctx, "PING", "", serverOf(conn))
	ictx.SetData(span)
}

// AfterPing ends the Ping span.
func AfterPing(ictx hook.HookContext, err error) { finish(ictx, err) }

// BeforePrepareBatch runs before (*clickhouse).PrepareBatch. Besides the
// span of the call, it marks the context the batch keeps, so that the
// batch's Send is traced too. The marked context is the caller's, so the
// Send span is a sibling of the PrepareBatch span, not its child.
//
//nolint:revive // a hook takes the hooked method's receiver before its context
func BeforePrepareBatch(
	ictx hook.HookContext,
	conn any,
	ctx context.Context,
	query string,
	_ ...driver.PrepareBatchOption,
) {
	// clickhouse-go cannot run with a nil context; there is nothing to trace.
	if !enabled() || ctx == nil {
		return
	}
	server := serverOf(conn)
	_, span := startSpan(ctx, chsemconv.OperationName(query), query, server)
	ictx.SetData(span)
	info := &batchInfo{server: server, query: query}
	ictx.SetParam(ctxParamIndex, context.WithValue(ctx, batchKey{}, info))
}

// AfterPrepareBatch ends the PrepareBatch span.
func AfterPrepareBatch(ictx hook.HookContext, _ driver.Batch, err error) { finish(ictx, err) }

// BeforeBatchSend runs before (*batch).Send and (*httpBatch).Send. It traces
// only batches prepared on a native conn: one span for the whole batch,
// whatever the number of rows appended to it.
func BeforeBatchSend(ictx hook.HookContext, b any) {
	ctx := accessors.batchContext(b)
	if ctx == nil {
		return
	}
	info, ok := ctx.Value(batchKey{}).(*batchInfo)
	if !ok || !enabled() {
		return
	}
	_, span := startSpan(ctx, chsemconv.OperationName(info.query), info.query, info.server)
	ictx.SetData(span)
}

// AfterBatchSend ends the Send span.
func AfterBatchSend(ictx hook.HookContext, err error) { finish(ictx, err) }

func start(ctx context.Context, ictx hook.HookContext, conn any, query string) {
	if !enabled() {
		return
	}
	_, span := startSpan(ctx, chsemconv.OperationName(query), query, serverOf(conn))
	ictx.SetData(span)
}

// startSpan starts the CLIENT span of a call. The Before hook stores it with
// ictx.SetData and the After hook of the same call ends it, see finish.
func startSpan(ctx context.Context, op, query string, server chsemconv.Server) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	//nolint:spancheck // ended by the After hook of the call, see finish
	return tracer().Start(ctx, chsemconv.SpanName(op, server.Namespace),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(chsemconv.ClientTraceAttrs(chsemconv.Request{
			Operation: op,
			Query:     query,
			Server:    server,
		})...))
}

func serverOf(conn any) chsemconv.Server {
	opt := accessors.options(conn)
	if opt == nil {
		return chsemconv.Server{}
	}
	return chsemconv.ServerFromAddrs(opt.Addr, opt.Auth.Database)
}

func finish(ictx hook.HookContext, err error) {
	span, ok := ictx.GetData().(trace.Span)
	if !ok || span == nil {
		return
	}
	if err != nil {
		var code int32
		if exc, isExc := errors.AsType[*ch.Exception](err); isExc {
			code = exc.Code
		}
		span.SetAttributes(chsemconv.ErrorAttrs(code)...)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
