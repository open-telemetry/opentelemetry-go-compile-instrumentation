# Troubleshooting

## Table of Contents

- ["Nothing Got Instrumented"](#nothing-got-instrumented)
- [Enabling Debug Output](#enabling-debug-output)
- [Inspecting Build Artifacts](#inspecting-build-artifacts)
- [Common Errors](#common-errors)
- [Duplicate Spans with `otelhttp`, `otelgin` and `otelgrpc`](#duplicate-spans-with-otelhttp-otelgin-and-otelgrpc)
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
- Verify the exporter protocol: `OTEL_EXPORTER_OTLP_PROTOCOL` (default: `http/protobuf`)
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

### Rule matching decisions

With `--debug` (or `OTELC_DEBUG=1`), the real matcher also writes experimental
JSON Lines diagnostics under `.otelc-build/debug/<escaped-package>/`:

- `setup.matches.jsonl`: target/version and per-source AST decisions during setup.
- `<source-base>.matches.jsonl`: decisions when toolexec applies rules to that source.

For example, a function exists but its argument types differ from the rule:

```json
{"package":"example.com/foo","rule":"client_hook","phase":"setup","source":"/src/foo.go","matched":false,"reason":"signature_mismatch"}
```

Records describe individual decisions, not a package-wide verdict. A rule can fail
in one file and match another. An omitted `source` means a package-level decision.
`matched: true` omits `reason`; in setup it means eligibility, not an applied change.
Call and literal rules are eligible in every file passing `where`; their join points
are resolved during instrumentation. Use the `.diff` reports to inspect changes.

The initial closed set of rejection codes is:

| Code | Decision |
| --- | --- |
| `version_mismatch` | The resolved version failed the rule's range (including unresolved versions). |
| `no_source_files` | There were no files for precise matching. |
| `where_mismatch` | The real `where` predicate tree returned false for this file. |
| `function_not_found` | No declaration had the requested function name. |
| `receiver_mismatch` | The name existed, but no receiver matched. |
| `signature_mismatch` | A signature, parameter, or result filter failed, including the `*_implements` filters. |
| `struct_not_found` | The requested struct was absent. |
| `directive_not_found` | The requested directive was absent. |
| `declaration_not_found` | No named declaration of the requested kind existed. |
| `ignored` | An `otelc:ignore` directive suppressed instrumentation, of the file, the function, or every matching call site. |
| `no_joinpoint` | A call, literal, or directive rule found nothing to instrument in this file. |

A rule whose target (exact, glob, list, or `$root`) does not select a package is
never a candidate for it and produces no record, so the stream lists only rules
that could have applied. Duplicate glob candidates are also omitted. Parse, predicate
construction, and transformation errors remain build errors. Predicate failures
report the whole `where` clause, including its combinators, rather than inventing
a single failing leaf. A file rule whose added file is excluded by its build
constraints produces no record: that is decided when the file is added, not by
the matcher. These diagnostics do not alter `matched.json` or build IDs.
Debug setup clears old artifacts; direct toolexec runs append. Record order across
setup workers is unspecified. Go may reuse cached packages without running toolexec;
use `go build -a` when you need fresh instrumentation decisions. The format is
experimental and may change.

### Instrumentation Diffs

When running with `--debug` (or `OTELC_DEBUG=1`), `otelc` writes unified `.diff` reports
showing all source modifications and source additions introduced into the Go compiler:

- **Output paths**: Diffs are written under `.otelc-build/debug/<escaped-package>/`,
  where `<escaped-package>` is derived from the package/import path (the `-p` flag passed
  to the compiler, or `pkg.PkgPath` during setup) with slashes and dots replaced by underscores
  (e.g. `github_com_redis_go-redis_v9` or `example_com_app_cmd_client`). Both runtime helper
  artifacts and compiler phase instrumentation reports use this package-scoped path, so packages
  with identical directory basenames (such as `cmd/client` and `internal/client`) stay separate.
  Slashes and dots are both replaced with underscores, so unusual import paths that collapse to the
  same escaped name can still share a directory.
- **Modified source files**: Reports contain labeled sections for each applied rule in
  application order (`=== rule X/N: <name> ===`), followed by an authoritative full diff
  (`=== full diff: <original> -> <compiled> ===`) that includes any post-processing
  optimizations (such as trampoline jump optimizations).
- **Generated instrumentation files**: Files introduced entirely by instrumentation are
  represented as added-file diffs against `/dev/null`:
  - `otelc.<name>.go.diff`: Added file generated by an `InstFileRule`, attributed to the
    responsible rule (`=== rule: <name> ===`).
  - `otelc.globals.go.diff`: Added file generated for trampoline and hook context globals,
    listing the contributing rules responsible for requiring the globals file.
  - `otelc.runtime.go.diff`: Added file generated during setup to hold runtime imports,
    linkage, and helper declarations required by matched instrumentation rules, listing the
    contributing rules responsible for requiring the runtime file.
- **Stale diff management**:
  - `otelc --debug go build/test/install` automatically resets the `.otelc-build/debug`
    directory at the beginning of each build before setup and compilation begin.
  - When using `otelc` as a direct `-toolexec`/GOFLAGS integration, run `otelc --debug setup`
    immediately before a diagnostic build to reset and prepare the debug artifacts. Direct
    `toolexec` children do not perform global cleanup because Go may invoke them concurrently.
    Repeated direct `go build` invocations without rerunning `otelc --debug setup` are not
    guaranteed to represent a fresh isolated debug report.
- **Go build cache**: The Go toolchain only invokes compiler tools (`-toolexec`) for
  packages that are actually recompiled. If Go serves a package entirely from cache,
  `otelc` does not re-instrument it during that run and no fresh `.diff` is generated.
  To force recompilation and regenerate diffs for all packages, pass `-a` (e.g.
  `otelc --debug go build -a .` or `OTELC_DEBUG=1 go build -a .`) or run `otelc cleanup` to
  remove `otelc`'s retained build directory and cache before rebuilding. (Note that `otelc`
  maintains its own persistent cache at `.otelc-build/gocache`, so a standard `go clean -cache`
  only affects your default external Go cache unless `GOCACHE=.otelc-build/gocache` is set.)

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

| File / Directory | Contents |
| --- | --- |
| `debug.log` | Full build log, appended each run (not truncated). |
| `matched.json` | Rules that matched dependencies; empty array when nothing matched. |
| `debug/<package>/*.diff` | Instrumentation diff reports (per-rule and full diffs, plus `/dev/null` added files). |
| `debug/<package>/*` | Retained copies of instrumented and generated source files sent to the compiler. |
| `debug/<package>/otelc.runtime.go` | Generated helper file for runtime hooks and file declarations for `<package>`. |
| `debug/<package>/otelc.runtime.go.diff` | Setup-generated runtime diff report for `<package>`. |
| `debug/main/go.mod` | Copy of `go.mod` after `otelc` adds its `replace` directives. |
| `debug/<package>/` | Instrumented source files as compiled, one directory per package; `/` and `.` in the import path become `_`. |
| `gocache/` | Persistent Go build cache used across `otelc` builds. |
| `added_imports.<pid>.json` | Per-process import tracking used during the link phase. |

With `--debug`, `go build -work` is also passed, so Go's own temporary work directory is
preserved after the build. The path is printed at the start of the build output as `WORK=...`.
Inspect the sources there to see the exact code that entered the compiler for each package.
Without `--debug`, Go removes the directory as usual.

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

## Duplicate Spans with `otelhttp`, `otelgin` and `otelgrpc`

An application can use wrappers from
[opentelemetry-go-contrib](https://github.com/open-telemetry/opentelemetry-go-contrib) and also
be built with `otelc`. Both layers can then record the same request. The result depends on the
path:

| Path | Result |
| --- | --- |
| `http.Client` with `otelhttp.NewTransport` over `*http.Transport` | One CLIENT span. `otelhttp` owns the span. `otelc` steps aside when the request context carries a live `otelhttp` client span. |
| `net/http` server with `otelhttp.NewHandler`, or gin with `otelgin.Middleware` | Two SERVER spans per request. `otelc` starts its span at the outer `net/http` layer, outside the app handler. The wrapper starts a second SERVER span inside it. |
| gRPC server or client with `otelgrpc` stats handlers | Two SERVER spans or two CLIENT spans per hop. Both are exported. On the client, `otelc`'s span is the parent of `otelgrpc`'s span. On the server, the two spans nest like the HTTP server spans. |

On the server side, for HTTP and gRPC, the two SERVER spans nest differently. Without an
incoming `traceparent`, the wrapper's span is a child of `otelc`'s span. With an incoming
`traceparent` in the HTTP headers or the gRPC metadata, the wrapper extracts it again and starts
its span from the remote parent. The two spans are then siblings.

For the server paths and gRPC, do one of the following to get one span per hop:

1. Remove the contrib wrapper and rely on `otelc` for that path.
2. Disable `otelc` for that protocol with `OTEL_GO_DISABLED_INSTRUMENTATIONS`. The `net/http`
   client and server instrumentation share the key `nethttp`, and gRPC uses `grpc`. Each
   key covers the client and the server side, so it also disables `otelc` for calls that the
   wrapper does not cover. See
   [Runtime selection without rebuilding](configuration.md#runtime-selection-without-rebuilding).

Disabling the `gin` key does not remove a span. The gin instrumentation does not start a span.
It only adds the route and any gin errors to the `net/http` server span.

The `net/http` server hook and the gRPC hooks have no step-aside logic yet.
[Issue #1695](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1695)
tracks the open design question.

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

**Package initialization order.** A hook can run before its own package's variable
initializers and `init()` functions, when the instrumented function is called during
package initialization (for example from a driver's `init()`). The hook package is only
imported from package `main`, so Go does not order it before the target package. A hook
that uses state set up at initialization time panics, and the trampoline prints
`failed to exec Before hook` or `failed to exec After hook` while the build and the tests
still pass. Hooks must create such state on first use. See
[Adding a New Instrumentation Hook](instrument-guide.md#limitations).

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
