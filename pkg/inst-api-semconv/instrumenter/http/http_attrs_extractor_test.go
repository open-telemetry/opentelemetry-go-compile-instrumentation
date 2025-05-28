// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/utils"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
	"testing"
)

type httpServerAttrsGetter struct {
}

type httpClientAttrsGetter struct {
}

func (h httpClientAttrsGetter) GetRequestMethod(request testRequest) string {
	return "GET"
}

func (h httpClientAttrsGetter) GetHttpRequestHeader(request testRequest, name string) []string {
	return []string{"request-header"}
}

func (h httpClientAttrsGetter) GetHttpResponseStatusCode(request testRequest, response testResponse, err error) int {
	return 200
}

func (h httpClientAttrsGetter) GetHttpResponseHeader(request testRequest, response testResponse, name string) []string {
	return []string{"response-header"}
}

func (h httpClientAttrsGetter) GetErrorType(request testRequest, response testResponse, err error) string {
	return ""
}

func (h httpServerAttrsGetter) GetRequestMethod(request testRequest) string {
	return "GET"
}

func (h httpServerAttrsGetter) GetHttpRequestHeader(request testRequest, name string) []string {
	return []string{"request-header"}
}

func (h httpServerAttrsGetter) GetHttpResponseStatusCode(request testRequest, response testResponse, err error) int {
	return 200
}

func (h httpServerAttrsGetter) GetHttpResponseHeader(request testRequest, response testResponse, name string) []string {
	return []string{"response-header"}
}

func (h httpServerAttrsGetter) GetErrorType(request testRequest, response testResponse, err error) string {
	return "error-type"
}

func (h httpServerAttrsGetter) GetHttpRoute(request testRequest) string {
	return "http-route"
}

func TestHttpClientExtractorStart(t *testing.T) {
	httpClientExtractor := HttpClientAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{
		Base: HttpCommonAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	attrs, _ = httpClientExtractor.OnStart(parentContext, attrs, testRequest{})
	if attrs[0].Key != semconv.HTTPRequestMethodKey || attrs[0].Value.AsString() != "GET" {
		t.Fatalf("http method should be GET")
	}
	if httpClientExtractor.GetSpanKey() != utils.HTTP_CLIENT_KEY {
		t.Fatalf("span key should be http-client")
	}
}

func TestHttpClientExtractorEnd(t *testing.T) {
	httpClientExtractor := HttpClientAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{
		Base: HttpCommonAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	attrs, _ = httpClientExtractor.OnEnd(attrs, parentContext, testRequest{}, testResponse{}, nil)
	if attrs[0].Key != semconv.HTTPResponseStatusCodeKey || attrs[0].Value.AsInt64() != 200 {
		t.Fatalf("status code should be 200")
	}
}

func TestHttpServerExtractorStart(t *testing.T) {
	httpServerExtractor := HttpServerAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{
		Base: HttpCommonAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	attrs, _ = httpServerExtractor.OnStart(attrs, parentContext, testRequest{})
	if attrs[0].Key != semconv.HTTPRequestMethodKey || attrs[0].Value.AsString() != "GET" {
		t.Fatalf("http method should be GET")
	}
	if attrs[1].Key != semconv.UserAgentOriginalKey || attrs[1].Value.AsString() != "request-header" {
		t.Fatalf("user agent original should be request-header")
	}
	if httpServerExtractor.GetSpanKey() != utils.HTTP_SERVER_KEY {
		t.Fatalf("span key should be http-server")
	}
}

func TestHttpServerExtractorEnd(t *testing.T) {
	httpServerExtractor := HttpServerAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{
		Base: HttpCommonAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	ctx := context.Background()
	ctx = trace.ContextWithSpan(ctx, &testReadOnlySpan{isRecording: true})
	attrs, _ = httpServerExtractor.OnEnd(attrs, ctx, testRequest{}, testResponse{}, nil)
	if attrs[0].Key != semconv.HTTPResponseStatusCodeKey || attrs[0].Value.AsInt64() != 200 {
		t.Fatalf("status code should be 200")
	}
	if attrs[1].Key != semconv.ErrorTypeKey || attrs[1].Value.AsString() != "error-type" {
		t.Fatalf("wrong error type")
	}
	if attrs[2].Key != semconv.HTTPRouteKey || attrs[2].Value.AsString() != "http-route" {
		t.Fatalf("httproute should be http-route")
	}
}

func TestHttpServerExtractorWithFilter(t *testing.T) {
	httpServerExtractor := HttpServerAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{
		Base: HttpCommonAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	httpServerExtractor.Base.AttributesFilter = func(attrs []attribute.KeyValue) []attribute.KeyValue {
		return []attribute.KeyValue{{
			Key:   "test",
			Value: attribute.StringValue("test"),
		}}
	}
	attrs = make([]attribute.KeyValue, 0)
	attrs, _ = httpServerExtractor.OnStart(attrs, parentContext, testRequest{Method: "test"})
	if attrs[0].Key != "test" || attrs[0].Value.AsString() != "test" {
		panic("attribute should be test")
	}
	attrs, _ = httpServerExtractor.OnEnd(attrs, parentContext, testRequest{Method: "test"}, testResponse{}, nil)
	if attrs[0].Key != "test" || attrs[0].Value.AsString() != "test" {
		panic("attribute should be test")
	}
}

func TestHttpClientExtractorWithFilter(t *testing.T) {
	httpClientExtractor := HttpClientAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{
		Base: HttpCommonAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	httpClientExtractor.Base.AttributesFilter = func(attrs []attribute.KeyValue) []attribute.KeyValue {
		return []attribute.KeyValue{{
			Key:   "test",
			Value: attribute.StringValue("test"),
		}}
	}
	attrs = make([]attribute.KeyValue, 0)
	attrs, _ = httpClientExtractor.OnStart(parentContext, attrs, testRequest{Method: "test"})
	if attrs[0].Key != "test" || attrs[0].Value.AsString() != "test" {
		panic("attribute should be test")
	}
	attrs, _ = httpClientExtractor.OnEnd(attrs, parentContext, testRequest{Method: "test"}, testResponse{}, nil)
	if attrs[0].Key != "test" || attrs[0].Value.AsString() != "test" {
		panic("attribute should be test")
	}
}

func TestNonRecordingSpan(t *testing.T) {
	httpServerExtractor := HttpServerAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{
		Base: HttpCommonAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	ctx := context.Background()
	ctx = trace.ContextWithSpan(ctx, &testReadOnlySpan{isRecording: false})
	attrs, _ = httpServerExtractor.OnEnd(attrs, ctx, testRequest{}, testResponse{}, nil)
	if attrs[0].Key != semconv.HTTPResponseStatusCodeKey || attrs[0].Value.AsInt64() != 200 {
		t.Fatalf("status code should be 200")
	}
	if attrs[1].Key != semconv.ErrorTypeKey || attrs[1].Value.AsString() != "error-type" {
		t.Fatalf("wrong error type")
	}
}

func TestResendCountHandling(t *testing.T) {
	httpClientExtractor := HttpClientAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{
		Base: HttpCommonAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{},
	}
	parentContext := context.Background()
	resendCount := int32(0)
	parentContext = context.WithValue(parentContext, utils.CLIENT_RESEND_KEY, &resendCount)
	var attributes []attribute.KeyValue
	attributes, _ = httpClientExtractor.OnStart(parentContext, attributes, testRequest{})
	if attributes[1].Key != semconv.HTTPRequestResendCountKey || attributes[1].Value.AsInt64() != 1 {
		t.Fatalf("wrong http.request.resend_count")
	}

	parentContext = context.Background()
	attributes, _ = httpClientExtractor.OnStart(parentContext, []attribute.KeyValue{}, testRequest{})
	if len(attributes) > 1 {
		t.Fatalf("wrong attributes length")
	}
}
