# OpenAI Compile-Time Instrumentation

Automatic OpenTelemetry instrumentation for `github.com/openai/openai-go` using compile-time code injection.

## Overview

Instruments OpenAI chat completion API calls at compile-time with zero code changes required. The instrumentation is implemented as an HTTP middleware injected via `option.WithMiddleware` at client construction time, which makes it portable across SDK major versions.

## Supported SDK Versions

| Package import path          | Hook module                                    |
|------------------------------|------------------------------------------------|
| `github.com/openai/openai-go`    | `pkg/instrumentation/openai/v1sdk` |
| `github.com/openai/openai-go/v2` | `pkg/instrumentation/openai/v2`    |
| `github.com/openai/openai-go/v3` | `pkg/instrumentation/openai/v3`    |

Azure OpenAI deployments are covered automatically — they call `openai.NewClient(azure.WithEndpoint(...))`, which is the same entry point this instrumentation hooks.

## Architecture

```
pkg/instrumentation/openai/
├── semconv/   GenAI attribute keys + helpers (shared across versions)
├── v1sdk/     Hook for github.com/openai/openai-go (SDK v1.x)
│   └── internal/middleware/   HTTP middleware, body parsing, metrics
├── v2/        Hook for github.com/openai/openai-go/v2
│   └── internal/middleware/   HTTP middleware, body parsing, metrics
└── v3/        Hook for github.com/openai/openai-go/v3
    └── internal/middleware/   HTTP middleware, body parsing, metrics
```

Each `v{N}/hook.go` is a thin adapter that prepends `option.WithMiddleware(middleware.OtelMiddleware)` to the opts slice passed to `NewClient` and `NewChatCompletionService`. The span, metric, and body-parsing logic is duplicated across each version's `internal/middleware/` subpackage: the compile-time framework only wires up a hook module plus the framework-provided `pkg/instrumentation/shared`, so keeping middleware as an internal subpackage avoids a cross-module dependency that the tool cannot resolve. Adding future SDK majors is a matter of copying a hook module wholesale.

## Features

- Zero code changes - automatic instrumentation during build
- Single shared HTTP middleware across every supported SDK major version
- Covers `openai.NewClient` and `openai.NewChatCompletionService` (and therefore Azure)
- Follows OpenTelemetry GenAI semantic conventions v1.37.0
- Automatic context propagation, token usage tracking, and error recording

## Usage

```bash
# Build with automatic instrumentation
/path/to/otelc go build -a

# Run normally
./myapp
```

## Configuration

Set environment variables to control behavior:

```bash
# Enable/disable instrumentation
export OTEL_GO_ENABLED_INSTRUMENTATIONS=openai
export OTEL_GO_DISABLED_INSTRUMENTATIONS=openai

# OpenTelemetry configuration
export OTEL_SERVICE_NAME=my-service
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
```

## Testing

```bash
# Unit tests
go test -v ./...

# Integration tests (no API key required for default tests)
go test -v -tags=integration ./test/integration/openai_*

# Demo (requires API key)
cd demo/app/openai/client
OPENAI_API_KEY=sk-... /path/to/otelc go run main.go
```

## Documentation

- [Semantic Conventions](../../docs/semantic-conventions.md)
- [Getting Started](../../docs/getting-started.md)
- [Implementation Details](../../docs/implementation.md)

## References

- [OTel GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/)
- [OpenAI API Reference](https://platform.openai.com/docs/api-reference)