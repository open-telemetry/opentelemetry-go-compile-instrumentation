// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package openai

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/openai/semconv"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/openai"
	instrumentationKey  = "OPENAI"
	ctxParamIndex       = 1 // ctx is param index 1 (index 0 is the receiver for methods)
)

var (
	logger   = shared.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

// openaiClientEnabler controls whether OpenAI instrumentation is enabled.
type openaiClientEnabler struct{}

func (o openaiClientEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var clientEnabler = openaiClientEnabler{}

func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func initInstrumentation() {
	initOnce.Do(func() {
		version := moduleVersion()
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/openai", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}
		logger.Info("OpenAI client instrumentation initialized")
	})
}

// startSpan is a shared helper that starts a GenAI span, updates the context
// parameter, and stores the span in the hook context for the after hook.
func startSpan(ictx inst.HookContext, ctx context.Context, operation, model string) {
	initInstrumentation()

	attrs := semconv.RequestTraceAttrs(operation, model)
	spanName := operation + " " + model
	ctx, span := tracer.Start(ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)

	ictx.SetParam(ctxParamIndex, ctx)
	ictx.SetData(map[string]interface{}{
		"span":  span,
		"start": time.Now(),
	})
}

// endSpanWithError is a shared helper that ends a span, optionally recording an error.
func endSpanWithError(ictx inst.HookContext, err error) trace.Span {
	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		return nil
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return span
}

// ---------------------------------------------------------------------------
// Chat Completion hooks
// ---------------------------------------------------------------------------

func beforeChatCompletionNew(
	ictx inst.HookContext,
	_ *openaisdk.ChatCompletionService,
	ctx context.Context,
	body openaisdk.ChatCompletionNewParams,
	opts ...option.RequestOption,
) {
	if !clientEnabler.Enable() {
		return
	}
	startSpan(ictx, ctx, semconv.OperationChat, string(body.Model))
}

func afterChatCompletionNew(ictx inst.HookContext, res *openaisdk.ChatCompletion, err error) {
	if !clientEnabler.Enable() {
		return
	}
	span := endSpanWithError(ictx, err)
	if span == nil {
		return
	}
	defer span.End()

	if res != nil {
		finishReasons := make([]string, 0, len(res.Choices))
		for _, choice := range res.Choices {
			finishReasons = append(finishReasons, string(choice.FinishReason))
		}
		attrs := semconv.ChatCompletionResponseTraceAttrs(
			res.ID,
			res.Model,
			finishReasons,
			res.Usage.PromptTokens,
			res.Usage.CompletionTokens,
		)
		span.SetAttributes(attrs...)
	}
}

// ---------------------------------------------------------------------------
// Chat Completion Streaming hooks
// ---------------------------------------------------------------------------

func beforeChatCompletionNewStreaming(
	ictx inst.HookContext,
	_ *openaisdk.ChatCompletionService,
	ctx context.Context,
	body openaisdk.ChatCompletionNewParams,
	opts ...option.RequestOption,
) {
	if !clientEnabler.Enable() {
		return
	}
	startSpan(ictx, ctx, semconv.OperationChat, string(body.Model))
}

// afterChatCompletionNewStreaming ends the span once the streaming connection
// is established. Token usage and finish reasons are not available until the
// stream is fully consumed, so only the connection latency is captured.
func afterChatCompletionNewStreaming(ictx inst.HookContext, stream *ssestream.Stream[openaisdk.ChatCompletionChunk]) {
	if !clientEnabler.Enable() {
		return
	}
	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		return
	}
	defer span.End()

	if stream == nil {
		span.SetStatus(codes.Error, "nil stream returned")
	}
}

// ---------------------------------------------------------------------------
// Embedding hooks
// ---------------------------------------------------------------------------

func beforeEmbeddingNew(
	ictx inst.HookContext,
	_ *openaisdk.EmbeddingService,
	ctx context.Context,
	body openaisdk.EmbeddingNewParams,
	opts ...option.RequestOption,
) {
	if !clientEnabler.Enable() {
		return
	}
	startSpan(ictx, ctx, semconv.OperationEmbeddings, string(body.Model))
}

func afterEmbeddingNew(ictx inst.HookContext, res *openaisdk.CreateEmbeddingResponse, err error) {
	if !clientEnabler.Enable() {
		return
	}
	span := endSpanWithError(ictx, err)
	if span == nil {
		return
	}
	defer span.End()

	if res != nil {
		attrs := semconv.EmbeddingResponseTraceAttrs(
			res.Model,
			res.Usage.PromptTokens,
		)
		span.SetAttributes(attrs...)
	}
}

// ---------------------------------------------------------------------------
// Legacy Completion hooks
// ---------------------------------------------------------------------------

func beforeCompletionNew(
	ictx inst.HookContext,
	_ *openaisdk.CompletionService,
	ctx context.Context,
	body openaisdk.CompletionNewParams,
	opts ...option.RequestOption,
) {
	if !clientEnabler.Enable() {
		return
	}
	startSpan(ictx, ctx, semconv.OperationTextCompletion, string(body.Model))
}

func afterCompletionNew(ictx inst.HookContext, res *openaisdk.Completion, err error) {
	if !clientEnabler.Enable() {
		return
	}
	span := endSpanWithError(ictx, err)
	if span == nil {
		return
	}
	defer span.End()

	if res != nil {
		finishReasons := make([]string, 0, len(res.Choices))
		for _, choice := range res.Choices {
			finishReasons = append(finishReasons, string(choice.FinishReason))
		}
		attrs := semconv.CompletionResponseTraceAttrs(
			res.ID,
			res.Model,
			finishReasons,
			res.Usage.PromptTokens,
			res.Usage.CompletionTokens,
		)
		span.SetAttributes(attrs...)
	}
}

// ---------------------------------------------------------------------------
// Legacy Completion Streaming hooks
// ---------------------------------------------------------------------------

func beforeCompletionNewStreaming(
	ictx inst.HookContext,
	_ *openaisdk.CompletionService,
	ctx context.Context,
	body openaisdk.CompletionNewParams,
	opts ...option.RequestOption,
) {
	if !clientEnabler.Enable() {
		return
	}
	startSpan(ictx, ctx, semconv.OperationTextCompletion, string(body.Model))
}

// afterCompletionNewStreaming ends the span once the streaming connection is
// established. Token usage is not available until the stream is fully consumed.
func afterCompletionNewStreaming(ictx inst.HookContext, stream *ssestream.Stream[openaisdk.Completion]) {
	if !clientEnabler.Enable() {
		return
	}
	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		return
	}
	defer span.End()

	if stream == nil {
		span.SetStatus(codes.Error, "nil stream returned")
	}
}
