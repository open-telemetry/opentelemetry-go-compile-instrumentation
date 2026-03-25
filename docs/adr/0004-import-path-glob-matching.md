# 4. Target Glob Matching

Date: 2026-03-19 (revised 2026-03-25)

## Status

Accepted (implementation deferred to a follow-on PR replacing the now-closed
[#382](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/382))

## Context

`matchDeps` pre-indexes rules by exact `target` import path. This is O(1) per dependency
and correct for all current rules. Some instrumentation use cases need to match a family of
packages — for example, all packages under `github.com/DataDog/dd-trace-go/contrib/...`.

An earlier design placed this matching in the `where` clause as an `import_path` filter
predicate. Reviewer feedback identified this as unnecessarily indirect: the `target` field
already identifies the package scope, so extending `target` to support glob patterns is
simpler and more natural.

Go's `path.Match` does not support `**` globstar — only `*` within a single path segment.
The `doublestar` library provides this but adds an external dependency for a single
function.

## Decision

Extend the `target` field to support glob patterns directly. A rule author writes:

```yaml
instrument-handler:
  target: "github.com/myapp/internal/*"
  func: Handler
  before: BeforeHandler
```

`matchDeps` detects glob patterns by checking `strings.ContainsAny(target, "*?[")` and
routes rules into two sets:

- `exactRules` — indexed by exact target (current behaviour, O(1) lookup)
- `globRules` — evaluated linearly against all dependencies

Glob rules are expected to be rare (custom user rules, not the default rule set). The
linear scan is acceptable at the anticipated scale. A small custom glob matcher supporting
`*` (single segment) and `**` (any segments) is implemented without external dependencies.

The `where: {import_path: ...}` filter predicate is dropped. It is replaced entirely by
glob support in `target`. PR [#382](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/382)
is closed; a new PR will implement this decision.

## Consequences

- Rules that previously required a `where: {import_path: ...}` predicate now express the
  same intent via `target: "glob/pattern/**"`. The YAML surface is smaller and more
  consistent.
- `matchDeps` requires a small refactor to split the rule index into `exactRules` and
  `globRules` before the dependency loop.
- Benchmarks should verify that the glob scan overhead is negligible for typical rule
  counts (expected: tens of glob rules at most).
- The `PackageMayMatch`-equivalent short-circuit from Orchestrion is preserved: glob
  matching still happens at the package level, before any source files are parsed.
