# 7. Migrate Instrumentations to opentelemetry-go-compile-contrib

Date: 2026-09-24

## Status

Proposed

## Context

`otelc` currently vendors every first-party instrumentation and most of
`pkg/` in this repository. New library support requires an `otelc`
release. The in-repo `otelc-bundle.tgz` copies those trees into the
tool, so almost every instrumentation change also rewrites the bundle.

[ADR-0004](0004-instrumentation-ownership-and-compatibility.md) kept a
small core set in this repository and pointed broader coverage at the
OpenTelemetry Registry and `opentelemetry-go-contrib`. That placement
does not scale: this repo still owns the growing `instrumentation/`
tree, and discovery still depends on a bundle rather than
[registry-based lookup](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/849).

[ADR-0005](0005-import-driven-instrumentation-selection.md) already
treats instrumentations as ordinary Go modules selected by import. That
model works only if those modules can be published and versioned
outside the `otelc` binary.

The SIG agreed to move the instrumentations into a sibling repository.
The work is tracked in
[open-telemetry/opentelemetry-go-compile-instrumentation#1260](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1260).

## Decision

Host first-party instrumentations and supporting runtime packages in
[`open-telemetry/opentelemetry-go-compile-contrib`](https://github.com/open-telemetry/opentelemetry-go-compile-contrib).
This repository keeps the compile-time tool and the hook API.

**What moves.** The `instrumentation/` tree and `pkg/` **except**
`pkg/hook`. `pkg/hook` stays here: it is the stability boundary
injected into every hook (`HookContext`).

**Module paths.** Align with `opentelemetry-go-contrib`. Import paths
become
`go.opentelemetry.io/otelc-contrib/instrumentation/<module-path>/otelc<pkg>`
(for example
`go.opentelemetry.io/otelc-contrib/instrumentation/go.mongodb.org/mongo-driver/mongo/otelcmongo`).

**Module layout.** Remove the top-level `instrumentation/go.mod`. Shared
packages (`net/http`, `kafka`, `grpc`, and similar) each get their own
`go.mod` so one instrumentation does not leak dependencies into the
others.

**Freeze.** Once the trees are copied, freeze pull requests that modify
`instrumentation/` or `pkg/` in this repository, except `pkg/hook`.
Maintainers choose pre-freeze exceptions. Land critical bug fixes here
before the move; hold non-critical work for contrib. Authors of open
pull requests move them to the contrib repository.

**Consumption.** After the first contrib release, `otelc` requires the
published modules (a temporary hardcoded manifest until the Ecosystem
Explorer registry is available). Then drop the local bundle `replace`
logic that unpacks `instrumentation/` and `pkg/` from
`otelc-bundle.tgz`.

This amends the "core lives in this repository" placement in ADR-0004
and the "`pkg/` holds all runtime packages" layout in
[ADR-0002](0002-api-design-and-project-structure.md). The three-tier
ownership model in ADR-0004 and the hook model in ADR-0002 stay.

## Consequences

- Instrumentations can be reviewed and released without shipping a new
  `otelc` binary, which is the path to registry discovery without a
  long-lived in-repo manifest.
- This repository shrinks to the tool plus `pkg/hook`. The bundle no
  longer packages instrumentation sources after the first contrib
  release.
- Contributors work across two repositories. Open instrumentation pull
  requests must move when the freeze starts.
- Until contrib is released and `otelc` consumes it, the tool still
  uses the local bundle. That window is deliberate and ends with
  [opentelemetry-go-compile-contrib#7](https://github.com/open-telemetry/opentelemetry-go-compile-contrib/issues/7).
- Import paths change. Call sites and
  [ADR-0005](0005-import-driven-instrumentation-selection.md) tool files
  will use `go.opentelemetry.io/otelc-contrib/...` after the move.
