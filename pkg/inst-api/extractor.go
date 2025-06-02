// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type AttributesExtractor[REQUEST any, RESPONSE any] interface {
	OnStart(parentContext context.Context, attributes []attribute.KeyValue, request REQUEST) ([]attribute.KeyValue,
		context.Context)
	OnEnd(parentContext context.Context, attributes []attribute.KeyValue, ctx context.Context, request REQUEST,
		response RESPONSE, err error) ([]attribute.KeyValue, context.Context)
}

type SpanKindExtractor[REQUEST any] interface {
	Extract(request REQUEST) trace.SpanKind
}

type SpanNameExtractor[REQUEST any] interface {
	Extract(request REQUEST) string
}

type SpanStatusExtractor[REQUEST any, RESPONSE any] interface {
	Extract(span trace.Span, request REQUEST, response RESPONSE, err error)
}

type SpanKeyProvider interface {
	GetSpanKey() attribute.Key
}

type AlwaysInternalExtractor[REQUEST any] struct{}

func (_ *AlwaysInternalExtractor[any]) Extract(_ any) trace.SpanKind {
	return trace.SpanKindInternal
}

type AlwaysClientExtractor[REQUEST any] struct{}

func (_ *AlwaysClientExtractor[any]) Extract(_ any) trace.SpanKind {
	return trace.SpanKindClient
}

type AlwaysServerExtractor[REQUEST any] struct{}

func (_ *AlwaysServerExtractor[any]) Extract(_ any) trace.SpanKind {
	return trace.SpanKindServer
}

type AlwaysProducerExtractor[request any] struct{}

func (_ *AlwaysProducerExtractor[any]) Extract(_ any) trace.SpanKind {
	return trace.SpanKindProducer
}

type AlwaysConsumerExtractor[request any] struct{}

func (_ *AlwaysConsumerExtractor[any]) Extract(_ any) trace.SpanKind {
	return trace.SpanKindConsumer
}

type defaultSpanStatusExtractor[request any, response any] struct{}

func (*defaultSpanStatusExtractor[REQUEST, RESPONSE]) Extract(
	span trace.Span,
	_ REQUEST,
	_ RESPONSE,
	err error,
) {
	if err != nil {
		span.SetStatus(codes.Error, "")
	}
}
