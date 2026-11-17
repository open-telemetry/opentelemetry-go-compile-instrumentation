# 6. Semconv Telemetry Contract Organization

Date: 2026-10-21

## Status

Proposed

Folds in the outcome of the Tier 2 RFC for
[#728](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/728),
["Generating otelc's Locally Declared Semantic Convention Attributes"](https://docs.google.com/document/d/1ssjfd9caO1op1yeq1a_oxnOt4Gep2j0QspcSrNbvzZc/edit),
as agreed in
[#745](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/745).
That issue originally left code generation to a later ADR; it is included here
because the generation design decides where local declarations live.

## Context

[PR #696](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/696)
added an OpenTelemetry Weaver registry under `schemas/otelc/`. It is the
machine-readable contract of the telemetry `otelc`'s instrumentations
emit: attributes that exist upstream are referenced with `ref:`, and
anything upstream does not cover is declared locally with `id:`.
`make lint-schema` validates it with `weaver registry check --future`
against the upstream version pinned in `.semconv-version`.

That PR established the registry but not the rules for growing it, and
nothing in Go is derived from it. The Go constants for the same
attributes are written by hand in each module (`semconv/genai.go` exists
four times), and tests assert on string literals. The contract and the
code can drift apart without any check failing.

Weaver v0.19.0, the version `otelc` pins, constrains the layout:

- one `registry_manifest.yaml` per registry, with a single dependency
  ("Currently, Weaver supports only a single dependency per registry");
- one directory tree per registry, scanned recursively;
- no validating one registry against several upstream versions at once;
- after resolution, a `ref:` attribute carries the same fields as an
  `id:` attribute, so a template cannot tell a local declaration from an
  upstream one by looking at the attribute alone.

[ADR-0004](0004-instrumentation-ownership-and-compatibility.md) sets the
three-tier ownership model and requires semantic convention alignment for
the in-repo core tier,
[ADR-0005](0005-import-driven-instrumentation-selection.md) treats
instrumentations as ordinary Go modules, and
[ADR-0007](0007-migrate-instrumentations-to-contrib.md) moves
`instrumentation/` and `pkg/` (except `pkg/hook`) to
`opentelemetry-go-compile-contrib`. None of them says where the declared
contract lives or how the Go code relates to it.

## Decision

**One contract, organised by family.** Contract files live under
`schemas/otelc/`, one YAML file per instrumentation family rather than
per module. Today that is 19 files for the 33 modules under
`instrumentation/`: `openai.yaml` covers `openai-go` v1, v2 and v3;
`grpc.yaml` covers client and server; `logs.yaml` covers `log`,
`log/slog`, `logrus` and `zap`. Sub-directories are permitted as the set
grows.

**Local declarations stay in one tree.** Attributes `otelc` declares
itself with `id:` live in `schemas/otelc/groups/`, next to the spans,
metrics and `ref:` attributes that use them. Weaver's resolved output
records provenance: each group names the registry it came from, and each
`ref:` attribute names its upstream source group, while `id:` attributes
have none. Filtering on that selects the 17 local attributes.

That output is one the `--lineage` flag still describes as not
implemented, so a test compares the generated keys with the `id:` entries
parsed from the YAML and fails if the two differ. If the output stops
being reliable, the fallback is a separate `schemas/otelc/local/` registry
between `groups/` and upstream, since Weaver allows one dependency per
registry.

**One upstream version.** A single upstream semconv version is pinned in
`.semconv-version`, currently v1.37.0. The manifest that depends on
upstream declares the matching version, and the `Makefile` guard checks
that manifest. Upgrades are atomic across every instrumentation; there
is no per-instrumentation pin.

**Locally declared attributes are generated.** A new `make generate`
target renders
the attributes declared with `id:` into one Go package, `pkg/otelcconv`, with
the attribute template that `go.opentelemetry.io/otel` uses for its own
semconv packages. The template is vendored with its source version
recorded rather than read from the module cache, so the output does not
depend on which `otel` version resolves. It emits a key per attribute
and, for most attributes, a typed helper, matching upstream. CI runs `make generate` followed
by `git diff --exit-code`. Attributes that exist upstream are imported
from `go.opentelemetry.io/otel/semconv` and never generated.

**Declaring is part of adding.** An instrumentation that emits telemetry
adds its declarations in the same change as the code that emits it.

**The contract moves with the code.** `schemas/otelc/` moves to
`opentelemetry-go-compile-contrib` together with the instrumentations,
so the registry and the package generated from it stay in one
repository and `make generate` has both available.

## Consequences

The registry stays a Weaver-validatable unit, and adding an
instrumentation means adding to a known place. Grouping by family keeps
the file count close to the number of libraries rather than the number
of Go modules, so the three `openai-go` major versions share one
declaration instead of drifting into three.

A local attribute that later appears upstream is already caught:
`make lint-schema` reports `DuplicateAttributeId`. The fix is an `id:`
to `ref:` swap, after checking
that the upstream type matches. `rpc.system.name` is the current case
where it does not: a string locally, an enum upstream.
`docs/semantic-conventions.md` gains a line stating this remedy, since
the diagnostic does not.

Until the move to contrib, `pkg/` is shipped in the bundle and built
from the same commit as its consumers, so removing a generated constant
fails CI in the same change that removes it. Once `pkg/` is published
from contrib, removing a graduated constant from `otelcconv` is a
breaking change, and Go selects one `otelcconv` version per build.
`otelcconv` is therefore released in the same module set as the
instrumentation modules, and a graduated constant is kept for one
release as a deprecated alias to its upstream symbol before it is
removed.

A single pinned version means a semconv bump is one change touching
every instrumentation, reviewable in one place. It also means dual-emit
cannot be expressed in the registry; if it is wanted, it belongs in the
code layer and needs its own decision.

Tests assert on the generated constants rather than string literals,
and a `forbidigo` pattern rejects literal attribute keys in
instrumentation code, so a registry rename fails the build instead of
changing what an assertion means. `golangci-lint` currently runs only on
the root module (which contains `tool/`), so the instrumentation modules have
to be brought under lint before that pattern takes effect.

Two things remain conventions rather than checks. Nothing verifies that
a module emitting telemetry has a declaration, and nothing verifies that
what it emits at runtime matches what it declares. Checking emitted
signals against the registry, for example with
`weaver registry live-check` in the integration tests as raised in #745,
is the natural follow-up and is tracked separately.

Colocating contract files with each module was rejected. Weaver needs
one tree per registry, so colocation would need an aggregation step to
assemble a registry before every validation, adding a build stage and a
new way for the contract to be wrong.
