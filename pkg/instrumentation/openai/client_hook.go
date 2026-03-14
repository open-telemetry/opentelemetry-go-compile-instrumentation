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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/semconv/v1.39.0/genaiconv"
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
	logger       = shared.Logger()
	tracer       trace.Tracer
	durationHist genaiconv.ClientOperationDuration
	tokenUsage   genaiconv.ClientTokenUsage
	initOnce     sync.Once
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
		meter := otel.GetMeterProvider().Meter(
			instrumentationName,
			metric.WithInstrumentationVersion(version),
		)
		var err error
		durationHist, err = genaiconv.NewClientOperationDuration(meter)
		if err != nil {
			logger.Error("failed to create duration histogram", "error", err)
		}
		tokenUsage, err = genaiconv.NewClientTokenUsage(meter)
		if err != nil {
			logger.Error("failed to create token usage histogram", "error", err)
		}
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
		"span":      span,
		"ctx":       ctx,
		"start":     time.Now(),
		"operation": operation,
		"model":     model,
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

// recordDuration records the gen_ai.client.operation.duration metric.
func recordDuration(ctx context.Context, ictx inst.HookContext, err error) {
	start, ok := ictx.GetKeyData("start").(time.Time)
	if !ok {
		return
	}
	operation, _ := ictx.GetKeyData("operation").(string)
	model, _ := ictx.GetKeyData("model").(string)

	duration := float64(time.Since(start)) / float64(time.Second)
	attrs := []attribute.KeyValue{
		durationHist.AttrRequestModel(model),
	}
	if err != nil {
		attrs = append(attrs, durationHist.AttrErrorType(genaiconv.ErrorTypeOther))
	}
	durationHist.Record(ctx, duration,
		genaiconv.OperationNameAttr(operation),
		genaiconv.ProviderNameOpenAI,
		attrs...,
	)
}

// recordTokenUsage records the gen_ai.client.token.usage metric for input and output tokens.
func recordTokenUsage(ctx context.Context, operation, model string, inputTokens, outputTokens int64) {
	modelAttr := tokenUsage.AttrRequestModel(model)
	if inputTokens > 0 {
		tokenUsage.Record(ctx, inputTokens,
			genaiconv.OperationNameAttr(operation),
			genaiconv.ProviderNameOpenAI,
			genaiconv.TokenTypeInput,
			modelAttr,
		)
	}
	if outputTokens > 0 {
		tokenUsage.Record(ctx, outputTokens,
			genaiconv.OperationNameAttr(operation),
			genaiconv.ProviderNameOpenAI,
			genaiconv.TokenTypeOutput,
			modelAttr,
		)
	}
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

	ctx, _ := ictx.GetKeyData("ctx").(context.Context)
	if ctx == nil {
		ctx = context.Background()
	}
	recordDuration(ctx, ictx, err)

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

		operation, _ := ictx.GetKeyData("operation").(string)
		model, _ := ictx.GetKeyData("model").(string)
		recordTokenUsage(ctx, operation, model, res.Usage.PromptTokens, res.Usage.CompletionTokens)
	}
}



