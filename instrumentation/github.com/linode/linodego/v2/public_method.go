// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/linode/linodego/v2/semconv"
	"go.opentelemetry.io/otelc/pkg/hook"
)

// Public API method hooks: shared beforeAPICall/afterAPICall implement the span
// lifecycle. public_methods_gen.go defines a unique BeforeX/AfterX wrapper per
// Client method (required so otelc //go:linkname stubs do not redeclare the same
// symbol across package files). Rules live in public_methods.otelc.yaml.
// Span names come from HookContext.GetFuncName().

func beforeAPICall(ictx hook.HookContext, params ...interface{}) {
	if !enabler.Enable() {
		return
	}
	initInstrumentation()

	operation := ictx.GetFuncName()
	ctx := context.Background()
	ctxIdx := -1
	for i, p := range params {
		if c, ok := p.(context.Context); ok {
			ctx = c
			ctxIdx = i
			break
		}
	}

	attrs := semconv.LinodegoOperationTraceAttrs(operation)
	ctx, span := tracer.Start(ctx,
		semconv.OperationSpanName(operation),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)

	if ctxIdx >= 0 {
		// Param indices include the receiver at 0; params[0] is the receiver.
		ictx.SetParam(ctxIdx, ctx)
	}

	ictx.SetKeyData(keySpan, span)
	ictx.SetKeyData(keyStart, time.Now())
	ictx.SetKeyData(keyOperation, operation)
	ictx.SetKeyData(keyCtx, ctx)

	logger.Debug("BeforeAPICall", "operation", operation)
}

func afterAPICall(ictx hook.HookContext, err error) {
	if !enabler.Enable() {
		return
	}

	span, ok := ictx.GetKeyData(keySpan).(trace.Span)
	if !ok || span == nil {
		logger.Debug("AfterAPICall: no span from before hook")
		return
	}
	defer span.End()

	operation, _ := ictx.GetKeyData(keyOperation).(string)
	start, _ := ictx.GetKeyData(keyStart).(time.Time)
	ctx, _ := ictx.GetKeyData(keyCtx).(context.Context)
	if ctx == nil {
		ctx = context.Background()
	}

	statusCode := finishSpanWithError(span, err)
	if !start.IsZero() {
		metrics.RecordOperationDuration(ctx, time.Since(start).Seconds(), operation, statusCode)
	}

	logger.Debug("AfterAPICall completed", "operation", operation)
}

func errorFromResults(results ...interface{}) error {
	if len(results) == 0 {
		return nil
	}
	err, _ := results[len(results)-1].(error)
	return err
}

// BeforeAPICall1 instruments methods with 1 parameter after the receiver
// (typically just context.Context).
func BeforeAPICall1(ictx hook.HookContext, recv, p0 interface{}) {
	beforeAPICall(ictx, recv, p0)
}

// BeforeAPICall2 instruments methods with 2 parameters after the receiver.
func BeforeAPICall2(ictx hook.HookContext, recv, p0, p1 interface{}) {
	beforeAPICall(ictx, recv, p0, p1)
}

// BeforeAPICall3 instruments methods with 3 parameters after the receiver.
func BeforeAPICall3(ictx hook.HookContext, recv, p0, p1, p2 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2)
}

// BeforeAPICall4 instruments methods with 4 parameters after the receiver.
func BeforeAPICall4(ictx hook.HookContext, recv, p0, p1, p2, p3 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3)
}

// BeforeAPICall5 instruments methods with 5 parameters after the receiver.
func BeforeAPICall5(ictx hook.HookContext, recv, p0, p1, p2, p3, p4 interface{}) {
	beforeAPICall(ictx, recv, p0, p1, p2, p3, p4)
}

// AfterAPICall1 instruments methods with 1 result (error).
func AfterAPICall1(ictx hook.HookContext, r0 interface{}) {
	afterAPICall(ictx, errorFromResults(r0))
}

// AfterAPICall2 instruments methods with 2 results (T, error).
func AfterAPICall2(ictx hook.HookContext, r0, r1 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1))
}

// AfterAPICall3 instruments methods with 3 results (…, error).
func AfterAPICall3(ictx hook.HookContext, r0, r1, r2 interface{}) {
	afterAPICall(ictx, errorFromResults(r0, r1, r2))
}
