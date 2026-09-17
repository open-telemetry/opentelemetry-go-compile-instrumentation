# 6. Provider-Neutral GenAI Adapter Contract

Date: 2026-09-17

## Status

Proposed

## Context

`otelc` currently ships GenAI instrumentation for two SDKs: `github.com/openai/openai-go`
(three major versions) and `github.com/anthropics/anthropic-sdk-go`. Each module owns a full
copy of the GenAI telemetry logic, and the copies have started to diverge.

The `semconv/genai.go` helper file exists four times: once in each of the three OpenAI modules at
96 lines each, and once in the Anthropic module at 101 lines. The OpenAI v2 and v3 copies are
byte-identical. The Anthropic copy has the same shape but has drifted.

The middleware files repeat the same lifecycle in each module:

1. A `before` hook on `NewClient` injects an HTTP middleware through the SDK's option list.
2. The middleware classifies the operation from the request path and gives up if it does not
   recognize it.
3. The request body is read through a bounded `io.TeeReader` and reassembled with an
   `io.MultiReader` so the SDK still receives the full payload.
4. A client span named `<operation> <model>` is started with `gen_ai.system`,
   `gen_ai.operation.name`, `gen_ai.request.model` and `gen_ai.provider.name`.
5. `runtime.SuppressHTTPClientInstrumentation` prevents a duplicate `net/http` client span.
6. The response is mapped to usage attributes and finish reasons, or to an error status, and the
   span is ended. For a streaming response the span outlives the middleware call and is ended by
   the stream reader.

Provider detection, operation classification, body-size limits, error mapping and the duration
histogram are duplicated with small differences in every module.

Behavior that differs today:

| Concern | OpenAI | Anthropic |
| --- | --- | --- |
| Operations | `chat`, `text_completion`, `embeddings` | `chat`, `count_tokens`; batches pass through |
| Message content capture | Gated on `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT`, emitted as `gen_ai.content.prompt` / `gen_ai.content.completion` events | Not implemented; content is never captured |
| Streaming | Instrumented through `streaming_bridge.go` and `internal/streaming`; the duration metric is recorded when the stream ends | `gen_ai.request.is_stream` is set, the stream itself is not instrumented |
| Total token usage | Read from the response `usage.total_tokens` | Computed as input + output |
| Provider-specific usage | None | `gen_ai.usage.cache_read.input_tokens`, `gen_ai.usage.cache_creation.input_tokens` |
| Provider host table | 23 entries | Fewer entries |

Three further targets are planned this term: Gemini, MCP, and a LangChain-style framework. MCP and
agent frameworks are not necessarily HTTP-based, so a contract expressed in terms of
`*http.Request` and `*http.Response` would not extend to them.

ADR 0004 establishes that instrumentation modules own their rules, version ranges and hooks. A
shared contract must not take that ownership away.

## Decision

Introduce a provider-neutral GenAI adapter contract in shared code. Provider modules keep their
own rules, hooks, version ranges and SDK-specific parsing, and call into the contract to produce
telemetry.

The contract is expressed in terms of an operation lifecycle, not a transport. An HTTP middleware
becomes one caller of the contract rather than the contract itself, which allows a message-based
integration such as MCP to use the same lifecycle.

The contract covers:

* The operation lifecycle: starting an operation from a normalized request description, and ending
  it with a response, an error, or a stream completion.
* Request mapping for model, operation name, provider name, and the sampling parameters that
  already appear in both modules.
* Response mapping for response id, response model and finish reasons.
* Usage mapping for input, output and total tokens, with the total derived when a provider does
  not report one.
* The streaming lifecycle, as an explicit handover, so that the span can be ended after the
  middleware returns and the duration metric is still recorded exactly once.
* Error and status mapping for transport errors and HTTP-style status errors, producing the
  `error.type` attribute and span status that both modules set today.
* Privacy filtering, as one implementation of the
  `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` gate, with content capture disabled unless
  the variable is explicitly enabled.
* A normalized representation of tool and function calls, which the agent frameworks later in the
  term require.

Provider-specific behavior stays with the provider through an extension point that contributes
additional attributes to an in-flight operation. Anthropic's cache token attributes are the first
consumer of that extension point, and they must survive the migration unchanged.

Migration order: define the contract, migrate OpenAI with no observable change in emitted
telemetry, then normalize Anthropic on the same contract.

## Consequences

* The four copies of `semconv/genai.go` and the duplicated provider tables, body limits and error
  mapping collapse into one implementation, so a semantic-convention change is made once.
* A new provider is a mapping layer rather than a new copy of the whole lifecycle, which is what
  makes Gemini, MCP and LangChain feasible inside one term.
* Migrating Anthropic onto the contract gives it message content capture, which it does not have
  today. That is a deliberate behavior change and needs maintainer sign-off, because it changes
  what an Anthropic user's spans contain when the capture variable is enabled.
* Streaming is the highest-risk part of the migration. The span outlives the middleware call, and
  the duration metric must still be recorded exactly once. Regression coverage for the existing
  OpenAI streaming paths is a precondition for the migration, not a follow-up.
* Instrumentation modules gain a dependency on the shared contract. Module ownership under
  ADR 0004 is preserved: rules, version ranges, hooks and SDK-specific parsing stay in the module.
* Three copies of the OpenAI instrumentation remain, one per major version. The contract removes
  the duplicated telemetry logic but not the per-version modules themselves.
* The contract's shape is constrained by targets that are not yet selected. The compatibility
  matrix in `docs/genai-compatibility-matrix.md` records the open choices for Gemini, MCP and the
  LangChain-style framework, and the contract is not final until those are confirmed.
