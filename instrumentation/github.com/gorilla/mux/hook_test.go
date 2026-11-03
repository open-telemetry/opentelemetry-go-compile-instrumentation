// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func setupContextTracer(t *testing.T) (*tracetest.SpanRecorder, trace.Tracer) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	return sr, tp.Tracer("test")
}

func matchRequest(
	t *testing.T,
	r *mux.Router,
	method, url string,
	span trace.Span,
) (*http.Request, mux.RouteMatch, bool) {
	t.Helper()
	req := httptest.NewRequest(method, url, nil)
	if span != nil {
		req = req.WithContext(trace.ContextWithSpan(context.Background(), span))
	}
	var match mux.RouteMatch
	ok := r.Match(req, &match)
	return req, match, ok
}

func afterMatch(req *http.Request, match *mux.RouteMatch, ok bool) {
	AfterMatch(hooktest.NewMockHookContext(mux.NewRouter(), req, match), ok)
}

func routeAttrs(span sdktrace.ReadOnlySpan) map[string]any {
	attrs := map[string]any{}
	for _, a := range span.Attributes() {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}
	return attrs
}

func TestAfterMatch_UpdatesSpanNameAndRoute(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	req, match, ok := matchRequest(t, r, http.MethodGet, "/users/42", span)
	require.True(t, ok)
	require.NoError(t, match.MatchErr)

	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]
	assert.Equal(t, "GET /users/{id}", ended.Name())
	assert.Equal(t, "/users/{id}", routeAttrs(ended)["http.route"])
}

func TestAfterMatch_SubrouterUsesFullPathTemplate(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	r := mux.NewRouter()
	s := r.PathPrefix("/api").Subrouter()
	s.HandleFunc("/apps/{appId}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	req, match, ok := matchRequest(t, r, http.MethodGet, "/api/apps/7", span)
	require.True(t, ok)
	require.NotNil(t, match.Route)
	tpl, err := match.Route.GetPathTemplate()
	require.NoError(t, err)

	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]
	assert.Equal(t, "GET "+tpl, ended.Name())
	assert.Equal(t, tpl, routeAttrs(ended)["http.route"])
	assert.Contains(t, tpl, "{appId}")
}

func TestAfterMatch_FalseMatchWithStaleRouteDoesNotSetRoute(t *testing.T) {
	// Match can return false with Route still set (stale or caller-filled)
	// and MatchErr nil. The bool is the match result; do not enrich.
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	_, okMatch, ok := matchRequest(t, r, http.MethodGet, "/users/42", nil)
	require.True(t, ok)
	require.NotNil(t, okMatch.Route)
	require.NoError(t, okMatch.MatchErr)

	req := httptest.NewRequest(http.MethodGet, "/stale", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	stale := mux.RouteMatch{Route: okMatch.Route}
	afterMatch(req, &stale, false)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name())
	assert.NotContains(t, routeAttrs(sr.Ended()[0]), "http.route")
}

func TestAfterMatch_UnmatchedRouteIsNoop(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	req, match, ok := matchRequest(t, r, http.MethodGet, "/does-not-exist", span)
	require.False(t, ok)
	require.ErrorIs(t, match.MatchErr, mux.ErrNotFound)

	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name())
	assert.NotContains(t, routeAttrs(sr.Ended()[0]), "http.route")
}

func TestAfterMatch_MethodNotAllowedDoesNotSetRoute(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "POST")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	req, match, ok := matchRequest(t, r, http.MethodPost, "/users/42", span)
	require.False(t, ok)
	require.ErrorIs(t, match.MatchErr, mux.ErrMethodMismatch)

	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "POST", sr.Ended()[0].Name())
	assert.NotContains(t, routeAttrs(sr.Ended()[0]), "http.route")
}

func TestAfterMatch_MethodNotAllowedWithLeftoverRouteDoesNotSetRoute(t *testing.T) {
	// Mux leaves Route unset on 405 today. If that changes, or a caller
	// fills Route while MatchErr is ErrMethodMismatch, we still must not
	// treat the path template as a matched route (echo's 405 Path() case).
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "POST")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	okReq, okMatch, ok := matchRequest(t, r, http.MethodGet, "/users/42", nil)
	require.True(t, ok)
	require.NotNil(t, okMatch.Route)
	_ = okReq

	req := httptest.NewRequest(http.MethodPost, "/users/42", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	leftover := mux.RouteMatch{Route: okMatch.Route, MatchErr: mux.ErrMethodMismatch}
	afterMatch(req, &leftover, true)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "POST", sr.Ended()[0].Name())
	assert.NotContains(t, routeAttrs(sr.Ended()[0]), "http.route")
}

func TestAfterMatch_CustomMethodNotAllowedHandlerDoesNotSetRoute(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "POST")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	r.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	req, match, ok := matchRequest(t, r, http.MethodPost, "/users/42", span)
	require.True(t, ok, "custom MethodNotAllowedHandler makes Match return true")
	require.ErrorIs(t, match.MatchErr, mux.ErrMethodMismatch)

	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "POST", sr.Ended()[0].Name())
	assert.NotContains(t, routeAttrs(sr.Ended()[0]), "http.route")
}

func TestAfterMatch_CustomNotFoundHandlerDoesNotSetRoute(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	req, match, ok := matchRequest(t, r, http.MethodGet, "/does-not-exist", span)
	require.True(t, ok, "custom NotFoundHandler makes Match return true")
	require.ErrorIs(t, match.MatchErr, mux.ErrNotFound)

	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name())
	assert.NotContains(t, routeAttrs(sr.Ended()[0]), "http.route")
}

func TestAfterMatch_IdempotentOnRepeatedMatch(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	r := mux.NewRouter()
	r.HandleFunc("/items/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	req, match, ok := matchRequest(t, r, http.MethodGet, "/items/7", span)
	require.True(t, ok)

	afterMatch(req, &match, ok)
	afterMatch(req, &match, ok)
	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	var routeAttrCount int
	for _, a := range sr.Ended()[0].Attributes() {
		if string(a.Key) == "http.route" {
			routeAttrCount++
		}
	}
	assert.Equal(t, 1, routeAttrCount)
}

func TestAfterMatch_NonRecordingSpanDoesNotEnrich(t *testing.T) {
	sr, _ := setupContextTracer(t)

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	req, match, ok := matchRequest(t, r, http.MethodGet, "/users/42", nil)
	require.True(t, ok)

	afterMatch(req, &match, ok)
	assert.Empty(t, sr.Ended())
}

func TestAfterMatch_DisabledInstrumentation(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "mux")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	req, match, ok := matchRequest(t, r, http.MethodGet, "/users/42", span)
	require.True(t, ok)

	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name())
	assert.NotContains(t, routeAttrs(sr.Ended()[0]), "http.route")
}

func TestAfterMatch_NotInEnabledList(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	req, match, ok := matchRequest(t, r, http.MethodGet, "/users/42", span)
	require.True(t, ok)

	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name())
}

func TestAfterMatch_InEnabledList(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp,mux")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	r := mux.NewRouter()
	r.HandleFunc("/users/{id}", func(http.ResponseWriter, *http.Request) {}).Methods(http.MethodGet)
	req, match, ok := matchRequest(t, r, http.MethodGet, "/users/42", span)
	require.True(t, ok)

	afterMatch(req, &match, ok)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET /users/{id}", sr.Ended()[0].Name())
}

func TestAfterMatch_NilHookContextIsNoop(t *testing.T) {
	AfterMatch(nil, true)
}

func TestAfterMatch_NilMatchIsNoop(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))

	afterMatch(req, nil, true)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name())
}
