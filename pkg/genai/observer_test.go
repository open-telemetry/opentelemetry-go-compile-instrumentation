package genai

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// A simple mock span to capture set attributes for testing.
type mockSpan struct {
	trace.Span
	attrs []attribute.KeyValue
}

func (s *mockSpan) SetAttributes(kv ...attribute.KeyValue) {
	s.attrs = append(s.attrs, kv...)
}
func (s *mockSpan) End(options ...trace.SpanEndOption) {}

// mockTracer returns our mockSpan
type mockTracer struct {
	trace.Tracer
	span *mockSpan
}

func (t *mockTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	t.span = &mockSpan{Span: noop.Span{}}
	// Just capture the attributes passed at start time
	cfg := trace.NewSpanStartConfig(opts...)
	t.span.attrs = append(t.span.attrs, cfg.Attributes()...)
	return ctx, t.span
}

func TestObserver_Accumulation(t *testing.T) {
	tracer := &mockTracer{Tracer: noop.Tracer{}}
	opts := ObserverOptions{CaptureContent: true}
	obs := NewObserver(tracer, opts)

	req := Request{
		Model:    "gpt-4",
		IsStream: true,
	}

	_, span := obs.Start(context.Background(), req)

	// Simulate streaming chunks
	tokens := []int64{10, 20}
	obs.ObserveChunk(Response{
		ChunkContent: "Hello, ",
		OutputTokens: &tokens[0],
	})

	// Sleep tiny bit to ensure TTFT > 0
	time.Sleep(10 * time.Millisecond)

	obs.ObserveChunk(Response{
		ChunkContent: "World!",
		OutputTokens: &tokens[1],
		FinishReasons: []string{"stop"},
	})

	obs.Finalize(span, nil)

	mock, ok := span.(*mockSpan)
	if !ok {
		t.Fatalf("expected mockSpan")
	}

	foundTotal := false
	foundContent := false
	for _, attr := range mock.attrs {
		if attr.Key == UsageOutputTokensKey {
			if attr.Value.AsInt64() != 30 {
				t.Errorf("expected 30 output tokens, got %d", attr.Value.AsInt64())
			}
		}
		if attr.Key == UsageTotalTokensKey {
			foundTotal = true
			if attr.Value.AsInt64() != 30 {
				t.Errorf("expected 30 total tokens, got %d", attr.Value.AsInt64())
			}
		}
		if attr.Key == ContentCompletionKey {
			foundContent = true
			if attr.Value.AsString() != "Hello, World!" {
				t.Errorf("expected 'Hello, World!', got %q", attr.Value.AsString())
			}
		}
		if attr.Key == ResponseTimeToFirstToken {
			if attr.Value.AsFloat64() <= 0 {
				t.Errorf("expected TTFT > 0, got %f", attr.Value.AsFloat64())
			}
		}
	}

	if !foundTotal {
		t.Error("expected total tokens attribute to be derived and set")
	}
	if !foundContent {
		t.Error("expected content completion attribute to be set")
	}
}

func TestObserver_Truncation(t *testing.T) {
	tracer := &mockTracer{Tracer: noop.Tracer{}}
	opts := ObserverOptions{CaptureContent: true}
	obs := NewObserver(tracer, opts)

	req := Request{
		Model:    "gpt-4",
		IsStream: true,
	}

	_, span := obs.Start(context.Background(), req)

	// Build a chunk larger than maxContentLength
	largeChunk := strings.Repeat("A", maxContentLength+100)
	obs.ObserveChunk(Response{
		ChunkContent: largeChunk,
	})

	obs.Finalize(span, nil)

	mock := span.(*mockSpan)
	for _, attr := range mock.attrs {
		if attr.Key == ContentCompletionKey {
			if len(attr.Value.AsString()) != maxContentLength {
				t.Errorf("expected content to be exactly %d bytes, got %d", maxContentLength, len(attr.Value.AsString()))
			}
		}
	}
}
