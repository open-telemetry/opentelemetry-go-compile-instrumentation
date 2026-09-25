# 8. Generate Locally Declared Semantic-Convention Attributes from the Registry

Date: 2026-09-22

## Status

Proposed — draft for discussion. The open questions at the end need maintainer input before
this can move to Accepted.

This covers Tier 2 of #728. ADR 0006 (#745) settles where the telemetry contract files live and
explicitly defers code generation to a later ADR; this is that ADR. Tier 1 (HTTP metrics on
`httpconv`) is tracked separately in #744, and Tier 3 (span attribute tables) depends on
[weaver#819](https://github.com/open-telemetry/weaver/issues/819), so both are out of scope here.

## Context

PR #696 introduced a local Weaver registry under `schemas/otelc/`. Attributes that upstream
defines are referenced with `ref:`; attributes otelc invents are declared with `id:`. The registry
is validated by `make lint-schema` (`weaver registry check`), but nothing reads the Go code: the
registry describes the telemetry, it does not produce or constrain it.

For the attributes otelc declares itself, the same definition therefore exists in several places
that are kept in step by hand:

* **The registry.** Fourteen attributes are declared with `id:` today: five `gen_ai.*`
  (`openai.yaml`, `anthropic.yaml`), six `k8s.*`, one `messaging.kafka.*` and two in `aws.yaml`.
  #728 counted nine when it was written in August, so the set is growing.
* **Go constants.** The `gen_ai.*` constants live in a `semconv/genai.go` file that exists four
  times: in each of the three OpenAI modules (96 lines each, the v2 and v3 copies byte-identical)
  and in the Anthropic module (101 lines, drifted). #728 also notes inconsistent typing across
  packages, such as `K8SObjectUID` as a bare string against `GenAIUsageTotalTokensKey` as an
  `attribute.Key`.
* **Tests.** Test files under `instrumentation/` and `test/` contain 793 string literals beginning
  with `"gen_ai.`, written out rather than taken from the constants above.

Nothing fails when these disagree. Removing a declaration from the registry, adding an attribute
in Go without declaring it, or renaming a constant while a test keeps the old literal all leave CI
green.

There is a second failure mode that appears only on a semantic-conventions upgrade. The registry
pins core semantic conventions `v1.37.0`, in both `.semconv-version` and
`registry_manifest.yaml`. `gen_ai.usage.cache_read.input_tokens` is not defined in `v1.37.0`, so
declaring it locally with `id:` is correct today. It is defined upstream from `v1.40.0`. After a
bump to `v1.40.0` or later, the local declaration becomes a second definition of an upstream
attribute, and nothing reports it. #728 anticipates exactly this ("a one-line `id:` to `ref:`
swap"), but relies on someone remembering to make it.

## Decision

Generate Go constants for locally declared attributes from the registry, and make that generated
code the only place those names are written down in Go.

* **Source.** Generate with `weaver registry generate` from `id:` declarations only. `ref:`
  entries are never generated: upstream already ships a symbol for them, and generating another
  would create a competing definition of the same attribute.
* **Output.** One generated package in the shared `pkg/` module, which every instrumentation
  module already depends on. Generated files are named `zz_generated_*.go` and carry the standard
  `// Code generated ... DO NOT EDIT.` header. Templates live under `schemas/otelc/templates/`.
* **Shape.** Every generated attribute key is an `attribute.Key`, so all callers use one type.
  Attributes whose registry type is `template[...]` generate a key-builder function rather than a
  constant, since the member names are only known at runtime (#705).
* **Adoption.** Instrumentations and their tests import the generated package. As each module
  migrates, its hand-written `semconv/genai.go` copy and the string literals in its tests are
  removed. `gen_ai.*` migrates first because it is duplicated four times; `k8s`, `messaging.kafka`
  and `aws` follow.
* **Enforcement.** A new `make generate` target regenerates the package, and CI runs it followed
  by `git diff --exit-code`, so a registry change that is not reflected in Go fails the build.
* **Graduation check.** CI fails when an attribute declared locally with `id:` also exists in the
  pinned upstream registry version. This turns the `id:` to `ref:` swap from something a
  maintainer has to remember into a failing check on the semconv upgrade PR itself.

## Consequences

* A registry change and its Go counterpart can no longer drift silently: the clean-tree check
  fails until the generated file is updated, and removing a declared attribute is a compile error
  at every call site.
* The four `semconv/genai.go` copies collapse into one generated file. Adding an instrumentation
  or a new major-version module no longer adds another copy.
* Tests assert against the same constants the instrumentation emits, so a rename cannot leave a
  stale literal behind.
* Contributors need Weaver locally to run `make generate`. `make weaver-install` already exists
  and the Makefile pins `otel/weaver:v0.19.0`; the contribution guide needs a short section on it.
* The graduation check makes a semconv upgrade PR fail until the affected attributes are switched
  to `ref:`. That is intended, but it means an upgrade can no longer be a one-line version bump.
* Migrating all 793 test literals is mechanical but large. Doing it per module keeps each PR
  reviewable, at the cost of the old and new styles coexisting for a while.
* Several locally declared `gen_ai.*` attributes sit in a namespace otelc does not own. Generating
  them is not a reason to stop proposing them upstream; when one graduates, the graduation check
  surfaces it.

## Open questions

These are the decisions this ADR cannot make on its own.

1. **Which registry is the GenAI dependency?** `registry_manifest.yaml` depends on core semantic
   conventions `v1.37.0`, while the project plan in #1370 names
   `open-telemetry/semantic-conventions-genai` as the source of truth. ADR 0006 notes that Weaver
   cannot merge multiple independent registries. Should the GenAI groups depend on core semconv,
   on the GenAI registry, or should the core dependency be bumped to a version that already carries
   the needed GenAI attributes?
2. **Package location and name.** A single `pkg/semconv` package for all domains, or one package
   per domain (`pkg/semconv/genai`, `pkg/semconv/k8s`) to mirror upstream's `*conv` layout?
3. **Scope of the first PR.** Generator plus the `gen_ai.*` migration together, so the generator
   lands with a real consumer, or the generator alone first?
4. **Test literals.** Migrate the test literals in the same PR as each module's constants, or
   leave them for a separate mechanical PR?
5. **`gen_ai.usage.total_tokens`.** #728 asks whether to keep it, since it is derivable from input
   plus output tokens and upstream may have left it out deliberately. Keeping it means generating
   it; dropping it is a user-visible change.
6. **Graduation check strictness.** Fail the build, or warn, when a local `id:` appears upstream?
   Failing is safer but blocks the upgrade PR until the swap is done.
