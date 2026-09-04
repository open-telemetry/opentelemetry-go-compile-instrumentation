# 6. Semconv Telemetry Contract Organization

Date: 2026-07-22

## Status

Proposed

## Context

PR [#696](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/696)
added a project-owned OTel Weaver registry under `schemas/otelc/` so this
repository can validate the telemetry contract its compile-time
instrumentations emit.

That registry already depends on a single upstream semantic-conventions
version, pinned in `.semconv-version` and mirrored in
`schemas/otelc/registry_manifest.yaml`. CI guards also require the Go
`semconv/vX.Y.Z` imports to match the same version.

The registry introduces two related but distinct forms of conformance:

- declared-contract validity: `make lint-schema` verifies that the local
  registry resolves against the pinned upstream semantic conventions;
- runtime-emission conformance: instrumentation tests verify that telemetry
  emitted at runtime matches the declared contract.

Static Weaver validation does not execute instrumentation or observe exported
telemetry. Runtime verification therefore remains necessary, and its coverage
is limited to the execution paths exercised by tests.

Follow-up work tracked in
[#728](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/728)
needs a stable answer to two questions before Tier 2/3 registry-driven code
generation can proceed:

- where telemetry contract files live;
- how semantic-convention versioning is managed across the registry.

The current Weaver toolchain imposes hard constraints that shape the answer:

- one `registry_manifest.yaml` per registry;
- one directory tree of group files, scanned recursively;
- no support for merging multiple independent registries at validation time;
- no support for validating the same registry against multiple upstream
  semantic-convention versions simultaneously.

These constraints make a centralized registry organization the only viable
default without introducing project-specific aggregation tooling.

## Decision

Adopt a centralized organization for otelc's telemetry contract registry.

- The registry is the authoritative declaration of telemetry emitted by
  in-repository instrumentations.
- All telemetry contract files live under `schemas/otelc/groups/`.
- The unit of ownership is an instrumentation family, represented by one YAML
  file. A family may cover multiple modules when they describe one shared
  telemetry surface, such as `http.yaml`, `logs.yaml`, or `otel-sdk.yaml`.
- Subdirectories under `schemas/otelc/groups/` are allowed later for logical
  grouping, but all files remain part of the same registry tree.
- The project keeps a single global upstream semantic-conventions version in
  `.semconv-version`.
- Changes to `.semconv-version`,
  `schemas/otelc/registry_manifest.yaml`, and Go `semconv/vX.Y.Z` imports are
  atomic: they are bumped together in the same change.
- New instrumentations that emit telemetry must add or update a corresponding
  group file declaring that emitted telemetry.
- Instrumentations that emit no telemetry of their own still require a
  contract file with `groups: []` and an explanation of why the instrumentation
  is contract-neutral.
- A metric name may be declared only once in the local otelc registry. If two
  instrumentations emit the same standard metric, that metric must be owned by
  one shared family contract instead of being duplicated across multiple local
  group files.
- Instrumentation tests must verify that emitted telemetry conforms to the
  registry across each materially distinct emission path, including applicable
  conditional and error paths.
- Static registry validation and runtime-emission verification are
  complementary requirements. The runtime enforcement mechanism, such as
  Weaver live-check, generated assertions, or registry-aware test helpers, is
  deferred to follow-up work.
- Code generation from the registry is explicitly deferred to follow-up work;
  this ADR defines the registry layout, versioning rules, and verification
  boundary only.

## Consequences

- The registry has one stable home and one validation model, which keeps
  Weaver-based checks simple and predictable.
- Semantic-convention upgrades become explicit, atomic reviews instead of
  independent per-instrumentation version drift.
- Contributors have a clear rule: every telemetry-emitting instrumentation must
  be represented in `schemas/otelc/groups/`.
- `make lint-schema` proves that the declared registry is internally valid, but
  it does not prove that runtime telemetry conforms to that declaration.
- Runtime verification is only as complete as the scenarios exercised by the
  tests. Follow-up enforcement must account for path-sensitive telemetry,
  including attributes emitted only conditionally or on error paths.
- Registry files are not colocated with the instrumentations they describe,
  which trades local proximity for a simpler, tool-compatible registry.
- Migration windows that need dual-emit or validation against multiple
  semantic-convention versions cannot be modeled in the registry today and
  must be handled in code and tests until a later ADR defines a different
  approach.
- Future Tier 2/3 work may build generators or validators on top of this
  centralized registry, but it does not need to re-decide where the contract
  lives.
