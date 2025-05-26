// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"testing"
)

type testSpan struct {
	trace.Span
	status *codes.Code
	Kvs    []attribute.KeyValue
}

func (ts *testSpan) SetStatus(status codes.Code, desc string) {
	*ts.status = status
}

func (ts *testSpan) SetAttributes(kv ...attribute.KeyValue) {
	ts.Kvs = kv
}

type testReadOnlySpan struct {
	sdktrace.ReadWriteSpan
	isRecording bool
}

func (t *testReadOnlySpan) Name() string {
	return "http-route"
}

func (t *testReadOnlySpan) IsRecording() bool {
	return t.isRecording
}

type customizedNetHttpAttrsGetter struct {
	code int
}

func (c customizedNetHttpAttrsGetter) GetRequestMethod(request any) string {
	//TODO implement me
	panic("implement me")
}

func (c customizedNetHttpAttrsGetter) GetHttpRequestHeader(request any, name string) []string {
	//TODO implement me
	panic("implement me")
}

func (c customizedNetHttpAttrsGetter) GetHttpResponseStatusCode(request any, response any, err error) int {
	return c.code
}

func (c customizedNetHttpAttrsGetter) GetHttpResponseHeader(request any, response any, name string) []string {
	//TODO implement me
	panic("implement me")
}

func (c customizedNetHttpAttrsGetter) GetErrorType(request any, response any, err error) string {
	//TODO implement me
	panic("implement me")
}

func TestHttpClientSpanStatusExtractor500(t *testing.T) {
	c := HttpClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHttpAttrsGetter{
			code: 500,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Error {
		panic("span status should be error!")
	}
}

func TestHttpClientSpanStatusExtractor400(t *testing.T) {
	c := HttpClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHttpAttrsGetter{
			code: 400,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Error {
		panic("span status should be error!")
	}
	if span.Kvs == nil {
		panic("kv should not be nil")
	}
}

func TestHttpClientSpanStatusExtractor200(t *testing.T) {
	c := HttpClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHttpAttrsGetter{
			code: 200,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Ok {
		panic("span status should be ok!")
	}
}
func TestHttpClientSpanStatusExtractor201(t *testing.T) {
	c := HttpClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHttpAttrsGetter{
			code: 201,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Ok {
		panic("span status should be ok!")
	}
}
func TestHttpServerSpanStatusExtractor500(t *testing.T) {
	c := HttpServerSpanStatusExtractor[any, any]{
		Getter: customizedNetHttpAttrsGetter{
			code: 500,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Error {
		panic("span status should be error!")
	}
	if span.Kvs == nil {
		panic("kv should not be nil")
	}
}

func TestHttpServerSpanStatusExtractor400(t *testing.T) {
	c := HttpServerSpanStatusExtractor[any, any]{
		Getter: customizedNetHttpAttrsGetter{
			code: 400,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Unset {
		panic("span status should be error!")
	}
}

func TestHttpServerSpanStatusExtractor200(t *testing.T) {
	c := HttpClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHttpAttrsGetter{
			code: 200,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Ok {
		panic("span status should be ok!")
	}
}
func TestHttpServerSpanStatusExtractor201(t *testing.T) {
	c := HttpClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHttpAttrsGetter{
			code: 201,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Ok {
		panic("span status should be ok!")
	}
}
