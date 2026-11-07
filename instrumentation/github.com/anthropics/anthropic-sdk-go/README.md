# Anthropic SDK Go Compile-Time Instrumentation

This package provides automatic OpenTelemetry instrumentation for the
[Anthropic SDK for Go](https://github.com/anthropics/anthropic-sdk-go) using
compile-time code injection.

## Overview

Unlike traditional instrumentation that requires manually adding middleware, this
package automatically instruments **all** Anthropic API calls in your application
at compile-time. Zero code changes required!

### Key Features

- **Zero Code Changes**: Automatic instrumentation without modifying application code
- **GenAI Semantic Conventions**: Follows OpenTelemetry GenAI semantic conventions for AI/LLM observability
- **Request Attribute Extraction**: Captures model, temperature, top_p, top_k, and max_tokens
- **Response Metadata**: Records response ID, model, finish reasons, and token usage
- **Cache Usage Tracking**: Records Anthropic-specific prompt cache metrics (input_tokens, cache_read, cache_creation)
- **Provider Detection**: Automatically detects the API provider from the request URL
- **HTTP Suppression**: Prevents duplicate spans by suppressing the generic `net/http` client instrumentation for instrumented requests

## Supported Operations

| Operation | Endpoint | Status |
|-----------|----------|--------|
| Messages (non-streaming) | `POST /v1/messages` | Supported |
| Messages (streaming) | `POST /v1/messages` | Pass-through (see [Limitations](#limitations)) |
| Count tokens | `POST /v1/messages/count_tokens` | Supported |
| Message batches | `POST /v1/messages/batches` | Not instrumented |

## How It Works

### Compile-Time Injection

The instrumentation hooks into `anthropic.NewClient` and injects HTTP middleware
that intercepts API calls:

```
┌─────────────────────────────────────────────┐
│  1. go build (with otelc toolexec)          │
│                                             │
│  2. Setup Phase:                            │
│     - Scan dependencies                     │
│     - Match anthropic-sdk-go/NewClient      │
│     - Generate otelc.runtime.go              │
│                                             │
│  3. Instrument Phase:                       │
│     - Inject before-hook into NewClient     │
│     - Middleware installed via option        │
│                                             │
│  4. Build with instrumentation baked in     │
└─────────────────────────────────────────────┘
```

### Runtime Execution

When your application runs, the injected hook automatically:

1. **Before**: Installs the OTel middleware into the client's HTTP transport
2. **Request**: Classifies the operation, parses request attributes, creates a span
3. **Response**: Records response metadata, token usage, and ends the span

## Semantic Conventions

This instrumentation emits spans following the
[GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/):

### Span Name

`chat <model>` (e.g., `chat claude-sonnet-4-5`) or `count_tokens <model>`

### Attributes

| Attribute | Description | Example |
|-----------|-------------|---------|
| `gen_ai.system` | AI system provider | `"anthropic"` |
| `gen_ai.operation.name` | Operation type | `"chat"` or `"count_tokens"` (token counting has no GenAI semconv value yet) |
| `gen_ai.request.model` | Model used | `"claude-sonnet-4-5"` |
| `gen_ai.response.model` | Response model | `"claude-sonnet-4-5"` |
| `gen_ai.response.id` | Response ID | `"msg_01XFDUDYJgAACzvnptvVoYEL"` |
| `gen_ai.provider.name` | Detected provider | `"anthropic"` |
| `gen_ai.request.max_tokens` | Max tokens requested | `1024` |
| `gen_ai.request.temperature` | Temperature | `0.7` |
| `gen_ai.request.top_p` | Top-p sampling | `0.9` |
| `gen_ai.request.top_k` | Top-k sampling | `40` |
| `gen_ai.request.is_stream` | Whether streaming was requested | `false` |
| `gen_ai.response.finish_reasons` | Stop reasons | `["end_turn"]` |
| `gen_ai.usage.input_tokens` | Input token count | `150` |
| `gen_ai.usage.output_tokens` | Output token count | `42` |
| `gen_ai.usage.total_tokens` | Total token count | `192` |
| `gen_ai.usage.cache_read.input_tokens` | Cache read tokens (Anthropic-specific) | `50` |
| `gen_ai.usage.cache_creation.input_tokens` | Cache creation tokens (Anthropic-specific) | `30` |
| `server.address` | GenAI server hostname or IP address | `"api.anthropic.com"` |
| `server.port` | GenAI server port, including default ports | `443` |

## Configuration

The instrumentation can be enabled or disabled at runtime using environment
variables:

```bash
# Enable only Anthropic instrumentation
export OTEL_GO_ENABLED_INSTRUMENTATIONS=anthropic

# Disable only Anthropic instrumentation
export OTEL_GO_DISABLED_INSTRUMENTATIONS=anthropic
```

Instrumentation names are lowercase. If neither variable is set, all
instrumentations run by default.

### Native SDK OpenTelemetry (`v1.81.0+`)

Starting in `v1.81.0`, `anthropic-sdk-go` includes built-in OpenTelemetry
tracing that activates whenever a global `TracerProvider` is registered. When
`otelc`'s Anthropic instrumentation is enabled (the default), it automatically
sets `ANTHROPIC_OPEN_TELEMETRY=false` (if not already set) to prevent duplicate
spans.

To use the SDK's built-in tracing instead of `otelc`'s middleware, either:

- Set `ANTHROPIC_OPEN_TELEMETRY=true` (or any non-`false` value) at runtime, or
- Disable `otelc`'s Anthropic instrumentation via `OTEL_GO_DISABLED_INSTRUMENTATIONS=anthropic`.

> **Note:** Because `ANTHROPIC_OPEN_TELEMETRY=false` is set process-wide, built-in SDK spans are also disabled for message batches (`POST /v1/messages/batches`), which `otelc` does not currently instrument. To trace message batches, enable native SDK tracing via `ANTHROPIC_OPEN_TELEMETRY=true`.

### Privacy

Message bodies are **never captured** by default. Only request parameters
(model, temperature, etc.) and response metadata (token counts, finish reasons)
are recorded. The request and response bodies are read through a bounded
`io.TeeReader` (1 MB request, 4 MB response) for attribute extraction, then
reassembled so the SDK always receives the full payload.

## Limitations

- **Streaming is not yet instrumented.** When `"stream": true` is set in the
  request, the middleware passes the request through without creating a span.
  Streaming support is planned as a follow-up.
- Message batches (`POST /v1/messages/batches`) are passed through without
  instrumentation. Since `otelc` disables native SDK tracing process-wide by
  default, message batches will not emit spans unless `ANTHROPIC_OPEN_TELEMETRY=true`
  is set.

## Example

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/anthropics/anthropic-sdk-go"
)

func main() {
	client := anthropic.NewClient()

	resp, err := client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model: "claude-sonnet-4-5",
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("Hello, world!")),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp.Content[0].Text)
}
```

Build with otelc to instrument automatically:

```bash
otelc go build -o myapp .
./myapp
```

The resulting binary will emit GenAI spans for every Anthropic API call.
