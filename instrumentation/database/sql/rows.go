// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"context"
	"database/sql"
	"time"

	"go.opentelemetry.io/otel/codes"
	otelsemconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otelc/instrumentation/database/sql/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
)

func instrumentQueryEnd(ictx hook.HookContext, rows *sql.Rows, err error) {
	state, ok := ictx.GetData().(*queryState)
	trackRows := ok && rows != nil && err == nil && state.span.IsRecording()
	instrumentEnd(ictx, err)
	if !trackRows {
		return
	}

	// Retain the query's span context, not the caller's context.
	ctx := trace.ContextWithSpanContext(context.Background(), state.span.SpanContext())
	start := time.Now()
	attrs := semconv.DbClientRequestTraceAttrs(state.req)
	var count int64
	rows.OtelTrackRows(func() { count++ }, func(closeErr error) {
		end := time.Now()
		// Creating the span at closure avoids changing the implicit GLS parent between reads.
		_, span := tracer.Start(ctx, "sql.rows",
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithTimestamp(start),
			trace.WithAttributes(attrs...),
			trace.WithAttributes(otelsemconv.DBResponseReturnedRowsKey.Int64(count)),
		)
		if closeErr != nil {
			span.RecordError(closeErr)
			span.SetStatus(codes.Error, closeErr.Error())
		}
		span.End(trace.WithTimestamp(end))
	})
}
