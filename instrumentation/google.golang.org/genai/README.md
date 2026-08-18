# Google Gen AI Go SDK Compile-Time Instrumentation

This package provides automatic OpenTelemetry instrumentation for the
[Google Gen AI SDK for Go](https://github.com/googleapis/go-genai)
(`google.golang.org/genai`) using compile-time code injection.

## Overview

The SDK is the single Go client for both of Google's generative AI surfaces,
the Gemini Developer API and Vertex AI. This package instruments **all**
`GenerateContent` calls in your application at compile time. Zero code changes
required.

### Key Features

- **Zero Code Changes**: Automatic instrumentation without modifying application code
- **GenAI Semantic Conventions**: Follows OpenTelemetry GenAI semantic conventions for AI/LLM observability
- **Exact Provider Detection**: Reads the configured backend off the client, so Vertex AI and the Gemini Developer API are distinguished precisely rather than guessed from the request host
- **Request Attribute Extraction**: Captures model, temperature, top_p, top_k, and max_tokens
- **Response Metadata**: Records response ID, model version, finish reasons, and token usage
- **Thinking-Token Accounting**: Folds `thoughtsTokenCount` into `gen_ai.usage.output_tokens`, which the API reports separately
- **HTTP Suppression**: Prevents duplicate spans by suppressing the generic `net/http` client instrumentation for instrumented requests

## Supported Operations

| Operation | Endpoint | Status |
| ----------- | ---------- | -------- |
| Generate content (non-streaming) | `POST .../<model>:generateContent` | Supported |
| Generate content (streaming) | `POST .../<model>:streamGenerateContent` | Pass-through (see [Limitations](#limitations)) |
| Count tokens | `POST .../<model>:countTokens` | Not instrumented |
| Embed content | `POST .../<model>:embedContent` | Not instrumented |

Both backends are covered, because both route through the same method:

```text
Gemini Developer API  /v1beta/models/<model>:generateContent
Vertex AI             /v1beta1/projects/<p>/locations/<l>/publishers/google/models/<model>:generateContent
```

`Chats` sessions are covered too, because `client.Chats` issues the same
`generateContent` requests underneath.

## How It Works

### Compile-Time Injection

The instrumentation hooks `genai.NewClient` and decorates the transport of the
HTTP client the SDK settled on:

```text
┌─────────────────────────────────────────────┐
│  1. go build (with otelc toolexec)          │
│                                             │
│  2. Setup Phase:                            │
│     - Scan dependencies                     │
│     - Match google.golang.org/genai NewClient│
│     - Generate otelc.runtime.go             │
│                                             │
│  3. Instrument Phase:                       │
│     - Inject after-hook into NewClient      │
│     - Tracing transport installed on the    │
│       client's *http.Client                 │
│                                             │
│  4. Build with instrumentation baked in     │
└─────────────────────────────────────────────┘
```

### Why an After Hook

`NewClient` fills in a nil `ClientConfig.HTTPClient` itself, and for Vertex AI
without explicit credentials the client it builds carries
application-default-credentials authentication. A Before hook that substituted
its own client would break that authentication. Hooking the return value instead
means the instrumentation decorates whatever client the SDK chose, so
authentication is untouched.

`Client.ClientConfig()` returns a copy of the config, but `HTTPClient` is a
pointer the copy shares with the internal API client that issues requests, so
replacing its transport takes effect for every subsequent call.

### Runtime Execution

1. **After `NewClient`**: Wraps the client's transport, once only; clients sharing an `*http.Client` do not stack
2. **Request**: Classifies the path, reads generation parameters, creates a span
3. **Response**: Records response metadata, token usage, and ends the span

## Semantic Conventions

This instrumentation emits spans following the
[GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/).

### Span Name

`chat <model>` (e.g. `chat gemini-2.5-flash`)

### Attributes

| Attribute | Description | Example |
| ----------- | ------------- | --------- |
| `gen_ai.system` | AI system | `"gemini"` / `"vertex_ai"` |
| `gen_ai.provider.name` | Provider, from the configured backend | `"gcp.gemini"` / `"gcp.vertex_ai"` |
| `gen_ai.operation.name` | Operation type | `"chat"` |
| `gen_ai.request.model` | Model requested | `"gemini-2.5-flash"` |
| `gen_ai.request.max_tokens` | `generationConfig.maxOutputTokens` | `1024` |
| `gen_ai.request.temperature` | `generationConfig.temperature` | `0.7` |
| `gen_ai.request.top_p` | `generationConfig.topP` | `0.9` |
| `gen_ai.request.top_k` | `generationConfig.topK` | `40` |
| `gen_ai.request.is_stream` | Set only when a response arrives as SSE | `true` |
| `gen_ai.response.id` | `responseId` | `"resp-abc123"` |
| `gen_ai.response.model` | `modelVersion` | `"gemini-2.5-flash-001"` |
| `gen_ai.response.finish_reasons` | Per-candidate `finishReason` | `["STOP"]` |
| `gen_ai.usage.input_tokens` | `promptTokenCount` | `150` |
| `gen_ai.usage.output_tokens` | `candidatesTokenCount` + `thoughtsTokenCount` | `42` |
| `gen_ai.usage.total_tokens` | `totalTokenCount` (derived if absent) | `192` |
| `error.type` | HTTP status code on a failed call | `"429"` |

### Token Accounting

Two Gemini-specific details are normalized so the span means the same thing it
does for the other GenAI instrumentations:

- **Thinking tokens.** `thoughtsTokenCount` is generated and billed as output but
  reported apart from `candidatesTokenCount`, so the two are summed into
  `gen_ai.usage.output_tokens`.
- **Cached prompts.** `promptTokenCount` already covers cached content, so it
  maps onto `gen_ai.usage.input_tokens` unchanged, unlike Anthropic's
  `input_tokens`, which excludes cache reads and has to be folded back together.

## Configuration

```bash
# Enable only Gemini instrumentation
export OTEL_GO_ENABLED_INSTRUMENTATIONS=gemini

# Disable only Gemini instrumentation
export OTEL_GO_DISABLED_INSTRUMENTATIONS=gemini
```

Instrumentation names are lowercase. If neither variable is set, all
instrumentations run by default.

### Privacy

Prompt and response content is **never captured**. Only request parameters
(model, temperature, ...) and response metadata (token counts, finish reasons)
are recorded. The GenAI semantic conventions gate content capture behind
`OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT`; this instrumentation does
not implement that opt-in, so metadata-only is the only behaviour.

The request body is read through `Request.GetBody`, the replayable copy
net/http keeps, so the payload the server receives is never consumed. The
response body is read through a bounded `io.TeeReader` (4 MB) and reassembled so
the SDK always sees the full payload.

## Limitations

- **Streaming is not yet instrumented.** `streamGenerateContent` requests pass
  through without a span, because usage totals only arrive in the final
  server-sent event and this instrumentation does not accumulate SSE yet.
- Only `generateContent` is instrumented; `countTokens`, `embedContent`,
  image/video generation and the remaining model RPCs pass through.
- Spans only, no metrics are recorded.

## Example

```go
package main

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  "...",
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Models.GenerateContent(ctx, "gemini-2.5-flash",
		genai.Text("Hello, world!"), nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Text())
}
```

Build with otelc to instrument automatically:

```bash
otelc go build -o myapp .
./myapp
```

The resulting binary emits a GenAI span for every `GenerateContent` call.
