// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/otelc/pkg/hook"
)

type mockHookContext struct {
	hook.HookContext
	params  []any
	keyData map[string]any
}

func newMockHookContext(params ...any) *mockHookContext {
	return &mockHookContext{
		params:  params,
		keyData: make(map[string]any),
	}
}

func (m *mockHookContext) GetParam(i int) any {
	if i < len(m.params) {
		return m.params[i]
	}
	return nil
}

func (m *mockHookContext) SetKeyData(key string, val any) { m.keyData[key] = val }

func (m *mockHookContext) GetKeyData(key string) any { return m.keyData[key] }

// newRecordingTracer installs a real SDK tracer so the hooks exercise the same
// code path they do in a build, and returns the recorder holding finished spans.
func newRecordingTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer = tp.Tracer("test")
	propagator = propagation.TraceContext{}
	// initInstrumentation is sync.Once-guarded and would otherwise replace the
	// tracer above the first time a hook runs.
	initOnce.Do(func() {})
	t.Cleanup(func() { require.NoError(t, tp.Shutdown(context.Background())) })

	return recorder
}

func attrsOf(t *testing.T, span sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	t.Helper()

	got := make(map[attribute.Key]attribute.Value, len(span.Attributes()))
	for _, kv := range span.Attributes() {
		got[kv.Key] = kv.Value
	}
	return got
}

// runRequest drives one request through a Fiber app whose route handler invokes
// the hooks the way the injected trampoline does.
func runRequest(t *testing.T, recorder *tracetest.SpanRecorder, method, target string,
	register func(app *fiber.App),
) []sdktrace.ReadOnlySpan {
	t.Helper()

	app := fiber.New()
	register(app)

	req := httptest.NewRequest(method, target, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return recorder.Ended()
}

func TestBeforeAfterNextCreatesServerSpan(t *testing.T) {
	recorder := newRecordingTracer(t)

	spans := runRequest(t, recorder, fiber.MethodGet, "/user/42", func(app *fiber.App) {
		app.Get("/user/:id", func(c *fiber.Ctx) error {
			// Stand in for the trampoline around (*App).next.
			ictx := newMockHookContext(app, c)
			BeforeNext(ictx, app, c)
			err := c.SendString("ok")
			AfterNext(ictx, true, nil)
			return err
		})
	})

	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, "GET /user/:id", span.Name())
	assert.Equal(t, trace.SpanKindServer, span.SpanKind())
	assert.Equal(t, codes.Unset, span.Status().Code)

	attrs := attrsOf(t, span)
	assert.Equal(t, "GET", attrs["http.request.method"].AsString())
	assert.Equal(t, "/user/:id", attrs["http.route"].AsString())
	assert.Equal(t, "/user/42", attrs["url.path"].AsString())
	assert.Equal(t, int64(fiber.StatusOK), attrs["http.response.status_code"].AsInt64())
}

// The route is only reachable through (*Ctx).Next when middleware is involved,
// which is the case the previous hook point could not see at all.
func TestMiddlewareProducesExactlyOneSpan(t *testing.T) {
	recorder := newRecordingTracer(t)

	spans := runRequest(t, recorder, fiber.MethodGet, "/user/42", func(app *fiber.App) {
		app.Use(func(c *fiber.Ctx) error {
			ictx := newMockHookContext(app, c)
			BeforeNext(ictx, app, c)
			err := c.Next()
			AfterNext(ictx, true, nil)
			return err
		})
		app.Get("/user/:id", func(c *fiber.Ctx) error {
			// The re-entrant frame (*Ctx).Next produces.
			ictx := newMockHookContext(app, c)
			BeforeNext(ictx, app, c)
			err := c.SendString("ok")
			AfterNext(ictx, true, nil)
			return err
		})
	})

	require.Len(t, spans, 1, "the outermost router frame owns the span")
	assert.Equal(t, "GET /user/:id", spans[0].Name())
}

func TestHandlerErrorIsRecordedAsFiveHundred(t *testing.T) {
	recorder := newRecordingTracer(t)
	handlerErr := errors.New("boom")

	spans := runRequest(t, recorder, fiber.MethodGet, "/fail", func(app *fiber.App) {
		app.Get("/fail", func(c *fiber.Ctx) error {
			ictx := newMockHookContext(app, c)
			BeforeNext(ictx, app, c)
			AfterNext(ictx, true, handlerErr)
			return handlerErr
		})
	})

	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "boom", span.Status().Description)
	// The response still reads 200 at this point: Fiber runs its error handler
	// only after (*App).next returns, so the status has to come from the error.
	assert.Equal(t, int64(fiber.StatusInternalServerError),
		attrsOf(t, span)["http.response.status_code"].AsInt64())
	require.Len(t, span.Events(), 1, "the error must be recorded on the span")
}

func TestFiberErrorKeepsItsOwnStatus(t *testing.T) {
	recorder := newRecordingTracer(t)
	notFound := fiber.NewError(fiber.StatusNotFound, "nope")

	spans := runRequest(t, recorder, fiber.MethodGet, "/missing", func(app *fiber.App) {
		app.Get("/missing", func(c *fiber.Ctx) error {
			ictx := newMockHookContext(app, c)
			BeforeNext(ictx, app, c)
			AfterNext(ictx, false, notFound)
			return notFound
		})
	})

	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, int64(fiber.StatusNotFound),
		attrsOf(t, span)["http.response.status_code"].AsInt64())
	// 4xx is the client's fault, so a server span stays Unset.
	assert.Equal(t, codes.Unset, span.Status().Code)
}

func TestDisabledInstrumentationCreatesNoSpan(t *testing.T) {
	recorder := newRecordingTracer(t)
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", instrumentationKey)

	spans := runRequest(t, recorder, fiber.MethodGet, "/user/42", func(app *fiber.App) {
		app.Get("/user/:id", func(c *fiber.Ctx) error {
			ictx := newMockHookContext(app, c)
			BeforeNext(ictx, app, c)
			err := c.SendString("ok")
			AfterNext(ictx, true, nil)
			return err
		})
	})

	assert.Empty(t, spans)
}

func TestSpanIsVisibleToTheHandler(t *testing.T) {
	recorder := newRecordingTracer(t)
	var seen trace.SpanContext

	spans := runRequest(t, recorder, fiber.MethodGet, "/user/42", func(app *fiber.App) {
		app.Get("/user/:id", func(c *fiber.Ctx) error {
			ictx := newMockHookContext(app, c)
			BeforeNext(ictx, app, c)
			// Anything the handler calls must nest under the server span.
			seen = trace.SpanContextFromContext(c.UserContext())
			err := c.SendString("ok")
			AfterNext(ictx, true, nil)
			return err
		})
	})

	require.Len(t, spans, 1)
	assert.True(t, seen.IsValid(), "handler must see a recording span on the user context")
	assert.Equal(t, spans[0].SpanContext().SpanID(), seen.SpanID())
}

func TestIncomingTraceContextIsContinued(t *testing.T) {
	recorder := newRecordingTracer(t)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	const (
		traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
		spanID  = "00f067aa0ba902b7"
	)

	app := fiber.New()
	app.Get("/user/:id", func(c *fiber.Ctx) error {
		ictx := newMockHookContext(app, c)
		BeforeNext(ictx, app, c)
		err := c.SendString("ok")
		AfterNext(ictx, true, nil)
		return err
	})

	req := httptest.NewRequest(fiber.MethodGet, "/user/42", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-"+spanID+"-01")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, traceID, spans[0].SpanContext().TraceID().String(),
		"the span must join the caller's trace")
	assert.Equal(t, spanID, spans[0].Parent().SpanID().String())
}

func TestHeaderCarrierReadsRequestHeaders(t *testing.T) {
	app := fiber.New()

	var keys []string
	var got string
	app.Get("/", func(c *fiber.Ctx) error {
		carrier := headerCarrier{c: c}
		got = carrier.Get("X-Probe")
		keys = carrier.Keys()
		carrier.Set("X-Set", "written")
		assert.Equal(t, "written", c.Get("X-Set"))
		return c.SendString("ok")
	})

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	req.Header.Set("X-Probe", "value")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, "value", got)
	assert.Contains(t, keys, "X-Probe")
}
