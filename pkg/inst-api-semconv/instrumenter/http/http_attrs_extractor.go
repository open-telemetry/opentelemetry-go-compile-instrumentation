// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/utils"
)

type (
	HTTPRequest  any
	HTTPResponse any
)

/**
Extract attributes from HTTPRequest and HTTPResponse according to
OpenTelemetry HTTP Spec for span and metric:
https://opentelemetry.io/docs/specs/semconv/http/http-spans/: Semantic Conventions for HTTP client and server spans.
https://opentelemetry.io/docs/specs/semconv/http/http-metrics/: Semantic Conventions for HTTP client and server metrics.
*/

type HTTPCommonAttrsExtractor[REQUEST HTTPRequest, RESPONSE HTTPResponse,
	COMMONATTRGETTER HTTPCommonAttrsGetter[REQUEST, RESPONSE]] struct {
	HTTPGetter       COMMONATTRGETTER
	AttributesFilter func(attrs []attribute.KeyValue) []attribute.KeyValue
}

func (h *HTTPCommonAttrsExtractor[REQUEST, RESPONSE, COMMONATTRGETTER]) OnStart(parentContext context.Context,
	attributes []attribute.KeyValue,
	request REQUEST,
) ([]attribute.KeyValue, context.Context) {
	attributes = append(attributes, attribute.KeyValue{
		Key:   semconv.HTTPRequestMethodKey,
		Value: attribute.StringValue(h.HTTPGetter.GetRequestMethod(request)),
	})
	return attributes, parentContext
}

func (h *HTTPCommonAttrsExtractor[REQUEST, RESPONSE, COMMONATTRGETTER]) OnEnd(context context.Context,
	attributes []attribute.KeyValue,
	request REQUEST, response RESPONSE, err error,
) ([]attribute.KeyValue, context.Context) {
	statusCode := h.HTTPGetter.GetHTTPResponseStatusCode(request, response, err)
	attributes = append(attributes, attribute.KeyValue{
		Key:   semconv.HTTPResponseStatusCodeKey,
		Value: attribute.IntValue(statusCode),
	})
	errorType := h.HTTPGetter.GetErrorType(request, response, err)
	if errorType != "" {
		attributes = append(
			attributes,
			attribute.KeyValue{Key: semconv.ErrorTypeKey, Value: attribute.StringValue(errorType)},
		)
	}
	return attributes, context
}

type HTTPClientAttrsExtractor[REQUEST HTTPRequest, RESPONSE HTTPResponse, GETTER1 HTTPClientAttrsGetter[REQUEST, RESPONSE]] struct {
	Base HTTPCommonAttrsExtractor[REQUEST, RESPONSE, GETTER1]
}

func (h *HTTPClientAttrsExtractor[REQUEST, RESPONSE, CLIENTATTRGETTER]) OnStart(parentContext context.Context,
	attributes []attribute.KeyValue,
	request REQUEST,
) ([]attribute.KeyValue, context.Context) {
	attributes, parentContext = h.Base.OnStart(parentContext, attributes, request)
	resendCount := parentContext.Value(utils.ClientResendKey)
	newCount := int32(0)
	if resendCount != nil {
		count, ok := resendCount.(*int32)
		if ok {
			newCount = atomic.AddInt32(count, 1)
		}
		if newCount > 0 {
			attributes = append(attributes, attribute.KeyValue{
				Key:   semconv.HTTPRequestResendCountKey,
				Value: attribute.IntValue(int(newCount)),
			})
		}
	}
	parentContext = context.WithValue(parentContext, utils.ClientResendKey, &newCount)
	if h.Base.AttributesFilter != nil {
		attributes = h.Base.AttributesFilter(attributes)
	}
	return attributes, parentContext
}

func (h *HTTPClientAttrsExtractor[REQUEST, RESPONSE, CLIENTATTRGETTER]) OnEnd(
	context context.Context,
	attributes []attribute.KeyValue,
	request REQUEST, response RESPONSE, err error,
) ([]attribute.KeyValue, context.Context) {
	attributes, context = h.Base.OnEnd(context, attributes, request, response, err)
	if h.Base.AttributesFilter != nil {
		attributes = h.Base.AttributesFilter(attributes)
	}
	return attributes, context
}

func (_ *HTTPClientAttrsExtractor[REQUEST, RESPONSE, CLIENTATTRGETTER]) GetSpanKey() attribute.Key {
	return utils.HTTPClientKey
}

type HTTPServerAttrsExtractor[REQUEST HTTPRequest, RESPONSE HTTPResponse,
	SERVERATTRGETTER HTTPServerAttrsGetter[REQUEST, RESPONSE]] struct {
	Base HTTPCommonAttrsExtractor[REQUEST, RESPONSE, SERVERATTRGETTER]
}

func (h *HTTPServerAttrsExtractor[REQUEST, RESPONSE, SERVERATTRGETTER]) OnStart(
	parentContext context.Context,
	attributes []attribute.KeyValue,
	request REQUEST,
) ([]attribute.KeyValue, context.Context) {
	attributes, parentContext = h.Base.OnStart(parentContext, attributes, request)
	userAgent := h.Base.HTTPGetter.GetHTTPRequestHeader(request, "User-Agent")
	var firstUserAgent string
	if len(userAgent) > 0 {
		firstUserAgent = userAgent[0]
	} else {
		firstUserAgent = ""
	}
	attributes = append(attributes, attribute.KeyValue{
		Key:   semconv.UserAgentOriginalKey,
		Value: attribute.StringValue(firstUserAgent),
	})
	if h.Base.AttributesFilter != nil {
		attributes = h.Base.AttributesFilter(attributes)
	}
	return attributes, parentContext
}

func (h *HTTPServerAttrsExtractor[REQUEST, RESPONSE, SERVERATTRGETTER]) OnEnd(
	context context.Context, attributes []attribute.KeyValue,
	request REQUEST, response RESPONSE, err error,
) ([]attribute.KeyValue, context.Context) {
	attributes, context = h.Base.OnEnd(context, attributes, request, response, err)
	route := h.Base.HTTPGetter.GetHTTPRoute(request)
	attributes = append(attributes, attribute.KeyValue{
		Key:   semconv.HTTPRouteKey,
		Value: attribute.StringValue(route),
	})
	if h.Base.AttributesFilter != nil {
		attributes = h.Base.AttributesFilter(attributes)
	}
	return attributes, context
}

func (_ *HTTPServerAttrsExtractor[REQUEST, RESPONSE, SERVERATTRGETTER]) GetSpanKey() attribute.Key {
	return utils.HTTPServerKey
}
