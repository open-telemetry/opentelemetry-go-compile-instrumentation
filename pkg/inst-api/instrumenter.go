// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Instrumenter encapsulates the entire logic for gathering telemetry, from collecting
// the data, to starting and ending spans, to recording values using metrics instruments.
// Instrumenter is called at the start and the end of a request/response lifecycle.
//
// The interface supports generic REQUEST and RESPONSE types, allowing for type-safe
// instrumentation of various operation types. It provides methods for both immediate
// instrumentation (StartAndEnd) and deferred instrumentation (Start/End pairs).
//
// Usage patterns:
//   - For operations with known duration: use StartAndEnd or StartAndEndWithOptions
//   - For ongoing operations: use Start to begin instrumentation, then End when complete
//   - Always call End after Start to prevent context leaks and ensure accurate telemetry
//
// The Instrumenter handles span creation, attribute extraction, status setting, and
// propagation of OpenTelemetry context throughout the operation lifecycle.
//
// For more detailed information about using it see:
// https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/blob/main/_docs/api-design-and-project-structure.md
type Instrumenter[REQUEST any, RESPONSE any] interface {
	// ShouldStart Determines whether the operation should be instrumented for telemetry or not.
	// Returns true by default.
	ShouldStart(parentContext context.Context, request REQUEST) bool
	// StartAndEnd Internal method for creating spans with given start/end timestamps.
	StartAndEnd(
		parentContext context.Context,
		request REQUEST,
		response RESPONSE,
		err error,
		startTime, endTime time.Time,
	)
	// StartAndEndWithOptions Internal method for creating spans with given start/end timestamps and other options.
	StartAndEndWithOptions(
		parentContext context.Context,
		request REQUEST,
		response RESPONSE,
		err error,
		startTime, endTime time.Time,
		startOptions []trace.SpanStartOption,
		endOptions []trace.SpanEndOption,
	)
	// Start Starts a new instrumented operation. The returned context should be propagated along
	// with the operation and passed to the End method when it is finished.
	Start(parentContext context.Context, request REQUEST, options ...trace.SpanStartOption) context.Context
	// End ends an instrumented operation. It is of extreme importance for this method to be always called
	// after Start. Calling Start without later End will result in inaccurate or wrong telemetry and context leaks.
	End(ctx context.Context, request REQUEST, response RESPONSE, err error, options ...trace.SpanEndOption)
}

type InternalInstrumenter[REQUEST any, RESPONSE any] struct {
	enabler              InstrumentEnabler
	spanNameExtractor    SpanNameExtractor[REQUEST]
	spanKindExtractor    SpanKindExtractor[REQUEST]
	spanStatusExtractor  SpanStatusExtractor[REQUEST, RESPONSE]
	attributesExtractors []AttributesExtractor[REQUEST, RESPONSE]
	operationListeners   []OperationListener
	contextCustomizers   []ContextCustomizer[REQUEST]
	tracer               trace.Tracer
	instVersion          string
	attributesPool       *sync.Pool
}

// PropagatingToDownstreamInstrumenter do instrumentation and propagate the context to downstream.
// e.g: http-client, rpc-client, message-producer, etc.

type PropagatingToDownstreamInstrumenter[REQUEST any, RESPONSE any] struct {
	carrierGetter func(REQUEST) propagation.TextMapCarrier
	prop          propagation.TextMapPropagator
	base          InternalInstrumenter[REQUEST, RESPONSE]
}

// PropagatingFromUpstreamInstrumenter extract context from remote first, and then do instrumentation.
// e.g: http-server, rpc-server, message-consumer, etc.

type PropagatingFromUpstreamInstrumenter[REQUEST any, RESPONSE any] struct {
	carrierGetter func(REQUEST) propagation.TextMapCarrier
	prop          propagation.TextMapPropagator
	base          InternalInstrumenter[REQUEST, RESPONSE]
}

const defaultAttributesSliceSize = 25

func (*InternalInstrumenter[REQUEST, RESPONSE]) ShouldStart(parentContext context.Context, request REQUEST) bool {
	// TODO: Here you can add some custom logic to determine whether the instrumentation logic is executed or not.
	_ = parentContext
	_ = request
	return true
}

func (i *InternalInstrumenter[REQUEST, RESPONSE]) StartAndEnd(
	parentContext context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	startTime, endTime time.Time,
) {
	ctx := i.doStart(parentContext, request, startTime)
	i.doEnd(ctx, request, response, err, endTime)
}

func (i *InternalInstrumenter[REQUEST, RESPONSE]) StartAndEndWithOptions(
	parentContext context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	startTime, endTime time.Time,
	startOptions []trace.SpanStartOption,
	endOptions []trace.SpanEndOption,
) {
	ctx := i.doStart(parentContext, request, startTime, startOptions...)
	i.doEnd(ctx, request, response, err, endTime, endOptions...)
}

func (i *InternalInstrumenter[REQUEST, RESPONSE]) Start(
	parentContext context.Context,
	request REQUEST,
	options ...trace.SpanStartOption,
) context.Context {
	return i.doStart(parentContext, request, time.Now(), options...)
}

func (i *InternalInstrumenter[REQUEST, RESPONSE]) doStart(
	parentContext context.Context,
	request REQUEST,
	timestamp time.Time,
	options ...trace.SpanStartOption,
) context.Context {
	if i.enabler != nil && !i.enabler.Enable() {
		return parentContext
	}
	for _, listener := range i.operationListeners {
		parentContext = listener.OnBeforeStart(parentContext, timestamp)
	}
	// extract span name
	spanName := i.spanNameExtractor.Extract(request)
	spanKind := i.spanKindExtractor.Extract(request)
	options = append(options, trace.WithSpanKind(spanKind), trace.WithTimestamp(timestamp))
	newCtx, span := i.tracer.Start(parentContext, spanName, options...)
	attrs := make([]attribute.KeyValue, 0, defaultAttributesSliceSize)
	for _, extractor := range i.attributesExtractors {
		attrs, newCtx = extractor.OnStart(newCtx, attrs, request)
	}
	for _, customizer := range i.contextCustomizers {
		newCtx = customizer.OnStart(newCtx, request, attrs)
	}
	for _, listener := range i.operationListeners {
		newCtx = listener.OnBeforeEnd(newCtx, attrs, timestamp)
	}
	span.SetAttributes(attrs...)
	return newCtx
}

func (i *InternalInstrumenter[REQUEST, RESPONSE]) End(
	ctx context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	options ...trace.SpanEndOption,
) {
	i.doEnd(ctx, request, response, err, time.Now(), options...)
}

func (i *InternalInstrumenter[REQUEST, RESPONSE]) doEnd(
	ctx context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	timestamp time.Time,
	options ...trace.SpanEndOption,
) {
	if i.enabler != nil && !i.enabler.Enable() {
		return
	}
	for _, listener := range i.operationListeners {
		listener.OnAfterStart(ctx, timestamp)
	}
	span := trace.SpanFromContext(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	// Initialize pool if not already initialized
	if i.attributesPool == nil {
		i.attributesPool = &sync.Pool{
			New: func() any {
				s := make([]attribute.KeyValue, 0, defaultAttributesSliceSize)
				return &s
			},
		}
	}

	attrsPtr, _ := i.attributesPool.Get().(*[]attribute.KeyValue)
	var attrs []attribute.KeyValue
	if attrsPtr != nil {
		attrs = *attrsPtr
	} else {
		attrs = make([]attribute.KeyValue, 0, defaultAttributesSliceSize)
	}
	defer func() {
		attrs = attrs[:0]
		i.attributesPool.Put(&attrs)
	}()
	for _, extractor := range i.attributesExtractors {
		attrs, ctx = extractor.OnEnd(ctx, attrs, request, response, err)
	}
	i.spanStatusExtractor.Extract(span, request, response, err)
	span.SetAttributes(attrs...)
	options = append(options, trace.WithTimestamp(timestamp))
	span.End(options...)
	for _, listener := range i.operationListeners {
		listener.OnAfterEnd(ctx, attrs, timestamp)
	}
}

func (p *PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE]) ShouldStart(
	parentContext context.Context,
	request REQUEST,
) bool {
	return p.base.ShouldStart(parentContext, request)
}

func (p *PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE]) StartAndEnd(
	parentContext context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	startTime, endTime time.Time,
) {
	newCtx := p.base.doStart(parentContext, request, startTime)
	if p.carrierGetter != nil {
		if p.prop != nil {
			p.prop.Inject(newCtx, p.carrierGetter(request))
		} else {
			otel.GetTextMapPropagator().Inject(newCtx, p.carrierGetter(request))
		}
	}
	p.base.doEnd(newCtx, request, response, err, endTime)
}

func (p *PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE]) StartAndEndWithOptions(
	parentContext context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	startTime, endTime time.Time,
	startOptions []trace.SpanStartOption,
	endOptions []trace.SpanEndOption,
) {
	newCtx := p.base.doStart(parentContext, request, startTime, startOptions...)
	if p.carrierGetter != nil {
		if p.prop != nil {
			p.prop.Inject(newCtx, p.carrierGetter(request))
		} else {
			otel.GetTextMapPropagator().Inject(newCtx, p.carrierGetter(request))
		}
	}
	p.base.doEnd(newCtx, request, response, err, endTime, endOptions...)
}

func (p *PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE]) Start(
	parentContext context.Context,
	request REQUEST,
	options ...trace.SpanStartOption,
) context.Context {
	newCtx := p.base.Start(parentContext, request, options...)
	if p.carrierGetter != nil {
		if p.prop != nil {
			p.prop.Inject(newCtx, p.carrierGetter(request))
		} else {
			otel.GetTextMapPropagator().Inject(newCtx, p.carrierGetter(request))
		}
	}
	return newCtx
}

func (p *PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE]) End(
	ctx context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	options ...trace.SpanEndOption,
) {
	p.base.End(ctx, request, response, err, options...)
}

func (p *PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE]) ShouldStart(
	parentContext context.Context,
	request REQUEST,
) bool {
	return p.base.ShouldStart(parentContext, request)
}

func (p *PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE]) StartAndEnd(
	parentContext context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	startTime, endTime time.Time,
) {
	var ctx context.Context
	if p.carrierGetter != nil {
		var extracted context.Context
		if p.prop != nil {
			extracted = p.prop.Extract(parentContext, p.carrierGetter(request))
		} else {
			extracted = otel.GetTextMapPropagator().Extract(parentContext, p.carrierGetter(request))
		}
		ctx = p.base.doStart(extracted, request, startTime)
	} else {
		ctx = parentContext
	}
	p.base.doEnd(ctx, request, response, err, endTime)
}

func (p *PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE]) StartAndEndWithOptions(
	parentContext context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	startTime, endTime time.Time,
	startOptions []trace.SpanStartOption,
	endOptions []trace.SpanEndOption,
) {
	var ctx context.Context
	if p.carrierGetter != nil {
		var extracted context.Context
		if p.prop != nil {
			extracted = p.prop.Extract(parentContext, p.carrierGetter(request))
		} else {
			extracted = otel.GetTextMapPropagator().Extract(parentContext, p.carrierGetter(request))
		}
		ctx = p.base.doStart(extracted, request, startTime, startOptions...)
	} else {
		ctx = parentContext
	}
	p.base.doEnd(ctx, request, response, err, endTime, endOptions...)
}

func (p *PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE]) Start(
	parentContext context.Context,
	request REQUEST,
	options ...trace.SpanStartOption,
) context.Context {
	if p.carrierGetter != nil {
		var extracted context.Context
		if p.prop != nil {
			extracted = p.prop.Extract(parentContext, p.carrierGetter(request))
		} else {
			extracted = otel.GetTextMapPropagator().Extract(parentContext, p.carrierGetter(request))
		}
		return p.base.Start(extracted, request, options...)
	}
	return parentContext
}

func (p *PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE]) End(
	ctx context.Context,
	request REQUEST,
	response RESPONSE,
	err error,
	options ...trace.SpanEndOption,
) {
	p.base.End(ctx, request, response, err, options...)
}
