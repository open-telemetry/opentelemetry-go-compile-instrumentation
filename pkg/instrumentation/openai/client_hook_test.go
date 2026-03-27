// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package openai

import (
	"context"
	"sync"
	"testing"

	openaisdk "github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst/insttest"
)

// mockHookContext is a type alias for the shared MockHookContext from insttest package.
type mockHookContext = insttest.MockHookContext

var _ inst.HookContext = (*mockHookContext)(nil)

// newMockHookContext creates a new MockHookContext using the shared implementation.
func newMockHookContext() *mockHookContext {
	return insttest.NewMockHookContext()
}

func setupTestTracer() (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	return sr, tp
}

func resetInit() {
	initOnce = *new(sync.Once)
}

// ---------------------------------------------------------------------------
// Chat Completion Tests
// ---------------------------------------------------------------------------

func TestBeforeAfterChatCompletionNew(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")

	sr, tp := setupTestTracer()
	defer tp.Shutdown(context.Background())

	ictx := newMockHookContext()
	ctx := context.Background()
	params := openaisdk.ChatCompletionNewParams{
		Model: "gpt-4",
	}

	beforeChatCompletionNew(ictx, &openaisdk.ChatCompletionService{}, ctx, params)

	// Verify context was updated
	newCtx, ok := ictx.GetParam(ctxParamIndex).(context.Context)
	require.True(t, ok, "context should be set via SetParam")
	require.NotEqual(t, ctx, newCtx, "context should be different (has span)")

	// Simulate response
	res := &openaisdk.ChatCompletion{
		ID:    "chatcmpl-test123",
		Model: "gpt-4-0613",
		Choices: []openaisdk.ChatCompletionChoice{
			{FinishReason: "stop"},
		},
		Usage: openaisdk.CompletionUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
		},
	}
	afterChatCompletionNew(ictx, res, nil)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	span := spans[0]

	assert.Equal(t, "chat gpt-4", span.Name())
	assert.Equal(t, codes.Unset, span.Status().Code)

	attrs := make(map[string]interface{})
	for _, attr := range span.Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsInterface()
	}

	assert.Equal(t, "openai", attrs["gen_ai.system"])
	assert.Equal(t, "chat", attrs["gen_ai.operation.name"])
	assert.Equal(t, "gpt-4", attrs["gen_ai.request.model"])
	assert.Equal(t, "chatcmpl-test123", attrs["gen_ai.response.id"])
	assert.Equal(t, "gpt-4-0613", attrs["gen_ai.response.model"])
	assert.Equal(t, int64(10), attrs["gen_ai.usage.input_tokens"])
	assert.Equal(t, int64(20), attrs["gen_ai.usage.output_tokens"])
}

func TestChatCompletionNew_Error(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_ENABLED_INSTRUMENTATIONS", "OPENAI")

	sr, tp := setupTestTracer()
	defer tp.Shutdown(context.Background())

	ictx := newMockHookContext()
	ctx := context.Background()
	params := openaisdk.ChatCompletionNewParams{
		Model: "gpt-4",
	}

	beforeChatCompletionNew(ictx, &openaisdk.ChatCompletionService{}, ctx, params)

	testErr := assert.AnError
	afterChatCompletionNew(ictx, nil, testErr)

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestChatCompletionNew_Disabled(t *testing.T) {
	resetInit()
	t.Setenv("OTEL_GO_DISABLED_INSTRUMENTATIONS", "OPENAI")

	sr, tp := setupTestTracer()
	defer tp.Shutdown(context.Background())

	ictx := newMockHookContext()
	ctx := context.Background()
	params := openaisdk.ChatCompletionNewParams{
		Model: "gpt-4",
	}

	beforeChatCompletionNew(ictx, &openaisdk.ChatCompletionService{}, ctx, params)
	afterChatCompletionNew(ictx, nil, nil)

	assert.Len(t, sr.Ended(), 0, "no spans when disabled")
}
