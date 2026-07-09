# Troubleshooting

## Table of Contents

- ["Nothing Got Instrumented"](#nothing-got-instrumented)
- [Enabling Debug Output](#enabling-debug-output)
- [Inspecting Build Artifacts](#inspecting-build-artifacts)
- [Common Errors](#common-errors)
- [Hook and Instrumentation Limitations](#hook-and-instrumentation-limitations)
- [Getting Help](#getting-help)

This guide covers common problems with `otelc` instrumentation: why instrumentation may not
be applied, how to read debug output, what the build artifacts contain, and the inherent
limitations of compile-time hook injection.

For configuration options and how to scope instrumentation, see
[Configuration and Fine-Tuning](configuration.md). For initial setup, see
[Getting Started](getting-started.md).

## "Nothing Got Instrumented"

When no rule matches any dependency, `otelc` prints to stderr:

```
Warning: no instrumentation will be applied
```

and the build continues without injecting any hooks. This is not a build error.

### Checking what matched

The file `.otelc-build/matched.json` is written after every build and lists every rule that
matched a dependency. An empty array (`[]`) confirms that no rules matched:

```bash
cat .otelc-build/matched.json | jq '.[].Name'
```

### Common causes

**The `target` import path does not match any dependency.** The `target` field must be an
exact import path (or a [glob](rules.md#glob-targets)) that appears in the module graph. Confirm
the package is present and find its exact path:

```bash
go list -m all | grep <library-name>
go list ./... | grep <package-name>
```

**The `version` range excludes the installed version.** The version range in your rule must
include the version `go.mod` resolves to. Check the resolved version:

```bash
go list -m <module-path>
```

**A `--rules` or `OTELC_RULES` override replaced the matching rules.** When either flag is
set, `otelc` uses only those rules. The embedded bundle and any `otel.instrumentation.go`
declarations are ignored. Remove the override or add your custom rules to the specified file.

**The `otel.instrumentation.go` file declares packages with no matching rules.** When a tool
file is present, `otelc` loads only the rules from the declared instrumentation packages and
ignores the embedded bundle. If those packages contain no `*.otelc.yml` files, the matched
set is empty. See [External Configuration Sources](external-configuration.md).

### Instrumented but no spans appear

This is a different problem. Since `otelc` injects the OpenTelemetry SDK initialization
automatically, instrumented binaries emit telemetry by default. If spans do not appear in
your backend, the issue is typically exporter configuration, not instrumentation:

- Verify the exporter endpoint: `OTEL_EXPORTER_OTLP_ENDPOINT`
- Verify the exporter protocol: `OTEL_EXPORTER_OTLP_PROTOCOL` (default: `grpc`)
- Check sampler settings: `OTEL_TRACES_SAMPLER` (default: `parentbased_always_on`)

The [OTel SDK environment variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/)
specification documents all available knobs.

## Enabling Debug Output

### `--debug` flag and `OTELC_DEBUG`

Run `otelc` with `--debug` (or set `OTELC_DEBUG=1`) to enable verbose logging. The flag is
equivalent to the environment variable:

```bash
otelc --debug go build .
# or
OTELC_DEBUG=1 otelc go build .
```

Debug output goes to `.otelc-build/debug.log`. The file is opened in append mode, so
entries accumulate across runs. Delete it between runs if you want a clean log.

### `--stats`

`--stats` (or `OTELC_STATS=1`) prints per-command wall-clock durations for each
`-toolexec` invocation. Use it to identify which compile step is slow:

```bash
otelc --stats go build .
```

### `otelc version --verbose`

Prints the tool version, build commit, and build time:

```bash
otelc version --verbose
```

Use this to confirm the installed version before filing a bug report.

## Inspecting Build Artifacts

`otelc` writes working files to `.otelc-build/` and keeps them after the build completes.
Run `otelc cleanup` to remove them.

| File | Contents |
| --- | --- |
| `debug.log` | Full build log, appended each run (not truncated). |
| `matched.json` | Rules that matched dependencies; empty array when nothing matched. |
| `debug/main/otelc.runtime.go` | Generated helper file for runtime hooks and file injections. |
| `debug/main/go.mod` | Copy of `go.mod` after `otelc` adds its `replace` directives. |
| `gocache/` | Persistent Go build cache used across `otelc` builds. |
| `added_imports.<pid>.json` | Per-process import tracking used during the link phase. |

`go build -work` is passed internally, so Go's own temporary work directory is also preserved
after the build. The path is printed at the start of the build output as `WORK=...`. Inspect
the sources there to see the exact code that entered the compiler for each package.

## Common Errors

### `no command provided. Only 'go build', 'go install' and 'go test' are supported`

`otelc go` was called without a subcommand, or with a subcommand other than `build`,
`install`, or `test`. Only these three are supported.

### `rule %q has no recognised selector`

A rule in a custom YAML file (loaded via `--rules` or `OTELC_RULES`) has a `where` block
that does not contain any known selector key. Check the rule's `where` block against the
[`where` semantics](rules.md#where-semantics) reference.

### `rule %q has an empty target; target is required`

The `target` field is missing or contains only whitespace. Every rule must declare a non-empty
`target`. Add the import path of the package to instrument.

### Malformed glob in `target`

If `target` contains glob characters (`*`, `?`, `[`, `{`) but is not a valid glob pattern
(for example, an unclosed `[`), the rule is rejected at load time with a descriptive error.
Correct the pattern or use an exact import path. See [Glob targets](rules.md#glob-targets).

### `failed to run build plan`

`otelc` runs a dry build to discover the dependency graph. When this fails, the error message
includes the captured build output. Common causes: the package path passed to `otelc go build`
does not exist, or the module graph has errors. Run `go build` directly first to confirm the
project builds without `otelc`.

### `Bumped go version (X -> Y)` / `Bumped dependency X (A -> B)`

These are warnings, not errors. `otelc` adds its own packages as `replace` directives in
`go.mod`, then runs `go mod tidy`. When tidy raises the `go` directive or updates a
dependency, `otelc` warns you. The build continues. Run `git restore go.mod go.sum` after
`otelc cleanup` if you do not want to keep the changes.

### Package-resolution failures

Errors from `pkgload` (for example, `"failed to resolve package name for ..."` or
`"no packages found for ..."`) mean `otelc` could not load a Go package during setup. These
are fatal. Common causes: the package is not in the module graph, the module is not
downloaded, or the module proxy is unreachable. Run `go mod download` and try again.

## Hook and Instrumentation Limitations

The following constraints apply to all hook implementations. They come from the compile-time
injection model and cannot be worked around.

**Restricted imports.** Hook code is injected into the target library's compile unit, so
it must not introduce circular dependencies. Imports from the target library itself,
OpenTelemetry packages, and the Go standard library are always safe. Additional third-party
packages are possible if declared in the instrumentation module's `go.mod`, but keep them to
a minimum — every extra dependency widens the transitive dependency graph for every user of
the instrumentation. See [Adding a New Instrumentation Hook](instrument-guide.md#limitations).

**Generic functions.** When the target function is generic, `HookContext` APIs that modify
parameters or return values (`SetParam`, `SetReturnVal`) cannot be used. The type parameters
are not available to the injected code.

**Goroutine-local storage scope.** Some instrumentation uses goroutine-local storage (GLS)
to propagate trace context across boundaries that do not pass `context.Context`. GLS state
is scoped to the goroutine that wrote it: spans started in a goroutine spawned from the
instrumented function are not automatically visible to that function's GLS slot.
GLS is an internal mechanism, not a public API. See
[GLS operation notes](../instrumentation/go.opentelemetry.io/otel/README.md)
for the full operational model.

## Getting Help

- [GitHub Discussions](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/discussions) —
  ask questions and discuss usage.
- [GitHub Issues](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues) —
  report bugs or request features.
- [OpenTelemetry Slack](https://cloud-native.slack.com) — `#otel-go-compile-instrumentation`
  channel (join via [slack.cncf.io](https://slack.cncf.io)).
