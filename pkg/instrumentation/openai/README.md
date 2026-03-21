# OpenAI Compile-Time Instrumentation

Automatic OpenTelemetry instrumentation for `github.com/openai/openai-go` using compile-time code injection.

## Overview

Instruments OpenAI chat completion API calls at compile-time with zero code changes required.

## Features

- Zero code changes - automatic instrumentation during build
- Chat completion coverage (`client.Chat.Completions.New`)
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