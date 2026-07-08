# Configuration and Fine-Tuning

This guide covers how to scope, filter, and tune `otelc` instrumentation for your project. It
assumes you have already completed the [Getting Started](getting-started.md) setup. For the full
rule schema reference, see [Instrumentation Rules](rules.md).

## Selecting Instrumentations

By default, `otelc` applies all instrumentation rules from its embedded bundle to every
dependency it finds in your module graph. This zero-configuration mode works well for
getting started: run `otelc go build` and all [supported libraries](getting-started.md#supported-libraries)
are instrumented automatically.

For projects that need tighter control — because they use a narrow set of libraries, because
they ship a library themselves, or because they need reproducible, auditable builds — you can
declare exactly which instrumentations to enable. See [External Configuration Sources](external-configuration.md)
for the `otel.instrumentation.go` mechanism that makes this explicit and source-controlled.

## Rule Sources and Precedence

`otelc` resolves rules from the following sources, in priority order (highest first):

1. **`OTELC_RULES` environment variable** — path to a rule file or comma-separated list of
   paths. When set, all other sources are ignored.
2. **`--rules` flag** — same format as `OTELC_RULES`. Takes effect when `OTELC_RULES` is not
   set.
3. **Tool files** (`otel.instrumentation.go` / `otelc.tool.go`) — when the project declares
   instrumentations explicitly. See [External Configuration Sources](external-configuration.md).
4. **Embedded defaults** — the instrumentation bundle built into `otelc`, applied when none of
   the above are present.

Each source entirely replaces those below it. There is no merging: when `--rules` is provided,
tool files and the embedded bundle are not consulted.

### Using `--rules` for development and debugging

`--rules` loads rules from a file or a directory tree. Paths can be comma-separated to load
from multiple locations:

```bash
# Single file
otelc --rules my-rules.yml go build .

# Directory — all *.otelc.yml and otelc.yml files inside are loaded
otelc --rules custom-rules/ go build .

# Multiple sources
otelc --rules base-rules/,extra.otelc.yml go build .
```

> [!NOTE]
> `--rules` is a global `otelc` flag and must appear **before** the `go` subcommand.
> `otelc go` passes all arguments after `go` directly to the Go toolchain without parsing
> them, so `otelc go build --rules ...` would forward `--rules` to `go build` instead.

The `OTELC_RULES` environment variable accepts the same format and is useful in CI pipelines
where you want to inject rules without modifying the build command:

```bash
OTELC_RULES=ci-rules/ otelc go build .
```

> [!NOTE]
> `--rules` and `OTELC_RULES` are intended for development and debugging, not for production
> configuration. For stable, versioned instrumentation, use the `otel.instrumentation.go`
> mechanism described in [External Configuration Sources](external-configuration.md).

## Narrowing What Gets Instrumented

Rules target packages and locations within them using three fields — the full schema is in
[Instrumentation Rules](rules.md). This section covers when to reach for each one.

- **`target`** — the package import path. Use an exact path for a single package, a
  [glob](rules.md#glob-targets) (`google.golang.org/grpc*`) to cover a package family, or
  `$root` to match your own module without hardcoding its path. See
  [Special `target` values](rules.md#special-target-values).
- **`version`** — restricts the rule to a [version range](rules.md#top-level-fields).
  Omit to match all versions.
- **`where.file`** predicates narrow to specific source files:
  - `has_func` / `has_struct` — when the same function name appears in multiple files and
    only one should be hooked.
  - `has_package` — use alongside a glob target to distinguish `foo` from `foo_test` within
    the same match.
  - `is_test` — restrict a rule to test builds (`otelc go test`) or exclude them. Has no
    effect under `otelc go build`.
  - `all-of`, `one-of`, `not` — compose predicates. See
    [`where.file` semantics](rules.md#wherefile-semantics).

**Planned:** Source-level opt-out pragmas (`//otelc:ignore`) are planned (#469).

## Runtime Tuning

`otelc` injects an OpenTelemetry SDK initialization package into instrumented binaries. The
SDK reads standard OTel environment variables at startup. There is no `otelc`-specific
configuration for exporters, samplers, or resource attributes — set those through the
[OTel SDK environment variable specification](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/).

One `otelc`-specific runtime knob is `OTEL_GLS_MAX_SPANS`, which controls the depth of the
goroutine-local storage (GLS) span stack. Instrumentation that does not pass `context.Context`
through all call boundaries relies on GLS to propagate trace context. Increasing
`OTEL_GLS_MAX_SPANS` beyond the default accommodates deeper call stacks; see
[GLS operation notes](../instrumentation/go.opentelemetry.io/otel/README.md) for
the operational constraints.

## Verifying Your Configuration

After a build, the file `.otelc-build/matched.json` lists every rule that matched a dependency
and the locations it was applied. Inspect it to confirm that the instrumentations you expect
are active:

```bash
cat .otelc-build/matched.json | jq '.[].Name'
```

If instrumentation is not applied, `otelc` prints a warning to stderr:

```
Warning: no instrumentation will be applied
```

Common causes:

- The `target` import path does not match any dependency in the module graph (check with
  `go list -m all`).
- The `version` range excludes the version actually in use.
- A `--rules` or `OTELC_RULES` override replaced the rules that would have matched.
- The project uses `otel.instrumentation.go` but the declared packages have no matching rules.

For a structured diagnosis workflow, see [Troubleshooting](troubleshooting.md).

The `.otelc-build/` directory is retained after every build and removed only when you run
`otelc cleanup`. It also contains `debug.log`, `debug/main/otelc.runtime.go`, and other
artifacts described in [Inspecting Build Artifacts](troubleshooting.md#inspecting-build-artifacts).

## See Also

- [Instrumentation Rules](rules.md) — full rule schema reference, including all `where.file`
  predicates, target glob grammar, and rule type definitions.
- [External Configuration Sources](external-configuration.md) — declare instrumentations
  explicitly via `otel.instrumentation.go` for reproducible, auditable builds.
- [Troubleshooting](troubleshooting.md) — diagnose why instrumentation was not applied.
