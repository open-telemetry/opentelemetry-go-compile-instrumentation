// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/trace"
)

// TODO: add route updater here, now we do not support such controller layer to update route.

type InstrumentEnabler interface {
	Enable() bool
}

type defaultInstrumentEnabler struct {
}

func NewDefaultInstrumentEnabler() InstrumentEnabler {
	return &defaultInstrumentEnabler{}
}

func (a *defaultInstrumentEnabler) Enable() bool {
	return true
}

type Builder[REQUEST any, RESPONSE any] struct {
	Enabler              InstrumentEnabler
	SpanNameExtractor    SpanNameExtractor[REQUEST]
	SpanKindExtractor    SpanKindExtractor[REQUEST]
	SpanStatusExtractor  SpanStatusExtractor[REQUEST, RESPONSE]
	AttributesExtractors []AttributesExtractor[REQUEST, RESPONSE]
	OperationListeners   []OperationListener
	ContextCustomizers   []ContextCustomizer[REQUEST]
	SpanSuppressor       SpanSuppressor
	InstVersion          string
	Scope                instrumentation.Scope
}

func (b *Builder[REQUEST, RESPONSE]) Init() *Builder[REQUEST, RESPONSE] {
	b.Enabler = &defaultInstrumentEnabler{}
	b.AttributesExtractors = make([]AttributesExtractor[REQUEST, RESPONSE], 0)
	b.ContextCustomizers = make([]ContextCustomizer[REQUEST], 0)
	b.SpanStatusExtractor = &defaultSpanStatusExtractor[REQUEST, RESPONSE]{}
	return b
}

func (b *Builder[REQUEST, RESPONSE]) SetInstrumentationScope(scope instrumentation.Scope) *Builder[REQUEST, RESPONSE] {
	b.Scope = scope
	return b
}

func (b *Builder[REQUEST, RESPONSE]) SetInstrumentEnabler(enabler InstrumentEnabler) *Builder[REQUEST, RESPONSE] {
	b.Enabler = enabler
	return b
}

func (b *Builder[REQUEST, RESPONSE]) SetSpanNameExtractor(spanNameExtractor SpanNameExtractor[REQUEST]) *Builder[REQUEST, RESPONSE] {
	b.SpanNameExtractor = spanNameExtractor
	return b
}

func (b *Builder[REQUEST, RESPONSE]) SetSpanStatusExtractor(spanStatusExtractor SpanStatusExtractor[REQUEST, RESPONSE]) *Builder[REQUEST, RESPONSE] {
	b.SpanStatusExtractor = spanStatusExtractor
	return b
}

func (b *Builder[REQUEST, RESPONSE]) SetSpanKindExtractor(spanKindExtractor SpanKindExtractor[REQUEST]) *Builder[REQUEST, RESPONSE] {
	b.SpanKindExtractor = spanKindExtractor
	return b
}

func (b *Builder[REQUEST, RESPONSE]) AddAttributesExtractor(attributesExtractor ...AttributesExtractor[REQUEST, RESPONSE]) *Builder[REQUEST, RESPONSE] {
	b.AttributesExtractors = append(b.AttributesExtractors, attributesExtractor...)
	return b
}

func (b *Builder[REQUEST, RESPONSE]) AddOperationListeners(operationListener ...OperationListener) *Builder[REQUEST, RESPONSE] {
	b.OperationListeners = append(b.OperationListeners, operationListener...)
	return b
}

func (b *Builder[REQUEST, RESPONSE]) AddContextCustomizers(contextCustomizers ...ContextCustomizer[REQUEST]) *Builder[REQUEST, RESPONSE] {
	b.ContextCustomizers = append(b.ContextCustomizers, contextCustomizers...)
	return b
}

func (b *Builder[REQUEST, RESPONSE]) BuildInstrumenter() *InternalInstrumenter[REQUEST, RESPONSE] {
	tracer := otel.GetTracerProvider().
		Tracer(b.Scope.Name,
			trace.WithInstrumentationVersion(b.Scope.Version),
			trace.WithSchemaURL(b.Scope.SchemaURL))
	return &InternalInstrumenter[REQUEST, RESPONSE]{
		enabler:              b.Enabler,
		spanNameExtractor:    b.SpanNameExtractor,
		spanKindExtractor:    b.SpanKindExtractor,
		spanStatusExtractor:  b.SpanStatusExtractor,
		attributesExtractors: b.AttributesExtractors,
		operationListeners:   b.OperationListeners,
		contextCustomizers:   b.ContextCustomizers,
		spanSuppressor:       b.buildSpanSuppressor(),
		tracer:               tracer,
		instVersion:          b.InstVersion,
	}
}

func (b *Builder[REQUEST, RESPONSE]) BuildPropagatingToDownstreamInstrumenter(carrierGetter func(REQUEST) propagation.TextMapCarrier, prop propagation.TextMapPropagator) *PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE] {
	tracer := otel.GetTracerProvider().
		Tracer(b.Scope.Name,
			trace.WithInstrumentationVersion(b.Scope.Version),
			trace.WithSchemaURL(b.Scope.SchemaURL))
	return &PropagatingToDownstreamInstrumenter[REQUEST, RESPONSE]{
		base: InternalInstrumenter[REQUEST, RESPONSE]{
			enabler:              b.Enabler,
			spanNameExtractor:    b.SpanNameExtractor,
			spanKindExtractor:    b.SpanKindExtractor,
			spanStatusExtractor:  b.SpanStatusExtractor,
			attributesExtractors: b.AttributesExtractors,
			operationListeners:   b.OperationListeners,
			contextCustomizers:   b.ContextCustomizers,
			spanSuppressor:       b.buildSpanSuppressor(),
			tracer:               tracer,
			instVersion:          b.InstVersion,
		},
		carrierGetter: carrierGetter,
		prop:          prop,
	}
}

func (b *Builder[REQUEST, RESPONSE]) BuildPropagatingFromUpstreamInstrumenter(carrierGetter func(REQUEST) propagation.TextMapCarrier, prop propagation.TextMapPropagator) *PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE] {
	tracer := otel.GetTracerProvider().
		Tracer(b.Scope.Name,
			trace.WithInstrumentationVersion(b.Scope.Version),
			trace.WithSchemaURL(b.Scope.SchemaURL))
	return &PropagatingFromUpstreamInstrumenter[REQUEST, RESPONSE]{
		base: InternalInstrumenter[REQUEST, RESPONSE]{
			enabler:              b.Enabler,
			spanNameExtractor:    b.SpanNameExtractor,
			spanKindExtractor:    b.SpanKindExtractor,
			spanStatusExtractor:  b.SpanStatusExtractor,
			attributesExtractors: b.AttributesExtractors,
			operationListeners:   b.OperationListeners,
			spanSuppressor:       b.buildSpanSuppressor(),
			tracer:               tracer,
			instVersion:          b.InstVersion,
		},
		carrierGetter: carrierGetter,
		prop:          prop,
	}
}

func (b *Builder[REQUEST, RESPONSE]) buildSpanSuppressor() SpanSuppressor {
	spanSuppressorStrategy := getSpanSuppressionStrategyFromEnv()
	kvs := make(map[attribute.Key]bool)
	for _, extractor := range b.AttributesExtractors {
		provider, ok := extractor.(SpanKeyProvider)
		if ok {
			kvs[provider.GetSpanKey()] = true
		}
	}
	kSlice := make([]attribute.Key, 0, len(kvs))
	for k := range kvs {
		kSlice = append(kSlice, k)
	}
	return spanSuppressorStrategy.create(kSlice)
}
