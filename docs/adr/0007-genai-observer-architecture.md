# 7. GenAI Observer Architecture

Date: 2026-08-24

## Status

PROPOSED

## Context

Instrumenting AI SDKs (OpenAI, Anthropic, Gemini) requires parsing Server-Sent Events (SSE) to aggregate telemetry like token counts, finish reasons, and dynamic model identifiers. Additionally, capturing streaming message content (`gen_ai.content.completion`) introduces a severe memory overhead risk if unbounded string deltas are accumulated naively in memory buffers during long-lived generation sessions.

Currently, this requires complex state machines inside the HTTP middleware of every SDK (e.g., `openai-go/streaming.go` is ~260 lines of custom `io.TeeReader` buffer management and content concatenation limits). As we expand to support Gemini, LangChain, and Model Context Protocol (MCP) hooks, duplicating this buffer management across packages leads to:
1. Memory leaks and OOM risks under heavy streaming load.
2. Inconsistent truncation boundaries and malformed UTF-8 string encoding.
3. Duplication of opt-in privacy constraints (`OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT`).
4. Semantic convention drift relative to the OpenTelemetry `gen_ai.*` specification.

## Decision

Introduce a completely **stateless `genai.Observer` adapter**. By centralizing the content capture and span lifecycle management into a stateless adapter, we guarantee that memory bounds and opt-in privacy constraints are enforced universally across Gemini, LangChain, and MCP hooks without duplicating complex state machines.

```text
                                  Client Application
                                          │
                    ┌─────────────────────┴─────────────────────┐
                    ▼                                           ▼
      HTTP SDKs (OpenAI/Anthropic/Gemini)             Non-HTTP Hooks (LangChain / MCP)
                    │                                           │
       Injected RoundTrip Middleware                  Compile-Time HookContext Injection
                    │                                           │
                    ▼                                           ▼
          Observer.WrapStream()                       Observer.ObserveHook()
  ┌───────────────────────────────┐               ┌───────────────────────────────┐
  │ • SSE Event Frame Splitting   │               │ • Span Lifecycle & Attributes │
  │ • ChunkExtractor Invocation   │──────────────▶│ • gen_ai.* Semantic Mapping   │
  │ • TTFT Measurement            │               │ • Token Aggregation           │
  │ • UTF-8 Safe Bounded Truncate │               │ • Opt-in Privacy Enforcement  │
  └───────────────────────────────┘               └───────────────────────────────┘
                    │                                           │
                    └─────────────────────┬─────────────────────┘
                                          ▼
                             OpenTelemetry Trace Spans
```

### 1. The Core Abstraction Contracts

The `Observer` itself maintains no per-request state, relying on the returned `io.ReadCloser` (for HTTP) or the injected `HookContext` (for Agent/MCP protocols) to maintain execution boundaries.

```go
package genai

import (
	"context"
	"io"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type ExtractedData struct {
	ID                       string
	Model                    string
	PromptTokens             int64
	CompletionTokens         int64
	TotalTokens              int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	FinishReasons            []string
	ContentDelta             string
	ProviderAttributes       []attribute.KeyValue
}

type ChunkExtractor func(rawFrame []byte) ExtractedData

type Observer struct {}

func (o *Observer) WrapStream(ctx context.Context, body io.ReadCloser, span trace.Span, ext ChunkExtractor) io.ReadCloser

func (o *Observer) ObserveHook(ctx context.Context, span trace.Span, data ExtractedData)
```

### 2. HTTP SDK Instrumentation (OpenAI, Anthropic, Gemini)

For SDKs operating over HTTP SSE transports:
* **Frame Splitting vs Event Decoding:** `Observer.WrapStream()` delimits stream chunks on standard SSE message boundaries (`\n\n`). This makes the transport reader protocol-agnostic: OpenAI single-line payloads (`data: {...}\n\n`) and Anthropic multi-line frames (`event: message_start\ndata: {...}\n\n`) are both delivered intact to the provider's `ChunkExtractor`.
* **State Machine Dispatch:** The `ChunkExtractor` decodes provider-specific fields (e.g. Anthropic's `event: message_start` input tokens vs `event: message_delta` output tokens) and normalizes them into `ExtractedData`.
* **Prompt Cache Normalization:** Cache read and creation metrics are mapped directly into `gen_ai.usage.cache_read.input_tokens` and `gen_ai.usage.cache_creation.input_tokens` without double-counting (OpenAI includes cached tokens in `prompt_tokens`, while Anthropic reports them separately).
* **Time-To-First-Token (TTFT):** Measured automatically upon the first non-empty content delta.
* **Bounded Content Capture:** Enforces opt-in privacy constraints (`OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT`) and strict 16 KiB / UTF-8 boundary truncation centrally, preventing memory accumulation leaks and malformed runes.

### 3. Non-HTTP Hooks (LangChain & MCP)

For agent orchestration frameworks and protocols that operate outside standard HTTP request/response pipelines:
* **Compile-time Bytecode Injection:** The `otelc` compiler injects hooks into LangChain chain executions and MCP tool payload handlers, capturing the precise `HookContext`.
* **Direct Observation:** Instead of wrapping an `io.ReadCloser`, the injected hooks invoke `Observer.ObserveHook()` directly. This standardizes the semantic output (applying `gen_ai.*` attributes and token aggregation) across all paradigms without fabricating HTTP layers or duplicating state machines.

## Migration Path

1. Implement `pkg/genai/observer.go` with full unit and memory-boundary test coverage.
2. Refactor `instrumentation/github.com/openai/openai-go` to use `Observer.WrapStream()`.
3. Migrate `instrumentation/google.golang.org/genai` (Gemini) onto the shared observer.
4. Wire non-HTTP `HookContext` handlers for LangChain and MCP into `Observer.ObserveHook()`.
5. Remove ad-hoc `streaming.go` implementations across individual provider packages.

## Backward Compatibility

This architecture operates entirely within the compile-time instrumentation layer. Application code requires zero modifications, and all generated spans remain 100% compliant with the OpenTelemetry `gen_ai.*` semantic conventions.

## Consequences

* **Positive:** Eliminates memory leak and OOM vectors across all supported AI providers.
* **Positive:** Universal enforcement of `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` and memory limits across HTTP (Gemini/OpenAI) and non-HTTP (LangChain/MCP) paradigms.
* **Positive:** Drastically reduces code footprint for new AI SDK integrations.
* **Trade-off:** Minimal per-chunk interface indirection, offset by elimination of duplicate buffer allocations in middleware.
