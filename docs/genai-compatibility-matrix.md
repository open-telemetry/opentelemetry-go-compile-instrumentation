# GenAI Compatibility Matrix

This document records what `otelc`'s GenAI instrumentation supports today, how it lines up with the
[`semantic-conventions-genai`](https://github.com/open-telemetry/semantic-conventions-genai)
registry, and what is proposed for the targets planned in
[issue #1370](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1370).
It is the design artifact required before implementation begins for Gemini, MCP and the
LangChain-style framework.

The registry is the source of truth for attribute names, allowed values and required-ness. This
document records where the current code already matches it and where it does not.

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
| Registry group | `openai.inference.client` in `model/gen-ai/spans.yaml` |
| Entry point | `NewClient`, via a `before` hook that appends `option.WithMiddleware` |
| Transport | HTTP client middleware |
| Operations | `chat` (`chat/completions`), `text_completion` (`completions`), `embeddings` (`embeddings`) |
| Request attributes | `gen_ai.system`, `gen_ai.operation.name`, `gen_ai.request.model`, `gen_ai.provider.name`, `gen_ai.request.max_tokens`, `gen_ai.request.temperature`, `gen_ai.request.top_p`, `gen_ai.request.frequency_penalty`, `gen_ai.request.presence_penalty` |
| Response attributes | `gen_ai.response.id`, `gen_ai.response.model`, `gen_ai.response.finish_reasons` |
| Usage | `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.usage.total_tokens` (read from the response) |
| Streaming | Supported. A response with `Content-Type: text/event-stream` sets `gen_ai.request.is_stream`, the span is handed to the stream reader and ended when the stream finishes, and the duration metric is recorded at stream end |
| Tool / function calls | Not mapped to attributes today |
| Metrics | `gen_ai.client.operation.duration` (histogram, seconds) |
| Content capture | Off by default. With `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=true`, prompts and completions are emitted as `gen_ai.content.prompt` / `gen_ai.content.completion` events |
| Known gaps | Any endpoint outside the three classified paths is passed through with no span. A request whose body does not parse, or which carries no `model`, is passed through |

### github.com/anthropics/anthropic-sdk-go

One module, built against v1.67.0. The rule declares a minimum library version of v1.0.0.

| | |
| --- | --- |
| Registry group | `anthropic.inference.client` in `model/gen-ai/spans.yaml` |
| Entry point | `NewClient`, via a `before` hook that appends `option.WithMiddleware` |
| Transport | HTTP client middleware |
| Operations | `chat` (`/v1/messages`), `count_tokens` (`/v1/messages/count_tokens`) |
| Request attributes | `gen_ai.system`, `gen_ai.operation.name`, `gen_ai.request.model`, `gen_ai.provider.name`, `gen_ai.request.max_tokens`, `gen_ai.request.temperature`, `gen_ai.request.top_p`, `gen_ai.request.top_k` |
| Response attributes | `gen_ai.response.id`, `gen_ai.response.model`, `gen_ai.response.finish_reasons` |
| Usage | `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.usage.total_tokens` (computed as input + output), plus `gen_ai.usage.cache_read.input_tokens` and `gen_ai.usage.cache_creation.input_tokens` |
| Streaming | Not instrumented. A request carrying `"stream": true` returns before a span is created, so no GenAI span is emitted at all |
| Tool / function calls | Not mapped to attributes today |
| Metrics | `gen_ai.client.operation.duration` (histogram, seconds) |
| Content capture | Not implemented. Content is never captured, including when the capture variable is enabled |
| Known gaps | Streaming, and message batches (`/v1/messages/batches`), which pass through with no span |

`gen_ai.request.is_stream` is set only in the defensive path where a request that did not declare
streaming receives an SSE response. That span is ended immediately without response attributes,
because the event stream is not accumulated.

## Registry conformance gaps

Found while comparing the current code against `model/gen-ai/registry.yaml`. These are migration
work items, not new conventions.

* **`gen_ai.provider.name` values.** The provider table in the OpenAI middleware can emit 18
  values. Four are near misses that the registry spells differently: `azure` (registry:
  `azure.ai.openai` or `azure.ai.inference`), `google` (`gcp.gemini`, `gcp.vertex_ai` or
  `gcp.gen_ai`), `mistral` (`mistral_ai`) and `moonshot` (`moonshot_ai`). Ten have no registry
  value at all: `ark`, `baidu`, `local`, `minimax`, `ollama`, `qwen`, `siliconflow`, `tencent`,
  `together`, `zhipu`. Only `openai`, `anthropic`, `deepseek` and `groq` match as written.
  Correcting these changes attribute values users may already query on, so it belongs in release
  notes.
* **`gen_ai.usage.total_tokens`.** OpenAI reads it from the response; Anthropic computes input
  plus output. The registry defines one attribute, so the derivation belongs in shared code rather
  than in each provider.
* **Tool and function calls.** Declared in the registry but not emitted by either module today.

## Planned targets

The package and version range for each target below is **proposed and awaiting maintainer
confirmation**. Coverage rows are the intended scope, not implemented behavior.

### Gemini

| | |
| --- | --- |
| Candidate package | `google.golang.org/genai` |
| Registry coverage | No Gemini-specific span group exists. The provider value `gcp.gemini` is declared, so this target implements the common GenAI spans rather than a provider group |
| Open question | [PR #1153](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1153) already adds instrumentation for this package. If it merges, the Gemini milestone becomes a migration onto generated conventions rather than new instrumentation |
| Transport | HTTP |
| Intended coverage | Unary generate-content requests, model metadata, usage attributes, error mapping; streaming where the SDK's API supports interception |

### MCP

| | |
| --- | --- |
| Candidate packages | `github.com/modelcontextprotocol/go-sdk`, or `github.com/mark3labs/mcp-go` |
| Registry coverage | `model/mcp/` declares `trace.mcp.common.attributes` plus operation and session metrics |
| Transport | To be determined from the selected SDK. MCP is not HTTP-only, so instrumentation follows the message/operation lifecycle. Where the deployment uses a Kafka or Redis message flow, the existing instrumentation for those transports provides the lifecycle hooks |
| Intended coverage | Operation lifecycle spans, tool invocation, error mapping |
| Constraint | The shared runtime layer must be transport-neutral for this target to be feasible |

### LangChain-style framework

| | |
| --- | --- |
| Candidate package | `github.com/tmc/langchaingo` |
| Registry coverage | Covered by the common GenAI agent and tool conventions; no framework-specific group |
| Transport | Framework-level; the underlying provider call may already be instrumented separately |
| Intended coverage | Chain and agent step lifecycle, tool calls, usage aggregation |
| Open question | How to avoid duplicate spans when the framework calls an SDK that `otelc` already instruments |

## Requirements that apply to every target

Taken from the acceptance criteria in issue #1370:

* Rules live in the appropriate `instrumentation/` module, with documented supported versions.
* Telemetry comes from generated semconv types backed by the registry, not ad-hoc attributes.
* Emitted spans, attributes and events are verified against the registry.
* Unit tests plus integration coverage under the existing `test/` structure.
* A runnable example application.
* Message content is not captured unless
  `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` is explicitly enabled.
* Unsupported APIs and transports are documented rather than silently skipped.
