# 4. Instrumentation Ownership and Compatibility

Date: 2026-06-05

## Status

Accepted

## Context

`otelc` instruments Go libraries at compile time. The open question
([open-telemetry/opentelemetry-go-compile-instrumentation#500](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/500))
is how many instrumentations we should own and maintain ourselves, and how to
stay aligned with the wider OTel ecosystem instead of drifting into a parallel
set of behaviors.

Owning every instrumentation in this repo does not scale and risks behavioral
drift from the OTel reference implementations (e.g. the `otelhttp`, `otelgrpc`,
etc. packages under `opentelemetry-go-contrib`). At the same time, users should
not see meaningful behavioral differences when instrumenting with `otelc`
versus the reference instrumentation for the same library.

## Decision

Adopt a three-tier ownership model for instrumentations.

- Core (in-repo). Keep a deliberately small, quality-assured set in this
  repository: standard library and the most popular libraries (`grpc`,
  `net/http`, `database/sql`, etc.), with full e2e tests and semantic
  convention alignment.
- Reference (long-term). Rather than re-implementing behavior, support the
  existing instrumentations under
  [`opentelemetry-go-contrib/instrumentation`](https://github.com/open-telemetry/opentelemetry-go-contrib/tree/main/instrumentation)
  by adding `otelc` files and e2e tests there, with our SIG as codeowners.
  Pending a conversation with the Go SDK SIG.
- Ecosystem. Make the rules in
  [`alibaba/loongsuite-go-agent/pkg/rules`](https://github.com/alibaba/loongsuite-go-agent/tree/main/pkg/rules)
  fully `otelc`-compatible, ship `otelc.yaml` files inside those modules, and
  advertise them through the
  [OpenTelemetry Registry](https://opentelemetry.io/ecosystem/registry/).

Key principle: `otelc` does not hold instrumentation logic. It holds only
the wrapper and the rule definition for go-contrib-defined
instrumentations; the actual behavior is delegated upstream to minimize drift.

Versioning: support the last two major versions of each instrumented
library.

## Consequences

- A small core set is realistic to keep at high quality and aligned with
  semantic conventions; broad coverage comes from the ecosystem tier without
  inflating this repo's maintenance burden.
- Delegating to go-contrib reference behavior reduces drift over time, but
  depends on coordination with the Go SDK SIG and on `otelc` files living
  alongside upstream instrumentation.
- Accepted tradeoff: the loongsuite rules are not designed to be reused without
  loongsuite, so they cannot be consumed without `otelc` and cannot be dropped
  into go-contrib as-is. This is acceptable for now.
- The "last two major versions" policy bounds the version matrix we test and
  support per library.
