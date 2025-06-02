// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
)

/**
For HTTPServer, status code >= 500 or < 100 is treated as error.
For HTTPClient, status code >= 400 or < 100 is treated as error.
*/

const invalidHTTPStatusCode = "INVALID_HTTP_STATUS_CODE"

type HTTPClientSpanStatusExtractor[REQUEST any, RESPONSE any] struct {
	Getter HTTPCommonAttrsGetter[REQUEST, RESPONSE]
}

func (h HTTPClientSpanStatusExtractor[REQUEST, RESPONSE]) Extract(
	span trace.Span,
	request REQUEST,
	response RESPONSE,
	err error,
) {
	statusCode := h.Getter.GetHTTPResponseStatusCode(request, response, err)
	if statusCode >= 400 || statusCode < 100 {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Error, invalidHTTPStatusCode)
		}
		span.SetAttributes(
			attribute.KeyValue{Key: semconv.ErrorTypeKey, Value: attribute.StringValue(strconv.Itoa(statusCode))},
		)
	} else if statusCode >= 200 && statusCode < 300 {
		span.SetStatus(codes.Ok, "success")
	}
}

type HTTPServerSpanStatusExtractor[REQUEST any, RESPONSE any] struct {
	Getter HTTPCommonAttrsGetter[REQUEST, RESPONSE]
}

func (h HTTPServerSpanStatusExtractor[REQUEST, RESPONSE]) Extract(
	span trace.Span,
	request REQUEST,
	response RESPONSE,
	err error,
) {
	statusCode := h.Getter.GetHTTPResponseStatusCode(request, response, err)
	if statusCode >= 500 || statusCode < 100 {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Error, invalidHTTPStatusCode)
		}
		span.SetAttributes(
			attribute.KeyValue{Key: semconv.ErrorTypeKey, Value: attribute.StringValue(strconv.Itoa(statusCode))},
		)
	} else if statusCode >= 200 && statusCode < 300 {
		span.SetStatus(codes.Ok, "success")
	}
}
