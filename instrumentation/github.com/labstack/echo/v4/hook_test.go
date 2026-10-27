// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook/hooktest"
)

func newEchoContextWithRoute(t *testing.T, method, routePattern, url string, span trace.Span) echo.Context {
	t.Helper()

	var captured echo.Context
	e := echo.New()
	e.Add(method, routePattern, func(c echo.Context) error {
		captured = c
		return nil
	})

	req := httptest.NewRequest(method, url, nil)
	if span != nil {
		req = req.WithContext(trace.ContextWithSpan(context.Background(), span))
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.NotNil(t, captured, "no handler was invoked; check route pattern and URL")
	return captured
}

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

func TestAfterFind_UpdatesSpanNameAndRoute(t *testing.T) {
	sr, tr := setupContextTracer(t)

	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	AfterFind(hooktest.NewMockHookContext(c))

	span.End()
	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]

	assert.Equal(t, "GET /users/:id", ended.Name())

	attrs := map[string]any{}
	for _, a := range ended.Attributes() {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}
	assert.Equal(t, "/users/:id", attrs["http.route"])
}

func TestAfterFind_EmptyRouteIsNoop(t *testing.T) {
	sr, tr := setupContextTracer(t)

	_, span := tr.Start(context.Background(), "GET")
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	c := e.NewContext(req, httptest.NewRecorder())

	AfterFind(hooktest.NewMockHookContext(c))
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name())
}

func TestAfterFind_IdempotentOnRepeatedFind(t *testing.T) {
	sr, tr := setupContextTracer(t)

	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/items/:id", "/items/7", span)
	ictx := hooktest.NewMockHookContext(c)

	AfterFind(ictx)
	AfterFind(ictx)
	AfterFind(ictx)

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

func TestAfterFind_NonRecordingSpanDoesNotBurnGate(t *testing.T) {
	sr, tr := setupContextTracer(t)

	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", nil)
	ictx := hooktest.NewMockHookContext(c)
	AfterFind(ictx)

	_, recording := tr.Start(context.Background(), "GET")
	c.SetRequest(c.Request().WithContext(
		trace.ContextWithSpan(c.Request().Context(), recording),
	))
	AfterFind(ictx)
	recording.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET /users/:id", sr.Ended()[0].Name())
}

func TestAfterFind_DisabledInstrumentation(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "echo")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	AfterFind(hooktest.NewMockHookContext(c))
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name())

	attrs := map[string]any{}
	for _, a := range sr.Ended()[0].Attributes() {
		attrs[string(a.Key)] = a.Value.AsInterface()
	}
	assert.NotContains(t, attrs, "http.route")
}

func TestAfterFind_NotInEnabledList(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	AfterFind(hooktest.NewMockHookContext(c))
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET", sr.Ended()[0].Name())
}

func TestAfterFind_MethodNotAllowedDoesNotSetRoute(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "POST")

	e := echo.New()
	e.GET("/users/:id", func(echo.Context) error { return nil })
	req := httptest.NewRequest(http.MethodPost, "/users/42", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	c := e.NewContext(req, httptest.NewRecorder())
	e.Router().Find(http.MethodPost, "/users/42", c)

	require.NotEmpty(t, c.Path(), "Find writes Path() on 405; that is the case under test")
	require.Equal(t,
		reflect.ValueOf(echo.MethodNotAllowedHandler).Pointer(),
		reflect.ValueOf(c.Handler()).Pointer(),
	)

	AfterFind(hooktest.NewMockHookContext(c))
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "POST", sr.Ended()[0].Name())
	for _, a := range sr.Ended()[0].Attributes() {
		assert.NotEqual(t, "http.route", string(a.Key))
	}
}

func TestAfterFind_MethodNotAllowedWithoutHeaderAllowKey(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "POST")

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/users/42", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	c := e.NewContext(req, httptest.NewRecorder())
	c.SetPath("/users/:id")
	c.SetHandler(echo.MethodNotAllowedHandler)
	require.Nil(t, c.Get(echo.ContextKeyHeaderAllow))

	AfterFind(hooktest.NewMockHookContext(c))
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "POST", sr.Ended()[0].Name())
	for _, a := range sr.Ended()[0].Attributes() {
		assert.NotEqual(t, "http.route", string(a.Key))
	}
}

func TestAfterFind_InEnabledList(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp,echo")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	AfterFind(hooktest.NewMockHookContext(c))
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, "GET /users/:id", sr.Ended()[0].Name())
}

func TestBeforeError_RecordsError(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	BeforeError(hooktest.NewMockHookContext(c), c, errors.New("db connection lost"))
	span.End()

	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]
	assert.Equal(t, codes.Error, ended.Status().Code)
	assert.Contains(t, ended.Status().Description, "db connection lost")
	require.Len(t, ended.Events(), 1)
	assert.Equal(t, "exception", ended.Events()[0].Name)
}

func TestBeforeError_DisabledInstrumentation(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "echo")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	BeforeError(hooktest.NewMockHookContext(c), c, errors.New("db connection lost"))
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, codes.Unset, sr.Ended()[0].Status().Code)
	assert.Empty(t, sr.Ended()[0].Events())
}

func TestBeforeError_InEnabledList(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp,echo")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	BeforeError(hooktest.NewMockHookContext(c), c, errors.New("db connection lost"))
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, codes.Error, sr.Ended()[0].Status().Code)
}

func TestBeforeError_NilErrorIsNoop(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/ping", "/ping", span)

	BeforeError(hooktest.NewMockHookContext(c), c, nil)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, codes.Unset, sr.Ended()[0].Status().Code)
	assert.Empty(t, sr.Ended()[0].Events())
}

func TestBeforeDefaultHTTPErrorHandler_DisabledInstrumentation(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "echo")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	BeforeDefaultHTTPErrorHandler(hooktest.NewMockHookContext(c), echo.New(), errors.New("handler returned error"), c)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, codes.Unset, sr.Ended()[0].Status().Code)
	assert.Empty(t, sr.Ended()[0].Events())
}

func TestBeforeDefaultHTTPErrorHandler_InEnabledList(t *testing.T) {
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "nethttp,echo")

	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	BeforeDefaultHTTPErrorHandler(hooktest.NewMockHookContext(c), echo.New(), errors.New("handler returned error"), c)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, codes.Error, sr.Ended()[0].Status().Code)
}

func TestBeforeDefaultHTTPErrorHandler_RecordsReturnedError(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	BeforeDefaultHTTPErrorHandler(hooktest.NewMockHookContext(c), echo.New(), errors.New("handler returned error"), c)
	span.End()

	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]
	assert.Equal(t, codes.Error, ended.Status().Code)
	assert.Contains(t, ended.Status().Description, "handler returned error")
	require.Len(t, ended.Events(), 1)
}

func TestBeforeDefaultHTTPErrorHandler_Skips4xx(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	BeforeDefaultHTTPErrorHandler(
		hooktest.NewMockHookContext(c),
		echo.New(),
		echo.NewHTTPError(http.StatusNotFound, "missing"),
		c,
	)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, codes.Unset, sr.Ended()[0].Status().Code)
	assert.Empty(t, sr.Ended()[0].Events())
}

func TestBeforeDefaultHTTPErrorHandler_NestedInternal4xxSkipped(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	outer := echo.NewHTTPError(http.StatusInternalServerError, "wrapper")
	outer.Internal = echo.NewHTTPError(http.StatusNotFound, "missing")
	BeforeDefaultHTTPErrorHandler(hooktest.NewMockHookContext(c), echo.New(), outer, c)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, codes.Unset, sr.Ended()[0].Status().Code)
	assert.Empty(t, sr.Ended()[0].Events())
}

func TestBeforeDefaultHTTPErrorHandler_NestedInternal5xxRecorded(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)

	outer := echo.NewHTTPError(http.StatusNotFound, "wrapper")
	outer.Internal = echo.NewHTTPError(http.StatusInternalServerError, "boom")
	BeforeDefaultHTTPErrorHandler(hooktest.NewMockHookContext(c), echo.New(), outer, c)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Equal(t, codes.Error, sr.Ended()[0].Status().Code)
	require.Len(t, sr.Ended()[0].Events(), 1)
}

// serveFlowWithSpan replicates the instrumented (*Echo).ServeHTTP request
// flow on a caller-created echo.Context: (*Router).Find runs with the
// injected AfterFind hook, ServeHTTP then reads c.Handler() and executes it,
// and dispatches a returned error to e.HTTPErrorHandler.
func serveFlowWithSpan(e *echo.Echo, c echo.Context) {
	e.Router().Find(c.Request().Method, c.Request().URL.Path, c)
	AfterFind(hooktest.NewMockHookContext(c))
	if err := c.Handler()(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}
}

func TestAfterFind_WrapsHandlerForCustomHTTPErrorHandler(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	e := echo.New()
	var customCalled bool
	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		customCalled = true
		_ = ctx.NoContent(http.StatusInternalServerError)
	}
	e.GET("/users/:id", func(echo.Context) error { return errors.New("handler returned error") })

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	serveFlowWithSpan(e, e.NewContext(req, httptest.NewRecorder()))
	span.End()

	assert.True(t, customCalled)
	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]
	assert.Equal(t, "GET /users/:id", ended.Name())
	assert.Equal(t, codes.Error, ended.Status().Code)
	assert.Contains(t, ended.Status().Description, "handler returned error")
	require.Len(t, ended.Events(), 1)
	assert.Equal(t, "exception", ended.Events()[0].Name)
}

func TestAfterFind_WrapsHandlerOnceOnRepeatedFind(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")

	e := echo.New()
	e.GET("/users/:id", func(echo.Context) error { return errors.New("handler returned error") })

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil).
		WithContext(trace.ContextWithSpan(context.Background(), span))
	c := e.NewContext(req, httptest.NewRecorder())

	// (*Router).Find runs once per request in ServeHTTP; repeated AfterFind
	// calls on the same context must not nest wrappers and re-record.
	e.Router().Find(http.MethodGet, "/users/42", c)
	ictx := hooktest.NewMockHookContext(c)
	AfterFind(ictx)
	AfterFind(ictx)
	require.Error(t, c.Handler()(c), "wrapped handler must pass the error through")
	span.End()

	require.Len(t, sr.Ended(), 1)
	ended := sr.Ended()[0]
	assert.Equal(t, codes.Error, ended.Status().Code)
	assert.Len(t, ended.Events(), 1)
}

func TestAfterFind_DisabledDoesNotWrapHandler(t *testing.T) {
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "echo")

	e := echo.New()
	e.GET("/users/:id", func(echo.Context) error { return nil })

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	c := e.NewContext(req, httptest.NewRecorder())
	e.Router().Find(http.MethodGet, "/users/42", c)
	orig := reflect.ValueOf(c.Handler()).Pointer()

	AfterFind(hooktest.NewMockHookContext(c))

	assert.Equal(t, orig, reflect.ValueOf(c.Handler()).Pointer())
}

// TestAfterFind_ConcurrentRequestsAreRaceFree drives concurrent initial requests:
// concurrent requests on one *Echo with a custom e.HTTPErrorHandler. The
// previous fix wrote e.HTTPErrorHandler from a before-hook on ServeHTTP,
// racing Echo's unlocked read of that field in ServeHTTP. The handler wrap
// only mutates the per-request echo.Context, so this must stay clean under
// the race detector.
func TestAfterFind_ConcurrentRequestsAreRaceFree(t *testing.T) {
	sr, tr := setupContextTracer(t)

	e := echo.New()
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		_ = c.NoContent(http.StatusInternalServerError)
	}
	e.GET("/boom", func(echo.Context) error { return errors.New("handler returned error") })

	const goroutines = 20
	const requests = 25

	start := make(chan struct{})
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range requests {
				_, span := tr.Start(context.Background(), "GET")
				req := httptest.NewRequest(http.MethodGet, "/boom", nil).
					WithContext(trace.ContextWithSpan(context.Background(), span))
				serveFlowWithSpan(e, e.NewContext(req, httptest.NewRecorder()))
				span.End()
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Len(t, sr.Ended(), goroutines*requests)
	for _, ended := range sr.Ended() {
		assert.Equal(t, "GET /boom", ended.Name())
		assert.Equal(t, codes.Error, ended.Status().Code)
		assert.Len(t, ended.Events(), 1)
	}
}

func TestRecordRequestError_IdempotentAcrossBothHooks(t *testing.T) {
	sr, tr := setupContextTracer(t)
	_, span := tr.Start(context.Background(), "GET")
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", span)
	err := errors.New("db connection lost")

	BeforeError(hooktest.NewMockHookContext(c), c, err)
	BeforeDefaultHTTPErrorHandler(hooktest.NewMockHookContext(c), echo.New(), err, c)
	span.End()

	require.Len(t, sr.Ended(), 1)
	assert.Len(t, sr.Ended()[0].Events(), 1)
}

func TestBeforeError_NonRecordingSpanSkipsRecording(t *testing.T) {
	sr, _ := setupContextTracer(t)
	c := newEchoContextWithRoute(t, http.MethodGet, "/users/:id", "/users/42", nil)

	BeforeError(hooktest.NewMockHookContext(c), c, errors.New("should not be recorded"))

	assert.Empty(t, sr.Ended())
}
