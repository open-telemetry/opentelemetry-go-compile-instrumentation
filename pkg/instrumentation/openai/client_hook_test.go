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
)

// mockHookContext implements inst.HookContext for testing.
type mockHookContext struct {
	data    interface{}
	keyData map[string]interface{}
	params  map[int]interface{}
	returns map[int]interface{}
}

var _ inst.HookContext = (*mockHookContext)(nil)

func newMockHookContext() *mockHookContext {
	return &mockHookContext{
		keyData: make(map[string]interface{}),
		params:  make(map[int]interface{}),
		returns: make(map[int]interface{}),
	}
}

func (m *mockHookContext) SetSkipCall(bool)    {}
func (m *mockHookContext) IsSkipCall() bool     { return false }
func (m *mockHookContext) GetFuncName() string  { return "test" }
func (m *mockHookContext) GetPackageName() string { return "test" }
func (m *mockHookContext) SetData(d interface{}) {
	m.data = d
	if dm, ok := d.(map[string]interface{}); ok {
		for k, v := range dm {
			m.keyData[k] = v
		}
	}
}
func (m *mockHookContext) GetData() interface{}                  { return m.data }
func (m *mockHookContext) GetKeyData(key string) interface{}     { return m.keyData[key] }
func (m *mockHookContext) SetKeyData(key string, val interface{}) { m.keyData[key] = val }
func (m *mockHookContext) HasKeyData(key string) bool            { _, ok := m.keyData[key]; return ok }
func (m *mockHookContext) GetParamCount() int                    { return len(m.params) }
func (m *mockHookContext) GetParam(idx int) interface{}          { return m.params[idx] }
func (m *mockHookContext) SetParam(idx int, val interface{})     { m.params[idx] = val }
func (m *mockHookContext) GetReturnValCount() int                { return len(m.returns) }
func (m *mockHookContext) GetReturnVal(idx int) interface{}      { return m.returns[idx] }
func (m *mockHookContext) SetReturnVal(idx int, val interface{}) { m.returns[idx] = val }

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
	newCtx, ok := ictx.params[ctxParamIndex].(context.Context)
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



