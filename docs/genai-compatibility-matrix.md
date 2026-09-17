# GenAI Compatibility Matrix

This document records what `otelc`'s GenAI instrumentation supports today, and what is proposed
for the targets planned in [issue #1370](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1370).
It is the design artifact required before implementation begins for Gemini, MCP and the
LangChain-style framework.

Status values: **Supported** (instrumented and covered by tests), **Partial** (instrumented with
documented gaps), **Not instrumented** (traffic passes through untouched), **Proposed** (not yet
confirmed with maintainers).

## Currently instrumented

### github.com/openai/openai-go

Three modules exist, one per major version: `openai-go` (built against v1.12.0), `openai-go/v2`
(v2.7.1) and `openai-go/v3` (v3.53.0). The rule files declare no minimum library version, so a
rule applies to any version whose `NewClient` matches.

| | |
| --- | --- |
| Entry point | `NewClient`, via a `before` hook that appends `option.WithMiddleware` |
| Transport | HTTP client middleware |
| Operations | `chat` (`chat/completions`), `text_completion` (`completions`), `embeddings` (`embeddings`) |
| Request attributes | `gen_ai.system`, `gen_ai.operation.name`, `gen_ai.request.model`, `gen_ai.provider.name`, `gen_ai.request.max_tokens`, `gen_ai.request.temperature`, `gen_ai.request.top_p`, `gen_ai.request.frequency_penalty`, `gen_ai.request.presence_penalty` |
| Response attributes | `gen_ai.response.id`, `gen_ai.response.model`, `gen_ai.response.finish_reasons` |
| Usage | `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.usage.total_tokens` (read from the response) |
| Streaming | Supported. `gen_ai.request.is_stream` is set, the span is ended by the stream reader, and the duration metric is recorded at stream end |
| Tool / function calls | Not mapped to attributes today |
| Metrics | `gen_ai.client.operation.duration` (histogram, seconds) |
| Content capture | Off by default. With `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=true`, prompts and completions are emitted as `gen_ai.content.prompt` / `gen_ai.content.completion` events |
| Known gaps | Any endpoint outside the three classified paths is passed through with no span. A request whose body does not parse, or which carries no `model`, is passed through |

### github.com/anthropics/anthropic-sdk-go

One module, built against v1.67.0. The rule declares a minimum library version of v1.0.0.

| | |
| --- | --- |
| Entry point | `NewClient`, via a `before` hook that appends `option.WithMiddleware` |
| Transport | HTTP client middleware |
| Operations | `chat` (`/v1/messages`), `count_tokens` (`/v1/messages/count_tokens`) |
| Request attributes | `gen_ai.system`, `gen_ai.operation.name`, `gen_ai.request.model`, `gen_ai.provider.name`, `gen_ai.request.max_tokens`, `gen_ai.request.temperature`, `gen_ai.request.top_p`, `gen_ai.request.top_k`, `gen_ai.request.is_stream` |
| Response attributes | `gen_ai.response.id`, `gen_ai.response.model`, `gen_ai.response.finish_reasons` |
| Usage | `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.usage.total_tokens` (computed as input + output), plus `gen_ai.usage.cache_read.input_tokens` and `gen_ai.usage.cache_creation.input_tokens` |
| Streaming | Partial. `gen_ai.request.is_stream` is set; the stream itself is not instrumented |
| Tool / function calls | Not mapped to attributes today |
| Metrics | `gen_ai.client.operation.duration` (histogram, seconds) |
| Content capture | Not implemented. Content is never captured, including when the capture variable is enabled |
| Known gaps | Streaming, and message batches (`/v1/messages/batches`), which pass through with no span |

## Planned targets

The package and version range for each target below is **proposed and awaiting maintainer
confirmation**. Coverage rows are the intended scope, not implemented behavior.

### Gemini

| | |
| --- | --- |
| Candidate package | `google.golang.org/genai` |
| Open question | [PR #1153](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1153) already adds instrumentation for this package. If it merges, the Gemini milestone becomes a migration onto the shared contract rather than new instrumentation |
| Transport | HTTP |
| Intended coverage | Unary generate-content requests, model metadata, usage attributes, error mapping; streaming where the SDK's API supports interception |

### MCP

| | |
| --- | --- |
| Candidate packages | `github.com/modelcontextprotocol/go-sdk`, or `github.com/mark3labs/mcp-go` |
| Transport | To be determined from the selected SDK. MCP is not HTTP-only, so instrumentation follows the message/operation lifecycle. Where the deployment uses a Kafka or Redis message flow, the existing instrumentation for those transports provides the lifecycle hooks |
| Intended coverage | Operation lifecycle spans, tool invocation, error mapping |
| Constraint | The adapter contract must be transport-neutral for this target to be feasible |

### LangChain-style framework

| | |
| --- | --- |
| Candidate package | `github.com/tmc/langchaingo` |
| Transport | Framework-level; the underlying provider call may already be instrumented separately |
| Intended coverage | Chain and agent step lifecycle, tool calls, usage aggregation |
| Open question | How to avoid duplicate spans when the framework calls an SDK that `otelc` already instruments |

## Requirements that apply to every target

Taken from the acceptance criteria in issue #1370:

* Rules live in the appropriate `instrumentation/` module, with documented supported versions.
* Telemetry is declared through the schema definitions in `schemas/otelc/groups/`, not as ad-hoc
  attributes.
* Unit tests plus integration coverage under the existing `test/` structure.
* A runnable example application.
* Message content is not captured unless
  `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` is explicitly enabled.
* Unsupported APIs and transports are documented rather than silently skipped.
