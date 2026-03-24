# 4. Import Path Glob Matching

Date: 2026-03-19

## Status

Accepted (implementation deferred to [#382](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/382))

## Context

`matchDeps` pre-indexes rules by exact target import path. This is O(1) per dependency and
correct for all current rules. Adding an `import_path` filter predicate (e.g.,
`github.com/DataDog/**`) requires matching against patterns that cannot be pre-indexed.

Go's `path.Match` does not support `**` globstar — only `*` within a single path segment.
The `doublestar` library provides this but adds an external dependency for a single function.

## Decision

Implement a small custom glob matcher supporting `*` (single segment) and `**` (any
segments). No external dependency is introduced.

`matchDeps` will separate rules into two sets:

- `exactRules` — indexed by exact target (current behaviour, O(1) lookup)
- `globRules` — evaluated linearly against all dependencies (O(G) per dep, where G is the
  number of glob rules)

Glob rules are expected to be rare (custom user rules, not the default rule set). The linear
scan is acceptable at the anticipated scale.

Rules with an `import_path` filter may omit the `target` field entirely; `matchDeps` routes
them through the glob path.

## Consequences

- A rule with both `target` and `where: {import_path: ...}` is an error; the predicates are
  redundant and should be rejected at rule-load time. (Validation is added in
  [#382](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/382).)
- [#382](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/382) adds a package-level import-path check before `ast.ParseFileFast`, recovering the
  Orchestrion-equivalent `PackageMayMatch` short-circuit for glob rules.
- Benchmarks should verify that the glob scan overhead is negligible for typical rule counts.
