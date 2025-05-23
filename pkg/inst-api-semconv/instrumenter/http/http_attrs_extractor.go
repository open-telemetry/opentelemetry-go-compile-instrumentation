// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/utils"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"sync/atomic"
)

type HttpRequest interface{}

type HttpResponse interface{}

/**
Extract attributes from HttpRequest and HttpResponse according to
OpenTelemetry HTTP Spec for span and metric:
https://opentelemetry.io/docs/specs/semconv/http/http-spans/: Semantic Conventions for HTTP client and server spans.
https://opentelemetry.io/docs/specs/semconv/http/http-metrics/: Semantic Conventions for HTTP client and server metrics.
*/

type HttpCommonAttrsExtractor[REQUEST HttpRequest, RESPONSE HttpResponse,
	COMMONATTRGETTER HttpCommonAttrsGetter[REQUEST, RESPONSE]] struct {
	HttpGetter       COMMONATTRGETTER
	AttributesFilter func(attrs []attribute.KeyValue) []attribute.KeyValue
}

func (h *HttpCommonAttrsExtractor[REQUEST, RESPONSE, COMMONATTRGETTER]) OnStart(parentContext context.Context,
	attributes []attribute.KeyValue,
	request REQUEST) ([]attribute.KeyValue, context.Context) {
	attributes = append(attributes, attribute.KeyValue{
		Key:   semconv.HTTPRequestMethodKey,
		Value: attribute.StringValue(h.HttpGetter.GetRequestMethod(request)),
	})
	return attributes, parentContext
}

func (h *HttpCommonAttrsExtractor[REQUEST, RESPONSE, COMMONATTRGETTER]) OnEnd(context context.Context,
	attributes []attribute.KeyValue,
	request REQUEST, response RESPONSE, err error) ([]attribute.KeyValue, context.Context) {
	statusCode := h.HttpGetter.GetHttpResponseStatusCode(request, response, err)
	attributes = append(attributes, attribute.KeyValue{
		Key:   semconv.HTTPResponseStatusCodeKey,
		Value: attribute.IntValue(statusCode),
	})
	errorType := h.HttpGetter.GetErrorType(request, response, err)
	if errorType != "" {
		attributes = append(attributes, attribute.KeyValue{Key: semconv.ErrorTypeKey, Value: attribute.StringValue(errorType)})
	}
	return attributes, context
}

type HttpClientAttrsExtractor[REQUEST HttpRequest, RESPONSE HttpResponse, GETTER1 HttpClientAttrsGetter[REQUEST, RESPONSE]] struct {
	Base HttpCommonAttrsExtractor[REQUEST, RESPONSE, GETTER1]
}

func (h *HttpClientAttrsExtractor[REQUEST, RESPONSE, CLIENTATTRGETTER]) OnStart(parentContext context.Context,
	attributes []attribute.KeyValue,
	request REQUEST) ([]attribute.KeyValue, context.Context) {
	attributes, parentContext = h.Base.OnStart(parentContext, attributes, request)
	resendCount := parentContext.Value(utils.CLIENT_RESEND_KEY)
	newCount := int32(0)
	if resendCount != nil {
		newCount = atomic.AddInt32(resendCount.(*int32), 1)
		if newCount > 0 {
			attributes = append(attributes, attribute.KeyValue{
				Key:   semconv.HTTPRequestResendCountKey,
				Value: attribute.IntValue(int(newCount)),
			})
		}
	}
	parentContext = context.WithValue(parentContext, utils.CLIENT_RESEND_KEY, &newCount)
	if h.Base.AttributesFilter != nil {
		attributes = h.Base.AttributesFilter(attributes)
	}
	return attributes, parentContext
}

func (h *HttpClientAttrsExtractor[REQUEST, RESPONSE, CLIENTATTRGETTER]) OnEnd(attributes []attribute.KeyValue,
	context context.Context, request REQUEST, response RESPONSE, err error) ([]attribute.KeyValue, context.Context) {
	attributes, context = h.Base.OnEnd(context, attributes, request, response, err)
	if h.Base.AttributesFilter != nil {
		attributes = h.Base.AttributesFilter(attributes)
	}
	return attributes, context
}

func (h *HttpClientAttrsExtractor[REQUEST, RESPONSE, CLIENTATTRGETTER]) GetSpanKey() attribute.Key {
	return utils.HTTP_CLIENT_KEY
}

type HttpServerAttrsExtractor[REQUEST HttpRequest, RESPONSE HttpResponse,
	SERVERATTRGETTER HttpServerAttrsGetter[REQUEST, RESPONSE]] struct {
	Base HttpCommonAttrsExtractor[REQUEST, RESPONSE, SERVERATTRGETTER]
}

func (h *HttpServerAttrsExtractor[REQUEST, RESPONSE, SERVERATTRGETTER]) OnStart(attributes []attribute.KeyValue,
	parentContext context.Context, request REQUEST) ([]attribute.KeyValue, context.Context) {
	attributes, parentContext = h.Base.OnStart(parentContext, attributes, request)
	userAgent := h.Base.HttpGetter.GetHttpRequestHeader(request, "User-Agent")
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

func (h *HttpServerAttrsExtractor[REQUEST, RESPONSE, SERVERATTRGETTER]) OnEnd(attributes []attribute.KeyValue,
	context context.Context, request REQUEST, response RESPONSE, err error) ([]attribute.KeyValue, context.Context) {
	attributes, context = h.Base.OnEnd(context, attributes, request, response, err)
	route := h.Base.HttpGetter.GetHttpRoute(request)
	attributes = append(attributes, attribute.KeyValue{
		Key:   semconv.HTTPRouteKey,
		Value: attribute.StringValue(route),
	})
	if h.Base.AttributesFilter != nil {
		attributes = h.Base.AttributesFilter(attributes)
	}
	return attributes, context
}

func (h *HttpServerAttrsExtractor[REQUEST, RESPONSE, SERVERATTRGETTER]) GetSpanKey() attribute.Key {
	return utils.HTTP_SERVER_KEY
}
