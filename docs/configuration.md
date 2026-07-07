# Configuration and Fine-Tuning

This guide covers how to scope, filter, and tune `otelc` instrumentation for your project. It
assumes you have already completed the [Getting Started](getting-started.md) setup. For the full
rule schema reference, see [Instrumentation Rules](rules.md).

## Selecting Instrumentations

By default, `otelc` applies all instrumentation rules from its embedded bundle to every
dependency it finds in your module graph. This zero-configuration mode works well for
getting started: run `otelc go build` and all supported libraries are instrumented
automatically.

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

Rules select packages and locations within those packages. The following fields let you scope
instrumentation precisely.

### Targeting packages

The `target` field selects the package to instrument by its import path:

```yaml
# Exact match — instruments only this package
instrument_http_client:
  target: net/http
  where:
    func: NewRequest
  do:
    - inject_hooks:
        before: Before
        path: example.com/myapp/hooks/http
```

When a single rule needs to cover a family of packages, `target` accepts glob syntax. A target
is treated as a glob when it contains `*`, `?`, `[`, or `{`. See
[Glob targets](rules.md#glob-targets) in the rules reference for the full pattern grammar.

**When to use globs:** A glob target is useful when a library ships multiple packages with
the same instrumentation contract — for example, `database/sql/driver*` to instrument every
driver-side package, or `google.golang.org/grpc*` to cover all gRPC sub-packages with one
rule.

### Filtering by version

The `version` field restricts a rule to a specific range of the target library, using the
format from [Top-level fields](rules.md#top-level-fields):

```yaml
instrument_v2_only:
  target: example.com/mylib
  version: "v2.0.0,v3.0.0"  # [v2.0.0, v3.0.0)
  where:
    func: NewClient
  do:
    - inject_hooks:
        before: Before
        path: example.com/myapp/hooks
```

Omitting `version` (or setting it to `""`) matches all versions.

### Filtering within a package

The `where.file` block narrows which source files and declarations a rule applies to. All
available predicates are documented in [`where.file` semantics](rules.md#wherefile-semantics);
this section covers *when* to reach for each one.

**`has_func` / `has_struct`** — scope a rule to files that declare a particular function or
struct. Use this when you need to instrument a function that appears under the same name in
multiple files and you only want one of them:

```yaml
instrument_driver_connect:
  target: database/sql/driver
  where:
    func: Connect
    file:
      has_struct: Conn
  do:
    - inject_hooks:
        before: Before
        path: example.com/myapp/hooks/sql
```

**`has_package`** — matches the declared `package` clause (the `package foo` line), not
the import path. Its main use is with glob targets that span multiple compiles: a glob like
`example.com/foo*` matches both `example.com/foo` (package `foo`) and
`example.com/foo_test` (package `foo_test`). Adding `has_package: foo` ensures the rule
only applies to the production package:

```yaml
instrument_main_package_only:
  target: example.com/foo*
  where:
    func: Handle
    file:
      has_package: foo
  do:
    - inject_hooks:
        before: Before
        path: example.com/myapp/hooks
```

**`is_test`** — gates on whether the compile is a test build (`otelc go test`). A plain
`otelc go build` never produces test builds, so this predicate has no effect there. Use it
to apply different instrumentation to test code than to production code, or to explicitly
exclude test builds:

```yaml
instrument_production_only:
  target: example.com/myservice
  where:
    func: HandleRequest
    file:
      is_test: false
  do:
    - inject_hooks:
        before: Before
        path: example.com/myapp/hooks
```

**Combinators** — `all-of`, `one-of`, and `not` compose multiple predicates. See
[Combining `where.file` predicates](rules.md#combining-wherefile-predicates) for examples.

**Planned:** A `target: root` shorthand is planned (#629) to select the module being built
without specifying its import path. Source-level opt-out pragmas (`//otelc:ignore`) are also
planned (#469).

## Runtime Tuning

`otelc` injects an OpenTelemetry SDK initialization package into instrumented binaries. The
SDK reads standard OTel environment variables at startup. There is no `otelc`-specific
configuration for exporters, samplers, or resource attributes — set those through the
[OTel SDK environment variable specification](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/).

One `otelc`-specific runtime knob is `OTEL_GLS_MAX_SPANS`, which controls the depth of the
goroutine-local storage (GLS) span stack. Instrumentation that does not pass `context.Context`
through all call boundaries relies on GLS to propagate trace context. Increasing
`OTEL_GLS_MAX_SPANS` beyond the default accommodates deeper call stacks; see
[GLS operation notes](../instrumentation/go.opentelemetry.io/otel/hook/gls-operation.md) for
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
