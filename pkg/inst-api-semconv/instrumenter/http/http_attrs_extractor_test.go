// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst-api-semconv/instrumenter/utils"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
)

type httpServerAttrsGetter struct{}

type httpClientAttrsGetter struct{}

func (httpClientAttrsGetter) GetRequestMethod(_ testRequest) string {
	return "GET"
}

func (httpClientAttrsGetter) GetHTTPRequestHeader(_ testRequest, name string) []string {
	return []string{"request-header"}
}

func (httpClientAttrsGetter) GetHTTPResponseStatusCode(_ testRequest, response testResponse, err error) int {
	return 200
}

func (httpClientAttrsGetter) GetHTTPResponseHeader(_ testRequest, _ testResponse, name string) []string {
	return []string{"response-header"}
}

func (httpClientAttrsGetter) GetErrorType(_ testRequest, _ testResponse, err error) string {
	return ""
}

func (httpServerAttrsGetter) GetRequestMethod(_ testRequest) string {
	return "GET"
}

func (httpServerAttrsGetter) GetHTTPRequestHeader(_ testRequest, name string) []string {
	return []string{"request-header"}
}

func (httpServerAttrsGetter) GetHTTPResponseStatusCode(_ testRequest, response testResponse, err error) int {
	return 200
}

func (httpServerAttrsGetter) GetHTTPResponseHeader(_ testRequest, _ testResponse, name string) []string {
	return []string{"response-header"}
}

func (httpServerAttrsGetter) GetErrorType(_ testRequest, _ testResponse, err error) string {
	return "error-type"
}

func (httpServerAttrsGetter) GetHTTPRoute(_ testRequest) string {
	return "http-route"
}

func TestHTTPClientExtractorStart(t *testing.T) {
	httpClientExtractor := HTTPClientAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{
		Base: HTTPCommonAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	attrs, _ = httpClientExtractor.OnStart(parentContext, attrs, testRequest{})
	if attrs[0].Key != semconv.HTTPRequestMethodKey || attrs[0].Value.AsString() != "GET" {
		t.Fatalf("http method should be GET")
	}
	if httpClientExtractor.GetSpanKey() != utils.HTTPClientKey {
		t.Fatalf("span key should be http-client")
	}
}

func TestHTTPClientExtractorEnd(t *testing.T) {
	httpClientExtractor := HTTPClientAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{
		Base: HTTPCommonAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	attrs, _ = httpClientExtractor.OnEnd(parentContext, attrs, testRequest{}, testResponse{}, nil)
	if attrs[0].Key != semconv.HTTPResponseStatusCodeKey || attrs[0].Value.AsInt64() != 200 {
		t.Fatalf("status code should be 200")
	}
}

func TestHTTPServerExtractorStart(t *testing.T) {
	httpServerExtractor := HTTPServerAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{
		Base: HTTPCommonAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	attrs, _ = httpServerExtractor.OnStart(parentContext, attrs, testRequest{})
	if attrs[0].Key != semconv.HTTPRequestMethodKey || attrs[0].Value.AsString() != "GET" {
		t.Fatalf("http method should be GET")
	}
	if attrs[1].Key != semconv.UserAgentOriginalKey || attrs[1].Value.AsString() != "request-header" {
		t.Fatalf("user agent original should be request-header")
	}
	if httpServerExtractor.GetSpanKey() != utils.HTTPServerKey {
		t.Fatalf("span key should be http-server")
	}
}

func TestHTTPServerExtractorEnd(t *testing.T) {
	httpServerExtractor := HTTPServerAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{
		Base: HTTPCommonAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	ctx := context.Background()
	ctx = trace.ContextWithSpan(ctx, &testReadOnlySpan{isRecording: true})
	attrs, _ = httpServerExtractor.OnEnd(ctx, attrs, testRequest{}, testResponse{}, nil)
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

func TestHTTPServerExtractorWithFilter(t *testing.T) {
	httpServerExtractor := HTTPServerAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{
		Base: HTTPCommonAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	httpServerExtractor.Base.AttributesFilter = func(_ []attribute.KeyValue) []attribute.KeyValue {
		return []attribute.KeyValue{{
			Key:   "test",
			Value: attribute.StringValue("test"),
		}}
	}
	attrs = make([]attribute.KeyValue, 0)
	attrs, _ = httpServerExtractor.OnStart(parentContext, attrs, testRequest{Method: "test"})
	if attrs[0].Key != "test" || attrs[0].Value.AsString() != "test" {
		t.Fatal("attribute should be test")
	}
	attrs, _ = httpServerExtractor.OnEnd(parentContext, attrs, testRequest{Method: "test"}, testResponse{}, nil)
	if attrs[0].Key != "test" || attrs[0].Value.AsString() != "test" {
		t.Fatal("attribute should be test")
	}
}

func TestHTTPClientExtractorWithFilter(t *testing.T) {
	httpClientExtractor := HTTPClientAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{
		Base: HTTPCommonAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	parentContext := context.Background()
	httpClientExtractor.Base.AttributesFilter = func(_ []attribute.KeyValue) []attribute.KeyValue {
		return []attribute.KeyValue{{
			Key:   "test",
			Value: attribute.StringValue("test"),
		}}
	}
	attrs = make([]attribute.KeyValue, 0)
	attrs, _ = httpClientExtractor.OnStart(parentContext, attrs, testRequest{Method: "test"})
	if attrs[0].Key != "test" || attrs[0].Value.AsString() != "test" {
		t.Fatal("attribute should be test")
	}
	attrs, _ = httpClientExtractor.OnEnd(parentContext, attrs, testRequest{Method: "test"}, testResponse{}, nil)
	if attrs[0].Key != "test" || attrs[0].Value.AsString() != "test" {
		t.Fatal("attribute should be test")
	}
}

func TestNonRecordingSpan(t *testing.T) {
	httpServerExtractor := HTTPServerAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{
		Base: HTTPCommonAttrsExtractor[testRequest, testResponse, httpServerAttrsGetter]{},
	}
	var attrs []attribute.KeyValue
	ctx := context.Background()
	ctx = trace.ContextWithSpan(ctx, &testReadOnlySpan{isRecording: false})
	attrs, _ = httpServerExtractor.OnEnd(ctx, attrs, testRequest{}, testResponse{}, nil)
	if attrs[0].Key != semconv.HTTPResponseStatusCodeKey || attrs[0].Value.AsInt64() != 200 {
		t.Fatalf("status code should be 200")
	}
	if attrs[1].Key != semconv.ErrorTypeKey || attrs[1].Value.AsString() != "error-type" {
		t.Fatalf("wrong error type")
	}
}

func TestResendCountHandling(t *testing.T) {
	httpClientExtractor := HTTPClientAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{
		Base: HTTPCommonAttrsExtractor[testRequest, testResponse, httpClientAttrsGetter]{},
	}
	parentContext := context.Background()
	resendCount := int32(0)
	parentContext = context.WithValue(parentContext, utils.ClientResendKey, &resendCount)
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
