// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package net

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

type NetworkAttrsExtractor[REQUEST any, RESPONSE any] struct {
	internalExtractor InternalNetworkAttributesExtractor[REQUEST, RESPONSE]
}

func (_ *NetworkAttrsExtractor[REQUEST, RESPONSE]) OnStart(parentContext context.Context,
	attributes []attribute.KeyValue, _ REQUEST,
) ([]attribute.KeyValue, context.Context) {
	return attributes, parentContext
}

func (i *NetworkAttrsExtractor[REQUEST, RESPONSE]) OnEnd(context context.Context, attributes []attribute.KeyValue,
	request REQUEST, response RESPONSE, _ error,
) ([]attribute.KeyValue, context.Context) {
	return i.internalExtractor.OnEnd(context, attributes, request, response)
}

func CreateNetworkAttributesExtractor[REQUEST any, RESPONSE any](
	getter NetworkAttrsGetter[REQUEST, RESPONSE],
) NetworkAttrsExtractor[REQUEST, RESPONSE] {
	return NetworkAttrsExtractor[REQUEST, RESPONSE]{
		internalExtractor: InternalNetworkAttributesExtractor[REQUEST, RESPONSE]{
			getter:                       getter,
			captureProtocolAttributes:    true,
			captureLocalSocketAttributes: true,
		},
	}
}

type URLAttrsExtractor[REQUEST any, RESPONSE any, GETTER URLAttrsGetter[REQUEST]] struct {
	Getter GETTER
}

func (u *URLAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnStart(parentContext context.Context,
	attributes []attribute.KeyValue, request REQUEST,
) ([]attribute.KeyValue, context.Context) {
	attributes = append(attributes, attribute.KeyValue{
		Key:   semconv.URLSchemeKey,
		Value: attribute.StringValue(u.Getter.GetURLScheme(request)),
	}, attribute.KeyValue{
		Key:   semconv.URLPathKey,
		Value: attribute.StringValue(u.Getter.GetURLPath(request)),
	}, attribute.KeyValue{
		Key:   semconv.URLQueryKey,
		Value: attribute.StringValue(u.Getter.GetURLQuery(request)),
	})
	return attributes, parentContext
}

func (_ *URLAttrsExtractor[REQUEST, RESPONSE, GETTER]) OnEnd(context context.Context,
	attributes []attribute.KeyValue, _ REQUEST,
	_ RESPONSE, _ error,
) ([]attribute.KeyValue, context.Context) {
	return attributes, context
}

type ServerAttributesExtractor[REQUEST any, RESPONSE any] struct {
	internalExtractor InternalServerAttributesExtractor[REQUEST]
}

func (s *ServerAttributesExtractor[REQUEST, RESPONSE]) OnStart(parentContext context.Context, attributes []attribute.
	KeyValue, request REQUEST,
) ([]attribute.KeyValue, context.Context) {
	return s.internalExtractor.OnStart(parentContext, attributes, request)
}

func (_ *ServerAttributesExtractor[REQUEST, RESPONSE]) OnEnd(ctx context.Context, attributes []attribute.KeyValue,
	_ REQUEST, _ RESPONSE, _ error,
) ([]attribute.KeyValue, context.Context) {
	return attributes, ctx
}

func CreateServerAttributesExtractor[REQUEST any, RESPONSE any](
	getter ServerAttributesGetter[REQUEST],
) ServerAttributesExtractor[REQUEST, RESPONSE] {
	return ServerAttributesExtractor[REQUEST, RESPONSE]{
		internalExtractor: InternalServerAttributesExtractor[REQUEST]{
			addressAndPortExtractor: &ServerAddressAndPortExtractor[REQUEST]{
				getter:            getter,
				fallbackExtractor: &NoopAddressAndPortExtractor[REQUEST]{},
			},
		},
	}
}

type ClientAttributesExtractor[REQUEST any, RESPONSE any] struct {
	internalExtractor InternalClientAttributesExtractor[REQUEST]
}

func (s *ClientAttributesExtractor[REQUEST, RESPONSE]) OnStart(parentContext context.Context,
	attributes []attribute.KeyValue, request REQUEST,
) ([]attribute.KeyValue, context.Context) {
	return s.internalExtractor.OnStart(parentContext, attributes, request)
}

func (_ *ClientAttributesExtractor[REQUEST, RESPONSE]) OnEnd(ctx context.Context, attributes []attribute.KeyValue,
	_ REQUEST,
	_ RESPONSE,
	_ error,
) ([]attribute.KeyValue, context.Context) {
	return attributes, ctx
}

func CreateClientAttributesExtractor[REQUEST any, RESPONSE any](
	getter ClientAttributesGetter[REQUEST],
) ClientAttributesExtractor[REQUEST, RESPONSE] {
	return ClientAttributesExtractor[REQUEST, RESPONSE]{
		internalExtractor: InternalClientAttributesExtractor[REQUEST]{
			addressAndPortExtractor: &ClientAddressAndPortExtractor[REQUEST]{
				getter:            getter,
				fallbackExtractor: &NoopAddressAndPortExtractor[REQUEST]{},
			},
			capturePort: true,
		},
	}
}
