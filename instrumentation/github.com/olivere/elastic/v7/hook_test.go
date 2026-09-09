// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v7

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/olivere/elastic/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
	"go.opentelemetry.io/otelc/pkg/runtime"
)

func setupTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	initOnce = sync.Once{}
	tracer = nil

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr
}

func TestElasticEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		want     bool
	}{
		{
			name: "enabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "elastic")
			},
			want: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "elastic")
			},
			want: false,
		},
		{
			name: "not in enabled list",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
			},
			want: false,
		},
		{
			name:     "default enabled when no env set",
			setupEnv: func(t *testing.T) {},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)
			assert.Equal(t, tt.want, enabler.Enable())
		})
	}
}

func TestParseElasticPath(t *testing.T) {
	tests := []struct {
		method, path, operation, index string
	}{
		{"POST", "/orders/_search", "search", "orders"},
		{"GET", "/orders/_search?pretty=true", "search", "orders"},
		{"PUT", "/orders/_doc/1", "index", "orders"},
		{"POST", "/orders/_doc/", "index", "orders"},
		{"GET", "/orders/_doc/1", "get", "orders"},
		{"DELETE", "/orders/_doc/1", "delete", "orders"},
		{"PUT", "/orders/_create/1", "create", "orders"},
		{"POST", "/orders/_update/1", "update", "orders"},
		{"POST", "/_bulk", "bulk", ""},
		{"POST", "/orders/_bulk", "bulk", "orders"},
		{"POST", "/orders,logs/_search", "search", "orders,logs"},
		{"POST", "/orders%2Clogs/_search", "search", "orders,logs"},
		{"POST", "/orders*/_search", "search", "orders*"},
		{"POST", "/caf%C3%A9/_search", "search", "café"},
		{"POST", "/orders/_doc/_search", "search", "orders"},
		{"GET", "/_cluster/health", "cluster.health", ""},
		{"GET", "/_nodes/http", "nodes.http", ""},
		{"GET", "/_nodes/n-123/stats", "nodes.stats", ""},
		{"GET", "/_tasks/a1b2c3d4", "tasks", ""},
		{"HEAD", "/orders", "exists", "orders"},
		{"PUT", "/orders", "create", "orders"},
		{"DELETE", "/orders", "delete", "orders"},
		{"GET", "/", "get", ""},
		{"POST", "/orders/_count", "count", "orders"},
		{"POST", "/orders/_delete_by_query", "delete_by_query", "orders"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			op, index := parseElasticPath(tt.method, tt.path)
			assert.Equal(t, tt.operation, op)
			assert.Equal(t, tt.index, index)
		})
	}
}

func TestBeforeAfterPerformRequest_Success(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "elastic")

	parent, parentSpan := otel.Tracer("test").Start(context.Background(), "inbound")
	defer parentSpan.End()

	opt := elastic.PerformRequestOptions{Method: "POST", Path: "/orders/_search"}
	ictx := hooktest.NewMockHookContext((*elastic.Client)(nil), parent, opt)

	BeforePerformRequest(ictx, nil, parent, opt)

	newCtx, ok := ictx.GetParam(ctxParamIndex).(context.Context)
	require.True(t, ok)
	require.True(t, trace.SpanContextFromContext(newCtx).IsValid())
	require.True(t, runtime.IsHTTPClientInstrumentationSuppressed(newCtx))
	require.Equal(t, parentSpan.SpanContext().TraceID(), trace.SpanContextFromContext(newCtx).TraceID())

	AfterPerformRequest(ictx, &elastic.Response{StatusCode: 200}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	got := spans[0]
	assert.Equal(t, "search orders", got.Name())
	assert.Equal(t, trace.SpanKindClient, got.SpanKind())
	assert.Equal(t, codes.Unset, got.Status().Code)
	assert.Equal(t, parentSpan.SpanContext().SpanID(), got.Parent().SpanID())

	attrs := attrMap(got.Attributes())
	assert.Equal(t, "elasticsearch", attrs["db.system.name"])
	assert.Equal(t, "search", attrs["db.operation.name"])
	assert.Equal(t, "orders", attrs["db.collection.name"])
	assert.Equal(t, "POST", attrs["http.request.method"])
	assert.Equal(t, "/orders/_search", attrs["url.path"])
	assert.Equal(t, "200", attrs["db.response.status_code"])
}

func TestBeforeAfterPerformRequest_Error(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "elastic")

	opt := elastic.PerformRequestOptions{Method: "GET", Path: "/orders/_doc/missing"}
	ictx := hooktest.NewMockHookContext()

	BeforePerformRequest(ictx, nil, context.Background(), opt)
	AfterPerformRequest(ictx, nil, &elastic.Error{Status: 404})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	got := spans[0]
	assert.Equal(t, codes.Error, got.Status().Code)
	attrs := attrMap(got.Attributes())
	assert.Equal(t, "get", attrs["db.operation.name"])
	assert.Equal(t, "404", attrs["db.response.status_code"])
	require.NotEmpty(t, got.Events(), "expected RecordError event")
}

func TestBeforeAfterPerformRequest_HTTPStatusError(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "elastic")

	opt := elastic.PerformRequestOptions{Method: "POST", Path: "/_bulk"}
	ictx := hooktest.NewMockHookContext()

	BeforePerformRequest(ictx, nil, context.Background(), opt)
	AfterPerformRequest(ictx, &elastic.Response{StatusCode: 429}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "bulk", spans[0].Name())
	assert.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestBeforeAfterPerformRequest_PlainError(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "elastic")

	opt := elastic.PerformRequestOptions{Method: "POST", Path: "/orders/_search"}
	ictx := hooktest.NewMockHookContext()

	BeforePerformRequest(ictx, nil, context.Background(), opt)
	AfterPerformRequest(ictx, nil, errors.New("connection refused"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Equal(t, "connection refused", spans[0].Status().Description)
}

func TestBeforeAfterPerformRequest_IgnoredStatus(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "elastic")

	opt := elastic.PerformRequestOptions{
		Method:       "HEAD",
		Path:         "/orders",
		IgnoreErrors: []int{404},
	}
	ictx := hooktest.NewMockHookContext()

	BeforePerformRequest(ictx, nil, context.Background(), opt)
	AfterPerformRequest(ictx, &elastic.Response{StatusCode: 404}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Unset, spans[0].Status().Code)
	assert.Equal(t, "404", attrMap(spans[0].Attributes())["db.response.status_code"])
}

func TestBeforePerformRequest_Disabled(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "elastic")

	opt := elastic.PerformRequestOptions{Method: "POST", Path: "/orders/_search"}
	ictx := hooktest.NewMockHookContext(nil, context.Background(), opt)

	BeforePerformRequest(ictx, nil, context.Background(), opt)
	AfterPerformRequest(ictx, &elastic.Response{StatusCode: 200}, nil)

	assert.Empty(t, sr.Ended())
	assert.Nil(t, ictx.GetData())
}

func TestAfterPerformRequest_NoBeforeSpan(t *testing.T) {
	sr := setupTestTracer(t)
	AfterPerformRequest(hooktest.NewMockHookContext(), &elastic.Response{StatusCode: 200}, nil)
	assert.Empty(t, sr.Ended())
}

func attrMap(attrs []attribute.KeyValue) map[string]interface{} {
	m := make(map[string]interface{}, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}
