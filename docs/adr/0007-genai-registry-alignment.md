# 7. GenAI Instrumentation Aligned to the Semantic Conventions Registry

Date: 2026-09-19

## Status

Proposed

Replaces the custom adapter contract proposed in the first revision of this ADR, following the
project-plan update in #1370. Depends on #728 (generate Go semconv types from the Weaver registry)
and #745 (centralize telemetry contracts under `schemas/otelc/groups/`).

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
   span is ended. Streaming is handled differently in each module: OpenAI hands the span to a
   stream reader that ends it later, while Anthropic passes a declared streaming request through
   without a span at all.

Provider detection, operation classification, body-size limits, error mapping and the duration
histogram are duplicated with small differences in every module.

Because each module writes its attributes by hand, nothing checks them against the published
conventions. Two examples found while surveying the code:

* The provider table in the OpenAI middleware can emit 18 different values for
  `gen_ai.provider.name`. Fourteen of them are not members of the registry's enum. Four are near
  misses that should be the registry spelling instead (`azure` / `azure.ai.openai`, `google` /
  `gcp.gemini`, `mistral` / `mistral_ai`, `moonshot` / `moonshot_ai`), and ten have no registry
  value at all.
* `gen_ai.usage.total_tokens` is read from the response by OpenAI and computed as input plus
  output by Anthropic.

Three further targets are planned: Gemini, MCP, and a LangChain-style framework. MCP and agent
frameworks are not necessarily HTTP-based, so instrumentation expressed in terms of
`*http.Request` and `*http.Response` would not extend to them.

ADR 0004 establishes that instrumentation modules own their rules, version ranges and hooks. A
shared layer must not take that ownership away.

## Decision

Treat the [`semantic-conventions-genai`](https://github.com/open-telemetry/semantic-conventions-genai)
registry as the source of truth for GenAI telemetry. `otelc` implements conventions that the
registry already declares and does not define its own GenAI contract.

The registry supplies the semantics. `model/gen-ai/spans.yaml` declares `openai.inference.client`
and `anthropic.inference.client` span groups, `model/gen-ai/registry.yaml` declares the shared
attributes and the `gen_ai.provider.name` enum, and `model/mcp/` declares the MCP conventions.
Attribute names, allowed values and required-ness come from there, surfaced as generated Go types
through the pipeline in #728 and organized under `schemas/otelc/groups/` per #745.

Shared Go code is limited to runtime mechanics that the registry does not describe:

* Span lifecycle, including the handover that lets a streaming span be ended after the middleware
  returns while the duration metric is still recorded exactly once.
* Privacy filtering, as one implementation of the
  `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` gate, with content capture disabled unless
  the variable is explicitly enabled.
* Error and status mapping for transport errors and HTTP-style status errors.
* Usage attribute assembly, including deriving a total when a provider does not report one.

This shared code is expressed in terms of an operation lifecycle rather than a transport, so a
message-based integration such as MCP can use it without an HTTP request and response.

Provider modules keep their rules, version ranges, hooks and SDK-specific parsing, and are
responsible for mapping provider-native requests, responses, errors, streams and tool calls onto
the generated types.

Migration order: land the #728 and #745 prerequisites, migrate OpenAI onto generated conventions
without changing observable semantics, then Anthropic, then implement the declared conventions for
Gemini, MCP and a LangChain-style framework.

## Consequences

* Attribute drift becomes detectable rather than invisible. The provider-value and total-token
  divergences above are the kind of defect this alignment is meant to surface, and they are fixed
  as part of each migration rather than as separate cleanups.
* The four copies of `semconv/genai.go` and the duplicated provider tables, body limits and error
  mapping collapse into generated types plus one shared runtime layer.
* A new provider becomes a mapping layer rather than a new copy of the whole lifecycle, which is
  what makes Gemini, MCP and LangChain feasible inside one term.
* Correcting `gen_ai.provider.name` to registry values changes attribute values that users may
  already be querying on. The migration must call this out in release notes, and the affected
  values are listed in the compatibility matrix.
* Anthropic gains message content capture, which it does not implement today. That is a
  deliberate behavior change and needs maintainer sign-off, because it changes what an Anthropic
  user's spans contain when the capture variable is enabled.
* Streaming is the highest-risk part of the migration. The OpenAI span outlives the middleware
  call, and the duration metric must still be recorded exactly once. Regression coverage for the
  existing OpenAI streaming paths is a precondition for the migration, not a follow-up.
* This work is sequenced behind #728 and #745. Until they land, the migrations cannot start, so
  the prerequisites are the critical path rather than the provider work itself.
* Instrumentation modules gain a dependency on the generated types and the shared runtime layer.
  Module ownership under ADR 0004 is preserved: rules, version ranges, hooks and SDK-specific
  parsing stay in the module.
* Three copies of the OpenAI instrumentation remain, one per major version. Generated conventions
  remove the duplicated telemetry definitions but not the per-version modules themselves.
* The registry does not declare a Gemini-specific span group today; it declares the provider value
  `gcp.gemini` alongside the common GenAI spans. The Gemini milestone therefore implements the
  common conventions rather than a provider-specific group, unless the registry adds one.
