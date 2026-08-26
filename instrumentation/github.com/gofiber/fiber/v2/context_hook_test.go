// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"go.opentelemetry.io/otelc/pkg/hook"
)

type mockHookContext struct {
	hook.HookContext
	params   []any
	keyData  map[string]any
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

func (m *mockHookContext) SetKeyData(key string, val any) {
	m.keyData[key] = val
}

func (m *mockHookContext) GetKeyData(key string) any {
	return m.keyData[key]
}

func TestFiberBeforeAndAfterNext(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	app := fiber.New()

	app.Get("/user/:id", func(c *fiber.Ctx) error {
		ictx := newMockHookContext(c)

		// Simulate request context with OTel span
		ctx, span := tracer.Start(context.Background(), "GET")
		defer span.End()

		c.SetUserContext(ctx)

		BeforeNext(ictx, c)
		AfterNext(ictx)

		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/user/123", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET /user/:id", spans[0].Name)

	hasRouteAttr := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == semconv.HTTPRouteKey {
			assert.Equal(t, "/user/:id", attr.Value.AsString())
			hasRouteAttr = true
		}
	}
	assert.True(t, hasRouteAttr)
}

func TestFiberAfterNext_ErrorStatus(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	app := fiber.New()

	app.Get("/error", func(c *fiber.Ctx) error {
		ictx := newMockHookContext(c)

		ctx, span := tracer.Start(context.Background(), "GET")
		defer span.End()

		c.SetUserContext(ctx)

		BeforeNext(ictx, c)
		c.Status(500)
		AfterNext(ictx)

		return c.SendStatus(500)
	})

	req := httptest.NewRequest("GET", "/error", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code)
}
