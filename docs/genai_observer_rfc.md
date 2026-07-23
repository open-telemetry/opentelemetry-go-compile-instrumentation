# RFC: `genai.Observer` — A Unified Adapter for GenAI Telemetry

## Background
Currently, AI SDK instrumentations (e.g., `openai-go`, `anthropic-sdk-go`) in `otelc` manually parse JSON, handle chunked Server-Sent Events (SSE), and independently map these idiosyncratic payloads to OpenTelemetry `gen_ai.*` semantic conventions. 

As we expand support to `gemini` and `langchain` (per LFX Term 3), duplicating this logic inside each SDK's `middleware.go` and `streaming.go` creates an unsustainable maintenance burden, especially for cross-cutting capabilities like `gen_ai.content.completion` capture, which require strict memory bounds and robust stream-accumulation logic.

## Proposal: The `genai.Observer`
We propose a centralized `genai.Observer` component in `pkg/genai`. SDK-specific instrumentations will act as lightweight **Extractors**, while the **Observer** handles all span management, Semantic Convention mapping, token usage aggregation, and content accumulation.

### 1. The Core Interface
The Observer exposes an interface that accepts normalized, agnostic events from the SDK extractors.

```go
package genai

import (
	"context"
	"go.opentelemetry.io/otel/trace"
)

// Request defines the unified structure of an outgoing GenAI prompt.
type Request struct {
	SystemPrompt      string
	OperationName     string
	Model             string
	Provider          string
	MaxTokens         *int64
	Temperature       *float64
	TopP              *float64
	FrequencyPenalty  *float64
	PresencePenalty   *float64
	IsStream          bool
	Messages          []string // For content capture
}

// Response defines the unified structure of a GenAI response or partial chunk.
type Response struct {
	ID            string
	Model         string
	FinishReasons []string
	InputTokens   *int64
	OutputTokens  *int64
	TotalTokens   *int64
	ChunkContent  string // Accumulated by the Observer if capture is enabled
}

// Observer manages the lifecycle of a GenAI span.
type Observer interface {
	// Start begins a GenAI span, mapping Request fields to semconv attributes.
	Start(ctx context.Context, req Request) (context.Context, trace.Span)
	
	// ObserveChunk is called sequentially as SSE chunks arrive.
	// The Observer internally accumulates usage metrics and content.
	ObserveChunk(resp Response)
	
	// Finalize applies all accumulated attributes, calculates TimeToFirstToken,
	// and ends the span.
	Finalize(span trace.Span, err error)
}
```

### 2. Implementation in SDKs
Instead of parsing and mapping SemConv natively, the `openai-go` middleware simply extracts fields and feeds the Observer:

```go
// Inside openai-go/middleware.go
observer := genai.NewObserver(tracer, genai.ObserverOptions{
    CaptureContent: true, // Tied to env var
})

// 1. Extract and Start
req := genai.Request{
    Model:       extractModel(bodyBytes),
    IsStream:    isStreaming,
    // ...
}
ctx, span := observer.Start(req.Context(), req)

// 2. Stream Handling
resp.Body = newStreamingReader(resp.Body, func(chunk openai.Chunk) {
    observer.ObserveChunk(genai.Response{
        FinishReasons: chunk.Choices[0].FinishReason,
        ChunkContent:  chunk.Choices[0].Delta.Content,
    })
})

// 3. Finalize on Close or Error
observer.Finalize(span, err)
```

## Benefits for LFX Term 3
1. **Zero-Overhead Content Capture:** The complex `strings.Builder` and OOM-truncation logic is moved *entirely* into the `Observer`. SDKs no longer care about it.
2. **Rapid Expansion:** Instrumenting Gemini or LangChain becomes trivial. We only need to write a simple JSON extractor that maps to `genai.Request/Response`.
3. **DRY SemConv:** Standardized semantic convention mapping happens in one place, guaranteeing spec-compliance across all AI SDKs.
