# 7. GenAI Observer Architecture

Date: 2026-07-30

## Status

PROPOSED

## Context

Instrumenting AI SDKs (OpenAI, Anthropic) requires parsing Server-Sent Events (SSE) to aggregate telemetry like input/output token usage and finish reasons. Additionally, capturing streaming message content (`gen_ai.content.completion`) poses a high OOM risk if unbounded string deltas are accumulated in memory.

Currently, this requires complex state machines inside the HTTP middleware of every SDK (e.g., `openai-go/streaming.go` is ~260 lines of custom `io.TeeReader` buffer management and content concatenation limits). As we expand to support Gemini and Agent frameworks (LangChain), duplicating this buffer management and span lifecycle across every package will lead to memory leaks, inconsistent truncation limits, and semantic convention drift.

## Decision

Introduce `pkg/genai.Observer` to centralize telemetry mapping, span lifecycle, and memory bounding. 

### 1. The `StreamAdapter` for HTTP Middleware
For SDKs operating at the HTTP layer, the Observer provides a `StreamAdapter` that takes ownership of the `*http.Response.Body`. It safely buffers SSE chunks, enforces a hard global memory limit (`maxResponseBodySize`) for content concatenation (preventing OOMs), calculates Time-To-First-Token (TTFT), and finalizes the OpenTelemetry span.

### 2. SDKs as JSON Extractors
HTTP SDK instrumentations are reduced to lightweight JSON mappers. They provide a callback to the `StreamAdapter`:

```go
type ExtractedData struct {
    ID                 string
    Model              string
    PromptTokens       int64
    CompletionTokens   int64
    TotalTokens        int64
    FinishReasons      []string
    ContentDelta       string
    ProviderAttributes []attribute.KeyValue
}

type ChunkExtractor func(rawEvent []byte) ExtractedData
```

The adapter splits the SSE stream by event boundaries (`\n\n`) and invokes `ChunkExtractor`. The SDK parses the event block and returns `ExtractedData`.
*   **Protocol Framing:** The central adapter MUST NOT strip `data: ` prefixes natively. OpenAI uses `data: [DONE]`, but Anthropic uses `event: message_stop`. The `ChunkExtractor` is responsible for handling its provider's proprietary framing and JSON schema.
*   **Semantic Integrity:** The `ID` and `Model` fields ensure `gen_ai.response.id` and `gen_ai.response.model` conventions are met dynamically as chunks arrive.
*   **Extensibility:** The `ProviderAttributes` array allows Anthropic to pass through unique metrics (e.g., `gen_ai.anthropic.usage.cache_read_input_tokens`) without polluting the generic struct.
*   **OOM Protection:** The `ContentDelta` is strictly bound and concatenated by the central adapter, ensuring safe `gen_ai.content.completion` emission.

### 3. Agent Frameworks (LangChain)
For non-HTTP frameworks like LangChain, the SDK instrumentation will bypass the `StreamAdapter` (as there is no SSE or `io.TeeReader`) and map their Go structs directly into the base `genai.Observer`, standardizing the semantic output across all paradigms.

### 4. Out of Scope: MCP
Model Context Protocol (MCP) relies on multiplexed JSON-RPC (often over WebSockets) where multiple requests share a stream and backends can fan-out. This requires tracking JSON-RPC IDs and child spans. `genai.Observer` is explicitly scoped to linear Request/Response lifecycles. MCP instrumentation must use a distinct `mcp.Observer` pattern.

## Migration Path

1. Merge `pkg/genai.Observer` and `genai.StreamAdapter`.
2. Refactor `openai-go` middleware to wrap responses with `genai.StreamAdapter`.
3. Pass `parseChatResponse` and `parseCompletionResponse` to the adapter as `ChunkExtractor` callbacks.
4. Delete `instrumentation/github.com/openai/openai-go/streaming.go`.

## Backward Compatibility

This refactor is entirely internal to the HTTP middleware. It requires no API changes for `otelc` users. Emitted telemetry will remain exactly compliant with the current `gen_ai.*` semantic conventions.

## Alternatives Considered

1. **Status Quo (Duplicating State Machines):** Write a new `streaming.go` for Gemini. Rejected. Duplicating `io.TeeReader` logic multiplies the surface area for memory leaks and OOM vulnerabilities when capturing content.
2. **Stateless Helper Functions:** Export stateless parsing helpers. Rejected. Aggregating usage tokens, calculating TTFT, and concatenating string deltas requires state persistence across the HTTP request lifecycle, mandating a stateful observer.

## Consequences

* **Positive:** Eliminates OOM vectors across all SDKs by centralizing content capture limits.
* **Positive:** Drastically reduces code footprint for new SDKs (Gemini integration becomes a trivial JSON unmarshaling callback).
* **Positive:** Extensible to Agent Frameworks without HTTP middleware dependencies.
* **Negative:** Minor allocation overhead introduced by the callback interface and `ExtractedData` mapping structs.
