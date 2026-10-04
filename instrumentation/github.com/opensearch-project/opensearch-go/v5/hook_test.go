// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v5

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/opensearch-project/opensearch-go/v5"
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

func TestOpenSearchEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		want     bool
	}{
		{
			name: "enabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "opensearch")
			},
			want: true,
		},
		{
			name: "disabled explicitly",
			setupEnv: func(t *testing.T) {
				t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "opensearch")
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

func TestParseOpenSearchPath(t *testing.T) {
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
		{"POST", "/m%C3%BCnchen/_search", "search", "münchen"},
		{"POST", "/orders/_doc/_search", "search", "orders"},
		{"GET", "/_cluster/health", "cluster.health", ""},
		{"GET", "/_nodes/http", "nodes.http", ""},
		{"GET", "/_nodes/n-123/stats", "nodes.stats", ""},
		{"GET", "/_tasks/a1b2c3d4", "tasks", ""},
		{"GET", "/_cat/indices", "cat.indices", ""},
		{"POST", "/orders/_pit", "pit", "orders"},
		{"POST", "/_search/pipeline", "search", ""},
		{"HEAD", "/orders", "exists", "orders"},
		{"PUT", "/orders", "create", "orders"},
		{"DELETE", "/orders", "delete", "orders"},
		{"GET", "/", "get", ""},
		{"POST", "/orders/_count", "count", "orders"},
		{"POST", "/orders/_delete_by_query", "delete_by_query", "orders"},
		{"POST", "/orders/_reindex", "reindex", "orders"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			op, index := parseOpenSearchPath(tt.method, tt.path)
			assert.Equal(t, tt.operation, op)
			assert.Equal(t, tt.index, index)
		})
	}
}

func TestBeforeAfterRequest_Success(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "opensearch")

	parent, parentSpan := otel.Tracer("test").Start(context.Background(), "inbound")
	defer parentSpan.End()

	req, _ := http.NewRequestWithContext(parent, "POST", "https://opensearch.example:9200/orders/_search", nil)
	ictx := hooktest.NewMockHookContext((*opensearch.Client)(nil), req)

	BeforeRequest(ictx, nil, req)

	newReq, ok := ictx.GetParam(reqParamIndex).(*http.Request)
	require.True(t, ok)
	require.Same(t, newReq, ictx.GetParam(1), "request parameter must be replaced at index 1, after the receiver")
	require.NotSame(t, req, newReq, "request must be cloned with the span context")
	require.Nil(t, ictx.GetParam(0), "receiver must not be overwritten")
	require.True(t, trace.SpanContextFromContext(newReq.Context()).IsValid())
	require.True(t, runtime.IsHTTPClientInstrumentationSuppressed(newReq.Context()))
	require.Equal(t, parentSpan.SpanContext().TraceID(), trace.SpanContextFromContext(newReq.Context()).TraceID())

	AfterRequest(ictx, &http.Response{StatusCode: 200}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	got := spans[0]
	assert.Equal(t, "search orders", got.Name())
	assert.Equal(t, trace.SpanKindClient, got.SpanKind())
	assert.Equal(t, codes.Unset, got.Status().Code)
	assert.Equal(t, parentSpan.SpanContext().SpanID(), got.Parent().SpanID())

	attrs := attrMap(got.Attributes())
	assert.Equal(t, "opensearch", attrs["db.system.name"])
	assert.Equal(t, "search", attrs["db.operation.name"])
	assert.Equal(t, "orders", attrs["db.collection.name"])
	assert.Equal(t, "POST", attrs["http.request.method"])
	assert.Equal(t, "/orders/_search", attrs["url.path"])
	assert.Equal(t, "opensearch.example", attrs["server.address"])
	assert.Equal(t, int64(9200), attrs["server.port"])
	assert.Equal(t, "200", attrs["db.response.status_code"])
}

func TestBeforeAfterRequest_Error(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "opensearch")

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://localhost:9200/orders/_doc/missing", nil)
	ictx := hooktest.NewMockHookContext((*opensearch.Client)(nil), req)

	BeforeRequest(ictx, nil, req)
	AfterRequest(ictx, nil, errors.New("connection refused"))

	spans := sr.Ended()
	require.Len(t, spans, 1)
	got := spans[0]
	assert.Equal(t, codes.Error, got.Status().Code)
	assert.Equal(t, "connection refused", got.Status().Description)
	attrs := attrMap(got.Attributes())
	assert.Equal(t, "get", attrs["db.operation.name"])
	assert.Equal(t, "*errors.errorString", attrs["error.type"])
	require.NotEmpty(t, got.Events(), "expected RecordError event")
}

func TestBeforeAfterRequest_HTTPStatusError(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "opensearch")

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://localhost:9200/_bulk", nil)
	ictx := hooktest.NewMockHookContext((*opensearch.Client)(nil), req)

	BeforeRequest(ictx, nil, req)
	AfterRequest(ictx, &http.Response{StatusCode: 429}, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "bulk", spans[0].Name())
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	attrs := attrMap(spans[0].Attributes())
	assert.Equal(t, "429", attrs["db.response.status_code"])
	assert.Equal(t, "429", attrs["error.type"])
}

func TestBeforeRequest_Disabled(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "opensearch")

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://localhost:9200/orders/_search", nil)
	ictx := hooktest.NewMockHookContext((*opensearch.Client)(nil), req)

	BeforeRequest(ictx, nil, req)
	AfterRequest(ictx, &http.Response{StatusCode: 200}, nil)

	assert.Empty(t, sr.Ended())
	assert.Nil(t, ictx.GetData())
}

func TestBeforeRequest_NilRequest(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "opensearch")

	ictx := hooktest.NewMockHookContext((*opensearch.Client)(nil), (*http.Request)(nil))

	BeforeRequest(ictx, nil, nil)
	AfterRequest(ictx, &http.Response{StatusCode: 200}, nil)

	assert.Empty(t, sr.Ended())
}

func TestBeforeRequest_NilURL(t *testing.T) {
	sr := setupTestTracer(t)
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "opensearch")

	req := &http.Request{Method: http.MethodGet}
	ictx := hooktest.NewMockHookContext((*opensearch.Client)(nil), req)

	BeforeRequest(ictx, nil, req)
	AfterRequest(ictx, nil, errors.New("missing URL"))

	assert.Empty(t, sr.Ended())
	assert.Nil(t, ictx.GetData())
}

func TestAfterRequest_NoBeforeSpan(t *testing.T) {
	sr := setupTestTracer(t)
	AfterRequest(hooktest.NewMockHookContext(), &http.Response{StatusCode: 200}, nil)
	assert.Empty(t, sr.Ended())
}

func attrMap(attrs []attribute.KeyValue) map[string]interface{} {
	m := make(map[string]interface{}, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}
