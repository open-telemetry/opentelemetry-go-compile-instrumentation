package genai

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// Request defines the unified structure of an outgoing GenAI prompt.
// Instrumentations extract fields from the provider-specific payload and map them to this agnostic format.
type Request struct {
	SystemPrompt     string
	OperationName    string
	Model            string
	Provider         string
	MaxTokens        *int64
	Temperature      *float64
	TopP             *float64
	FrequencyPenalty *float64
	PresencePenalty  *float64
	IsStream         bool
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

// ObserverOptions dictates how the Observer behaves (e.g., whether to buffer content).
type ObserverOptions struct {
	CaptureContent bool
}

// Observer manages the lifecycle of a GenAI span.
// It centralizes Semantic Convention mapping, usage metric aggregation, and payload buffering.
type Observer interface {
	// Start begins a GenAI span, mapping Request fields to semconv attributes.
	Start(ctx context.Context, req Request) (context.Context, trace.Span)

	// ObserveChunk is called sequentially as SSE chunks arrive, or once for a non-streaming response.
	// The Observer internally accumulates usage metrics and content.
	ObserveChunk(resp Response)

	// Finalize applies all accumulated attributes, calculates TimeToFirstToken (if streaming),
	// and ends the span.
	Finalize(span trace.Span, err error)
}
