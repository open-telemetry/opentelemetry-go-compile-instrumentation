# 8. GenAI Observer Architecture

Date: 2026-08-24 (Updated: 2026-09-27)

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

type TokenUsage struct {
	PromptTokens             *int64
	CompletionTokens         *int64
	TotalTokens              *int64
	CacheReadInputTokens     *int64
	CacheCreationInputTokens *int64
}

type ExtractedData struct {
	ID                 string
	Model              string
	Usage              TokenUsage
	FinishReasons      []string
	ContentDelta       string
	ProviderAttributes []attribute.KeyValue
}

type ChunkExtractor func(rawFrame []byte) ExtractedData

type Observer struct {}

func (o *Observer) WrapStream(ctx context.Context, body io.ReadCloser, span trace.Span, ext ChunkExtractor) io.ReadCloser

func (o *Observer) ObserveHook(ctx context.Context, span trace.Span, data ExtractedData)
```

### 2. HTTP SDK Instrumentation (OpenAI, Anthropic, Gemini)

For SDKs operating over HTTP SSE transports:

* **Frame Splitting vs Event Decoding:** 
  `Observer.WrapStream()` delimits stream chunks on standard SSE message boundaries per the W3C SSE specification (`\r\n\r\n`, `\n\n`, or `\r\r`). 
  The internal stream scanner maintains a sliding window across multiple `io.Reader.Read` calls so that delimiter sequences split across read buffers or packet boundaries (e.g. `\r` at the end of read *N*, followed by `\n\r\n` at the start of read *N+1*) are correctly reconstituted without corrupting frames.
  This makes the transport reader protocol-agnostic: OpenAI single-line payloads (`data: {...}\n\n`) and Anthropic multi-line frames (`event: message_start\ndata: {...}\n\n`) are both delivered intact to the provider's `ChunkExtractor`.

* **Token Counting and Prompt Cache Normalization:**
  - `TokenUsage` uses pointer fields (`*int64`) so the observer unambiguously distinguishes between an omitted/missing metric (`nil`) and an explicit count of zero (`0`).
  - Providers delivering cumulative totals at stream completion (e.g., OpenAI's final chunk when `stream_options.include_usage = true`) update the span's final usage attributes directly.
  - Providers emitting per-event deltas (e.g., Anthropic's `message_delta.usage.output_tokens`) are accumulated incrementally into running totals across the stream lifecycle.
  - Prompt cache tokens (`CacheReadInputTokens`, `CacheCreationInputTokens`) are mapped directly to `gen_ai.usage.cache_read.input_tokens` and `gen_ai.usage.cache_creation.input_tokens`. Providers bundling cached tokens into total prompt tokens (e.g., OpenAI `prompt_tokens_details.cached_tokens`) are normalized so that semantic attribute reporting adheres strictly to the OpenTelemetry semantic conventions without double-counting.

* **Parser Buffer Boundary & Graceful Degradation:**
  Buffering required for SSE frame reconstruction is strictly separated from message content capture:
  - **Parser Framing Buffer:** Capped at 64 KiB. If an upstream stream emits a pathological or un-delimited frame that exceeds 64 KiB, the observer drops observation for that frame, emits a warning trace event (`gen_ai.observation_degraded`), and falls back to unbuffered byte passthrough. Under no circumstances are application read bytes or errors modified or dropped.
  - **Message Content Capture:** Bounded by default to 16 KiB (governed by opt-in `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT`). Truncation is rune-boundary aware (using `utf8.DecodeLastRune`) to eliminate malformed UTF-8 replacement characters (`\uFFFD`) at chunk cutoffs.

* **Time-To-First-Token (TTFT):** Measured automatically upon the first non-empty content delta or completion chunk.

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
