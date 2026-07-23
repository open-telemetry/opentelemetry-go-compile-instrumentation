package genai

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Semantic Convention Constants
const (
	SystemKey                = attribute.Key("gen_ai.system")
	ProviderNameKey          = attribute.Key("gen_ai.provider.name")
	OperationNameKey         = attribute.Key("gen_ai.operation.name")
	RequestModelKey          = attribute.Key("gen_ai.request.model")
	RequestMaxTokensKey      = attribute.Key("gen_ai.request.max_tokens")
	RequestTemperatureKey    = attribute.Key("gen_ai.request.temperature")
	RequestTopPKey           = attribute.Key("gen_ai.request.top_p")
	RequestFrequencyPenaltyKey = attribute.Key("gen_ai.request.frequency_penalty")
	RequestPresencePenaltyKey  = attribute.Key("gen_ai.request.presence_penalty")
	RequestIsStreamKey       = attribute.Key("gen_ai.request.is_stream")

	ResponseModelKey         = attribute.Key("gen_ai.response.model")
	ResponseIDKey            = attribute.Key("gen_ai.response.id")
	ResponseFinishReasonsKey = attribute.Key("gen_ai.response.finish_reasons")
	ResponseTimeToFirstToken = attribute.Key("gen_ai.response.time_to_first_token")

	UsageInputTokensKey      = attribute.Key("gen_ai.usage.input_tokens")
	UsageOutputTokensKey     = attribute.Key("gen_ai.usage.output_tokens")
	UsageTotalTokensKey      = attribute.Key("gen_ai.usage.total_tokens")

	ContentCompletionKey     = attribute.Key("gen_ai.content.completion")
)

const maxContentLength = 128 * 1024 // 128KB max buffer to prevent OOM

type defaultObserver struct {
	tracer  trace.Tracer
	options ObserverOptions

	mu             sync.Mutex
	startTime      time.Time
	firstTokenTime time.Time
	isStreaming    bool

	// Accumulators
	inputTokens   int64
	outputTokens  int64
	totalTokens   int64
	finishReasons []string
	contentBuf    strings.Builder
	responseModel string
	responseID    string
}

// NewObserver creates a new GenAI observer.
func NewObserver(tracer trace.Tracer, opts ObserverOptions) Observer {
	return &defaultObserver{
		tracer:  tracer,
		options: opts,
	}
}

func (o *defaultObserver) Start(ctx context.Context, req Request) (context.Context, trace.Span) {
	o.startTime = time.Now()
	o.isStreaming = req.IsStream

	var attrs []attribute.KeyValue

	if req.SystemPrompt != "" {
		attrs = append(attrs, SystemKey.String(req.SystemPrompt))
	}
	if req.OperationName != "" {
		attrs = append(attrs, OperationNameKey.String(req.OperationName))
	}
	if req.Model != "" {
		attrs = append(attrs, RequestModelKey.String(req.Model))
	}
	if req.Provider != "" {
		attrs = append(attrs, ProviderNameKey.String(req.Provider))
	}
	if req.MaxTokens != nil {
		attrs = append(attrs, RequestMaxTokensKey.Int64(*req.MaxTokens))
	}
	if req.Temperature != nil {
		attrs = append(attrs, RequestTemperatureKey.Float64(*req.Temperature))
	}
	if req.TopP != nil {
		attrs = append(attrs, RequestTopPKey.Float64(*req.TopP))
	}
	if req.FrequencyPenalty != nil {
		attrs = append(attrs, RequestFrequencyPenaltyKey.Float64(*req.FrequencyPenalty))
	}
	if req.PresencePenalty != nil {
		attrs = append(attrs, RequestPresencePenaltyKey.Float64(*req.PresencePenalty))
	}
	if req.IsStream {
		attrs = append(attrs, RequestIsStreamKey.Bool(true))
	}

	// Always use GenAI client span kind
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	}

	spanName := "chat"
	if req.OperationName != "" {
		spanName = req.OperationName
	}

	return o.tracer.Start(ctx, spanName, opts...)
}

func (o *defaultObserver) ObserveChunk(resp Response) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.firstTokenTime.IsZero() {
		o.firstTokenTime = time.Now()
	}

	if resp.Model != "" {
		o.responseModel = resp.Model
	}
	if resp.ID != "" {
		o.responseID = resp.ID
	}
	if len(resp.FinishReasons) > 0 {
		o.finishReasons = append(o.finishReasons, resp.FinishReasons...)
	}

	if resp.InputTokens != nil {
		o.inputTokens += *resp.InputTokens
	}
	if resp.OutputTokens != nil {
		o.outputTokens += *resp.OutputTokens
	}
	if resp.TotalTokens != nil {
		o.totalTokens += *resp.TotalTokens
	}

	if o.options.CaptureContent && resp.ChunkContent != "" {
		if o.contentBuf.Len() < maxContentLength {
			o.contentBuf.WriteString(resp.ChunkContent)
			if o.contentBuf.Len() > maxContentLength {
				// Truncate to exactly maxContentLength if we exceeded it
				str := o.contentBuf.String()
				o.contentBuf.Reset()
				o.contentBuf.WriteString(str[:maxContentLength])
			}
		}
	}
}

func (o *defaultObserver) Finalize(span trace.Span, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	var attrs []attribute.KeyValue

	if o.responseModel != "" {
		attrs = append(attrs, ResponseModelKey.String(o.responseModel))
	}
	if o.responseID != "" {
		attrs = append(attrs, ResponseIDKey.String(o.responseID))
	}
	if len(o.finishReasons) > 0 {
		// Deduplicate and set
		attrs = append(attrs, ResponseFinishReasonsKey.StringSlice(o.finishReasons))
	}

	if o.inputTokens > 0 {
		attrs = append(attrs, UsageInputTokensKey.Int64(o.inputTokens))
	}
	if o.outputTokens > 0 {
		attrs = append(attrs, UsageOutputTokensKey.Int64(o.outputTokens))
	}
	
	// If TotalTokens wasn't provided directly but we have input/output, derive it
	finalTotal := o.totalTokens
	if finalTotal == 0 && (o.inputTokens > 0 || o.outputTokens > 0) {
		finalTotal = o.inputTokens + o.outputTokens
	}
	if finalTotal > 0 {
		attrs = append(attrs, UsageTotalTokensKey.Int64(finalTotal))
	}

	// Calculate TTFT for streaming
	if o.isStreaming && !o.firstTokenTime.IsZero() {
		ttft := o.firstTokenTime.Sub(o.startTime).Seconds()
		attrs = append(attrs, ResponseTimeToFirstToken.Float64(ttft))
	}

	// Append captured content
	if o.options.CaptureContent && o.contentBuf.Len() > 0 {
		attrs = append(attrs, ContentCompletionKey.String(o.contentBuf.String()))
	}

	span.SetAttributes(attrs...)

	if err != nil {
		span.RecordError(err)
	}

	span.End()
}
