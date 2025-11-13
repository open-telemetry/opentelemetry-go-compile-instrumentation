// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type testSpan struct {
	trace.Span
	status *codes.Code
	Kvs    []attribute.KeyValue
}

func (ts *testSpan) SetStatus(status codes.Code, _ string) {
	*ts.status = status
}

func (ts *testSpan) SetAttributes(kv ...attribute.KeyValue) {
	ts.Kvs = kv
}

type testReadOnlySpan struct {
	sdktrace.ReadWriteSpan
	isRecording bool
}

func (*testReadOnlySpan) Name() string {
	return "http-route"
}

func (t *testReadOnlySpan) IsRecording() bool {
	return t.isRecording
}

type customizedNetHTTPAttrsGetter struct {
	code int
}

func (customizedNetHTTPAttrsGetter) GetRequestMethod(_ any) string {
	// TODO implement me
	panic("implement me")
}

func (customizedNetHTTPAttrsGetter) GetHTTPRequestHeader(_ any, _ string) []string {
	// TODO implement me
	panic("implement me")
}

func (c customizedNetHTTPAttrsGetter) GetHTTPResponseStatusCode(_, _ any, _ error) int {
	return c.code
}

func (customizedNetHTTPAttrsGetter) GetHTTPResponseHeader(_, _ any, _ string) []string {
	// TODO implement me
	panic("implement me")
}

func (customizedNetHTTPAttrsGetter) GetErrorType(_, _ any, _ error) string {
	// TODO implement me
	panic("implement me")
}

func TestHTTPClientSpanStatusExtractor500(t *testing.T) {
	c := HTTPClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHTTPAttrsGetter{
			code: 500,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Error {
		t.Fatal("span status should be error!")
	}
}

func TestHTTPClientSpanStatusExtractor400(t *testing.T) {
	c := HTTPClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHTTPAttrsGetter{
			code: 400,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Error {
		t.Fatal("span status should be error!")
	}
	if span.Kvs == nil {
		t.Fatal("kv should not be nil")
	}
}

func TestHTTPClientSpanStatusExtractor200(t *testing.T) {
	c := HTTPClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHTTPAttrsGetter{
			code: 200,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Ok {
		t.Fatal("span status should be ok!")
	}
}

func TestHTTPClientSpanStatusExtractor201(t *testing.T) {
	c := HTTPClientSpanStatusExtractor[any, any]{
		Getter: customizedNetHTTPAttrsGetter{
			code: 201,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Ok {
		t.Fatal("span status should be ok!")
	}
}

func TestHTTPServerSpanStatusExtractor500(t *testing.T) {
	c := HTTPServerSpanStatusExtractor[any, any]{
		Getter: customizedNetHTTPAttrsGetter{
			code: 500,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Error {
		t.Fatal("span status should be error!")
	}
	if span.Kvs == nil {
		t.Fatal("kv should not be nil")
	}
}

func TestHTTPServerSpanStatusExtractor400(t *testing.T) {
	c := HTTPServerSpanStatusExtractor[any, any]{
		Getter: customizedNetHTTPAttrsGetter{
			code: 400,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Unset {
		t.Fatal("span status should be unset!")
	}
}

func TestHTTPServerSpanStatusExtractor200(t *testing.T) {
	c := HTTPServerSpanStatusExtractor[any, any]{
		Getter: customizedNetHTTPAttrsGetter{
			code: 200,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Ok {
		t.Fatal("span status should be ok!")
	}
}

func TestHTTPServerSpanStatusExtractor201(t *testing.T) {
	c := HTTPServerSpanStatusExtractor[any, any]{
		Getter: customizedNetHTTPAttrsGetter{
			code: 201,
		},
	}
	u := codes.Code(0)
	span := &testSpan{status: &u}
	c.Extract(span, nil, nil, nil)
	if *span.status != codes.Ok {
		t.Fatal("span status should be ok!")
	}
}
