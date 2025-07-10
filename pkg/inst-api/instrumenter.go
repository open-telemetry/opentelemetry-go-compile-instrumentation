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

// Invocation encapsulates the parameters needed for ending instrumentation operations
type Invocation[REQUEST any, RESPONSE any] struct {
	Request        REQUEST
	Response       RESPONSE
	Err            error
	StartTimeStamp time.Time
	EndTimeStamp   time.Time
}

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
	// StartAndEndWithOptions Internal method for creating spans with given start/end timestamps.
	StartAndEndWithOptions(
		parentContext context.Context,
		invocation Invocation[REQUEST, RESPONSE],
		startOptions []trace.SpanStartOption,
		endOptions []trace.SpanEndOption,
	)
	StartAndEnd(
		parentContext context.Context,
		invocation Invocation[REQUEST, RESPONSE],
	)
	// Start Starts a new instrumented operation. The returned context should be propagated along
	// with the operation and passed to the End method when it is finished.
	Start(parentContext context.Context, request REQUEST, options ...trace.SpanStartOption) context.Context
	// End ends an instrumented operation. It is of extreme importance for this method to be always called
	// after Start. Calling Start without later End will result in inaccurate or wrong telemetry and context leaks.
	End(ctx context.Context, invocation Invocation[REQUEST, RESPONSE], options ...trace.SpanEndOption)
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

func (i *InternalInstrumenter[REQUEST, RESPONSE]) StartAndEndWithOptions(
	parentContext context.Context,
	invocation Invocation[REQUEST, RESPONSE],
	startOptions []trace.SpanStartOption,
	endOptions []trace.SpanEndOption,
) {
	ctx := i.doStart(parentContext, invocation.Request, invocation.StartTimeStamp, startOptions...)
	i.End(ctx, invocation, endOptions...)
}

func (i *InternalInstrumenter[REQUEST, RESPONSE]) StartAndEnd(
	parentContext context.Context,
	invocation Invocation[REQUEST, RESPONSE],
) {
	ctx := i.doStart(parentContext, invocation.Request, invocation.StartTimeStamp)
	i.End(ctx, invocation)
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
		//nolint:fatcontext // There will not be so many operation listeners here
		parentContext = listener.OnBeforeStart(parentContext, timestamp)
	}
	// extract span name
	spanName := i.spanNameExtractor.Extract(request)
	spanKind := i.spanKindExtractor.Extract(request)
	options = append(options, trace.WithSpanKind(spanKind), trace.WithTimestamp(timestamp))
	newCtx, span := i.tracer.Start(parentContext, spanName, options...)
	attrs := make([]attribute.KeyValue, 0, defaultAttributesSliceSize)
	currentCtx := newCtx
	for _, extractor := range i.attributesExtractors {
		attrs, currentCtx = extractor.OnStart(currentCtx, attrs, request)
	}
	for _, customizer := range i.contextCustomizers {
		//nolint:fatcontext // There will not be so many customizers here
		currentCtx = customizer.OnStart(currentCtx, request, attrs)
	}
	for _, listener := range i.operationListeners {
		//nolint:fatcontext // There will not be so many operation listeners here
		currentCtx = listener.OnBeforeEnd(currentCtx, attrs, timestamp)
	}
	newCtx = currentCtx
	span.SetAttributes(attrs...)
	return newCtx
}

func (i *InternalInstrumenter[REQUEST, RESPONSE]) End(
	ctx context.Context,
	invocation Invocation[REQUEST, RESPONSE],
	options ...trace.SpanEndOption,
) {
	i.doEnd(ctx, invocation, invocation.EndTimeStamp, options...)
}

func (i *InternalInstrumenter[REQUEST, RESPONSE]) doEnd(
	ctx context.Context,
	invocation Invocation[REQUEST, RESPONSE],
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
	if invocation.Err != nil {
		span.RecordError(invocation.Err)
		span.SetStatus(codes.Error, invocation.Err.Error())
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
	currentCtx := ctx
	for _, extractor := range i.attributesExtractors {
		attrs, currentCtx = extractor.OnEnd(currentCtx, attrs, invocation.Request, invocation.Response, invocation.Err)
	}
	i.spanStatusExtractor.Extract(span, invocation.Request, invocation.Response, invocation.Err)
	span.SetAttributes(attrs...)
	options = append(options, trace.WithTimestamp(timestamp))
	span.End(options...)
	for _, listener := range i.operationListeners {
		listener.OnAfterEnd(currentCtx, attrs, timestamp)
	}
}

func (p *PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE]) ShouldStart(
	parentContext context.Context,
	request REQUEST,
) bool {
	return p.base.ShouldStart(parentContext, request)
}

func (p *PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE]) StartAndEndWithOptions(
	parentContext context.Context,
	invocation Invocation[REQUEST, RESPONSE],
	startOptions []trace.SpanStartOption,
	endOptions []trace.SpanEndOption,
) {
	newCtx := p.base.Start(parentContext, invocation.Request, startOptions...)
	if p.carrierGetter != nil {
		if p.prop != nil {
			p.prop.Inject(newCtx, p.carrierGetter(invocation.Request))
		} else {
			otel.GetTextMapPropagator().Inject(newCtx, p.carrierGetter(invocation.Request))
		}
	}
	p.base.End(newCtx, invocation, endOptions...)
}

func (p *PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE]) StartAndEnd(
	parentContext context.Context,
	invocation Invocation[REQUEST, RESPONSE],
) {
	p.StartAndEndWithOptions(parentContext, invocation, nil, nil)
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
	invocation Invocation[REQUEST, RESPONSE],
	options ...trace.SpanEndOption,
) {
	p.base.End(ctx, invocation, options...)
}

func (p *PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE]) ShouldStart(
	parentContext context.Context,
	request REQUEST,
) bool {
	return p.base.ShouldStart(parentContext, request)
}

func (p *PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE]) StartAndEndWithOptions(
	parentContext context.Context,
	invocation Invocation[REQUEST, RESPONSE],
	startOptions []trace.SpanStartOption,
	endOptions []trace.SpanEndOption,
) {
	var ctx context.Context
	if p.carrierGetter != nil {
		var extracted context.Context
		if p.prop != nil {
			extracted = p.prop.Extract(parentContext, p.carrierGetter(invocation.Request))
		} else {
			extracted = otel.GetTextMapPropagator().Extract(parentContext, p.carrierGetter(invocation.Request))
		}
		ctx = p.base.Start(extracted, invocation.Request, startOptions...)
	} else {
		ctx = parentContext
	}
	p.base.End(ctx, invocation, endOptions...)
}

func (p *PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE]) StartAndEnd(
	parentContext context.Context,
	invocation Invocation[REQUEST, RESPONSE],
) {
	p.StartAndEndWithOptions(parentContext, invocation, nil, nil)
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
	invocation Invocation[REQUEST, RESPONSE],
	options ...trace.SpanEndOption,
) {
	p.base.End(ctx, invocation, options...)
}
