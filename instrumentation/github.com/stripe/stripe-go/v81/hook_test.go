// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package v81

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	stripe "github.com/stripe/stripe-go/v81"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/instrumentation/github.com/stripe/stripe-go/v81/semconv"
	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func setupTestProviders(t *testing.T) (*tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	// Reset package-level init so each test binds to the test providers.
	initOnce = sync.Once{}
	tracer = nil

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})
	return sr, reader
}

func newRequest(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(context.Background(), method, u.String(), nil)
	require.NoError(t, err)
	return req
}

// newCallContext builds the mock hook context for
// (*BackendImplementation).requestWithRetriesAndTelemetry: receiver, request,
// body, handleResponse callback.
func newCallContext(req *http.Request) *hooktest.MockHookContext {
	return hooktest.NewMockHookContext(nil, req, nil, nil)
}

func attrMap(attrs []attribute.KeyValue) map[string]any {
	m := make(map[string]any, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}

func TestStripeEnabler(t *testing.T) {
	tests := []struct {
		name     string
		setupEnv func(t *testing.T)
		expected bool
	}{
		{"default enabled", func(t *testing.T) {}, true},
		{"enabled explicitly", func(t *testing.T) {
			t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "stripe")
		}, true},
		{"disabled explicitly", func(t *testing.T) {
			t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "stripe")
		}, false},
		{"not in enabled list", func(t *testing.T) {
			t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")
		}, false},
		{"enabled among many", func(t *testing.T) {
			t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp,stripe,grpc")
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)
			assert.Equal(t, tt.expected, enabler.Enable())
		})
	}
}

// A nil request must not panic; there is nothing to build a span from.
func TestBeforeRequestNilRequestIsNoop(t *testing.T) {
	sr, _ := setupTestProviders(t)
	ictx := newCallContext(nil)

	require.NotPanics(t, func() {
		BeforeRequest(ictx, nil, nil, nil, nil)
	})
	assert.Empty(t, sr.Ended())
}

// A request with no URL (never happens via net/http, but the hook signature
// allows it) must also bail out before touching req.URL.
func TestBeforeRequestNilURLIsNoop(t *testing.T) {
	sr, _ := setupTestProviders(t)
	req := &http.Request{}
	ictx := newCallContext(req)

	require.NotPanics(t, func() {
		BeforeRequest(ictx, nil, req, nil, nil)
	})
	assert.Same(t, req, ictx.GetParam(reqParamIndex))
	assert.Empty(t, sr.Ended())
}

func TestRequestSpanSuccess(t *testing.T) {
	sr, _ := setupTestProviders(t)

	req := newRequest(t, http.MethodGet, "https://api.stripe.com/v1/customers/cus_NffrFeUfNV2Hib")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil)

	resp := &http.Response{StatusCode: 200, Header: http.Header{}}
	resp.Header.Set("Request-Id", "req_abc123")
	AfterRequest(ictx, resp, nil, nil, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, "GET /v1/customers/{id}", span.Name())
	assert.Equal(t, codes.Unset, span.Status().Code)

	attrs := attrMap(span.Attributes())
	assert.Equal(t, "GET", attrs["http.request.method"])
	assert.Equal(t, "/v1/customers/{id}", attrs["url.template"])
	assert.Equal(t, "/v1/customers/cus_NffrFeUfNV2Hib", attrs["url.path"])
	assert.Equal(t, "api.stripe.com", attrs["server.address"])
	assert.Equal(t, int64(200), attrs["http.response.status_code"])
	assert.Equal(t, "req_abc123", attrs["stripe.request_id"])
}

// The outgoing request must be re-parented so per-attempt net/http spans nest
// under the Stripe span instead of becoming trace roots.
func TestRequestIsReparentedOntoSpanContext(t *testing.T) {
	sr, _ := setupTestProviders(t)

	req := newRequest(t, http.MethodPost, "https://api.stripe.com/v1/charges")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil)

	rewritten, ok := ictx.GetParam(reqParamIndex).(*http.Request)
	require.True(t, ok, "request parameter must be replaced")
	require.NotSame(t, req, rewritten)

	AfterRequest(ictx, &http.Response{StatusCode: 200, Header: http.Header{}}, nil, nil, nil)
	require.Len(t, sr.Ended(), 1)

	injected := trace.SpanContextFromContext(rewritten.Context())
	assert.True(t, injected.IsValid())
	assert.Equal(t, sr.Ended()[0].SpanContext().SpanID(), injected.SpanID())
}

func TestRequestSpanStripeError(t *testing.T) {
	sr, _ := setupTestProviders(t)

	req := newRequest(t, http.MethodPost, "https://api.stripe.com/v1/charges")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil)
	// The backend returns a nil response alongside the error.
	AfterRequest(ictx, nil, nil, nil, &stripe.Error{
		HTTPStatusCode: 402,
		Code:           stripe.ErrorCodeCardDeclined,
		Type:           stripe.ErrorTypeCard,
		RequestID:      "req_abc123",
		Msg:            "Your card was declined.",
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, codes.Error, span.Status().Code)
	attrs := attrMap(span.Attributes())
	assert.Equal(t, int64(402), attrs["http.response.status_code"])
	assert.Equal(t, "402", attrs["error.type"])
	assert.Equal(t, "card_declined", attrs["stripe.error.code"])
	assert.Equal(t, "req_abc123", attrs["stripe.request_id"])
	assert.NotEmpty(t, span.Events(), "RecordError must add an exception event")
}

func TestRequestSpanTransportError(t *testing.T) {
	sr, _ := setupTestProviders(t)

	req := newRequest(t, http.MethodGet, "https://api.stripe.com/v1/charges")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil)
	AfterRequest(ictx, nil, nil, nil, &url.Error{
		Op:  "Get",
		URL: "https://api.stripe.com/v1/charges",
		Err: errors.New("dial tcp: connection refused"),
	})

	spans := sr.Ended()
	require.Len(t, spans, 1)
	attrs := attrMap(spans[0].Attributes())
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.NotContains(t, attrs, "http.response.status_code")
	// error.type is required whenever a request ends in error, even when Stripe
	// never answered and there is no status code to report.
	assert.Equal(t, "*url.Error", attrs["error.type"])
}

// server.port is conditionally required, and a non-default port (any mock or
// proxy setup, including the integration test's httptest server) must report it.
func TestServerPortRecorded(t *testing.T) {
	sr, _ := setupTestProviders(t)

	req := newRequest(t, http.MethodGet, "http://127.0.0.1:8931/v1/charges")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil)
	AfterRequest(ictx, &http.Response{StatusCode: 200, Header: http.Header{}}, nil, nil, nil)

	require.Len(t, sr.Ended(), 1)
	attrs := attrMap(sr.Ended()[0].Attributes())
	assert.Equal(t, "127.0.0.1", attrs["server.address"])
	assert.Equal(t, int64(8931), attrs["server.port"])
}

// An implicit port must still be reported, resolved from the scheme.
func TestServerPortDefaultsFromScheme(t *testing.T) {
	sr, _ := setupTestProviders(t)

	req := newRequest(t, http.MethodGet, "https://api.stripe.com/v1/charges")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil)
	AfterRequest(ictx, &http.Response{StatusCode: 200, Header: http.Header{}}, nil, nil, nil)

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, int64(443), attrMap(sr.Ended()[0].Attributes())["server.port"])
}

// The gate is deliberately span-presence rather than the instrument guide's
// SetKeyData pattern: if the before hook did not start a span, the after hook
// closes nothing. That keeps the pair balanced when the environment variable
// flips between the two halves of one call.
func TestGateStaysBalancedWhenDisabledMidFlight(t *testing.T) {
	sr, _ := setupTestProviders(t)

	req := newRequest(t, http.MethodGet, "https://api.stripe.com/v1/charges")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil) // enabled: starts a span
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "stripe")
	require.NotPanics(t, func() {
		AfterRequest(ictx, &http.Response{StatusCode: 200, Header: http.Header{}}, nil, nil, nil)
	})

	// The span the before hook opened must still be ended, not leaked.
	require.Len(t, sr.Ended(), 1)
}

func TestDisabledInstrumentationEmitsNothing(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "stripe")
	sr, _ := setupTestProviders(t)

	req := newRequest(t, http.MethodGet, "https://api.stripe.com/v1/charges")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil)
	AfterRequest(ictx, &http.Response{StatusCode: 200, Header: http.Header{}}, nil, nil, nil)

	assert.Empty(t, sr.Ended())
	assert.Same(t, req, ictx.GetParam(reqParamIndex), "disabled hook must not rewrite params")
}

// An after hook that runs without its before hook (instrumentation switched on
// mid-flight) must be a no-op rather than panicking or emitting a partial span.
func TestAfterWithoutBeforeIsNoop(t *testing.T) {
	sr, _ := setupTestProviders(t)

	ictx := newCallContext(newRequest(t, http.MethodGet, "https://api.stripe.com/v1/charges"))
	require.NotPanics(t, func() {
		AfterRequest(ictx, &http.Response{StatusCode: 200, Header: http.Header{}}, nil, nil, nil)
	})
	assert.Empty(t, sr.Ended())
}

func TestRequestDurationMetricRecorded(t *testing.T) {
	_, reader := setupTestProviders(t)

	req := newRequest(t, http.MethodGet, "https://api.stripe.com/v1/customers/cus_NffrFeUfNV2Hib")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil)
	AfterRequest(ictx, &http.Response{StatusCode: 200, Header: http.Header{}}, nil, nil, nil)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	var hist metricdata.Histogram[float64]
	var found bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == semconv.MetricRequestDuration {
				hist, found = m.Data.(metricdata.Histogram[float64])
			}
		}
	}
	require.True(t, found, "expected %s histogram", semconv.MetricRequestDuration)
	require.Len(t, hist.DataPoints, 1)

	labels := map[string]any{}
	for _, kv := range hist.DataPoints[0].Attributes.ToSlice() {
		labels[string(kv.Key)] = kv.Value.AsInterface()
	}
	assert.Equal(t, "/v1/customers/{id}", labels["url.template"])
	assert.NotContains(t, labels, "url.path", "metric labels must stay bounded")
}

func TestAPIErrorFrom(t *testing.T) {
	typ := stripe.ErrorTypeInvalidRequest

	tests := []struct {
		name string
		err  error
		want semconv.APIError
	}{
		{
			name: "v1 error",
			err:  &stripe.Error{HTTPStatusCode: 404, Code: "resource_missing", Type: typ, RequestID: "req_1"},
			want: semconv.APIError{StatusCode: 404, Code: "resource_missing", Type: string(typ), RequestID: "req_1"},
		},
		{
			name: "wrapped v1 error",
			err:  fmt.Errorf("charging failed: %w", &stripe.Error{HTTPStatusCode: 402, Type: stripe.ErrorTypeCard}),
			want: semconv.APIError{StatusCode: 402, Type: string(stripe.ErrorTypeCard)},
		},
		// v81 has no v2 error shape; see the v82 package for that case.
		{
			name: "transport error",
			err:  errors.New("connection refused"),
			want: semconv.APIError{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, apiErrorFrom(tt.err))
		})
	}
}

// The span must cover the backend's whole retry loop, not one attempt, so its
// duration is measured from the before hook rather than from the per-attempt
// duration the backend returns.
func TestSpanCoversFullRetryLoop(t *testing.T) {
	sr, _ := setupTestProviders(t)

	req := newRequest(t, http.MethodGet, "https://api.stripe.com/v1/charges")
	ictx := newCallContext(req)

	BeforeRequest(ictx, nil, req, nil, nil)
	time.Sleep(10 * time.Millisecond)
	lastAttempt := time.Millisecond
	AfterRequest(ictx, &http.Response{StatusCode: 200, Header: http.Header{}}, nil, &lastAttempt, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Greater(t, spans[0].EndTime().Sub(spans[0].StartTime()), lastAttempt)
}
