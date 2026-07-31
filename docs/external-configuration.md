# Instrumentation Configuration Sources

This document describes the `otel.instrumentation.go` mechanism for declaring which
instrumentation packages a project uses. It covers the tool file format, how per-package rule
files are discovered, the full resolution protocol, and the rule-source precedence model.

For a quick introduction, see [Managing Instrumentations](getting-started.md#managing-instrumentations)
in the Getting Started guide and [Loading Rules](rules.md#loading-rules) in the rules
reference. This document is the authoritative protocol specification.

## Table of Contents

- [Overview](#overview)
- [The Tool File](#the-tool-file)
  - [Naming](#naming)
  - [Format](#format)
  - [Module scope](#module-scope)
- [Per-Package Rule Files](#per-package-rule-files)
- [Discovery and Resolution Protocol](#discovery-and-resolution-protocol)
- [Rule Source Precedence](#rule-source-precedence)
- [Errors and Diagnostics](#errors-and-diagnostics)
- [Composing Instrumentation Packages](#composing-instrumentation-packages)
- [See Also](#see-also)

## Overview

By default, `otelc` instruments every dependency it finds using its embedded rule bundle.
This all-or-nothing model works for getting started, but it does not give you reproducible
builds: adding a new version of `otelc` may silently change which libraries get instrumented.

The tool file mechanism gives you explicit, source-controlled control. You declare which
instrumentation packages to enable using the standard Go `tools.go` pattern. `otelc` reads
the imports in that file, resolves each one as a Go package, and loads the rule files found
there.

This approach mirrors how [DataDog Orchestrion](https://github.com/DataDog/orchestrion)
manages its instrumentation configuration with `orchestrion.tool.go`, and it realizes the
vendor-agnostic design described in [#567](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/567).

Any third-party package can ship instrumentation by including `*.otelc.yml` rule files. A
single blank import in your tool file activates it.

## The Tool File

### Naming

`otelc` recognizes two filenames, both treated identically:

| Filename | Role |
| --- | --- |
| `otel.instrumentation.go` | Canonical name |
| `otelc.tool.go` | Compatibility alias |

Use `otel.instrumentation.go`. The alias is accepted for compatibility with the issue tracker
and early documentation, but the canonical name is preferred.

Having both files in the same module is an error. See [Errors and Diagnostics](#errors-and-diagnostics).

### Format

The tool file follows the standard Go `tools.go` pattern:

```go
//go:build tools

package tools

import (
    _ "go.opentelemetry.io/otelc/instrumentation/net/http/server"
    _ "go.opentelemetry.io/otelc/instrumentation/google.golang.org/grpc"
)
```

Rules:

- The `//go:build tools` constraint is **required** so the file never compiles into the
  binary. `go mod` still tracks the imports as real dependencies.
- The package name must be `tools` (or any valid identifier — `package tools` is the
  convention).
- **Every import must be a blank import** (`_ "path"`). Named imports and dot imports are
  rejected at load time.

`otelc pin` can create or update the tool file automatically, discover applicable
instrumentation packages, and run `go mod tidy`.

After creating or editing the tool file manually, run `go mod tidy` to record the new
dependencies in `go.mod` and `go.sum`.

For current limitations around committing generated instrumentation files, see
[Managing Instrumentations](getting-started.md#managing-instrumentations).

### Module scope

> [!NOTE]
> Tool files are module-scoped, not package-scoped. They must be placed next to the module's
> `go.mod` file. For example, if an instrumentation package has import path
> `github.com/example/foo/bar` but the module root is `github.com/example/foo`, the tool
> file must live in `github.com/example/foo`.

For multi-module Go workspaces built in a single `otelc` invocation, `otelc` discovers tool
files in every module root that is part of the build. The rule sets from all discovered tool
files are unioned together.

## Per-Package Rule Files

An instrumentation package provides its rules in one or more YAML files placed in the
package directory. `otelc` recognizes the following filenames:

| Pattern | Example |
| --- | --- |
| `otelc.yml` | `otelc.yml` |
| `otelc.yaml` | `otelc.yaml` |
| `*.otelc.yml` | `http.otelc.yml` |
| `*.otelc.yaml` | `http.otelc.yaml` |

> [!NOTE]
> Some older design documents refer to `otel.instrumentation.yml`. That name is aspirational;
> the tool only reads the four `*.otelc.yml` / `otelc.yml` patterns listed above.

Rule files are discovered in the **package directory** (not the module root), and the walk
skips any subdirectory that contains its own `go.mod` — so sub-modules are not accidentally
included. The rule schema is documented in [Instrumentation Rules](rules.md).

## Discovery and Resolution Protocol

When `otelc` finds a tool file during setup, it runs the following algorithm to build the
complete rule set:

1. **Parse.** The tool file is parsed with `go/parser` using `parser.ImportsOnly`. The
   `//go:build tools` constraint is intentionally ignored — the file is read even though it
   never compiles into the binary.

2. **Collect imports.** All blank imports (`_ "path"`) are extracted in the order they
   appear. Any non-blank import (named or dot) causes an immediate error.

3. **Resolve packages.** All collected import paths are resolved in a single
   `packages.Load` call from the directory containing the tool file, using the project's
   normal module graph (including `replace` directives and the module proxy).

4. **Discover per-package config.** For each resolved package:
   - The tool looks for a tool file (`otel.instrumentation.go` / `otelc.tool.go`) in the
     package's **module root** (not the package directory).
   - It also walks the **package directory** for `*.otelc.yml` / `otelc.yml` rule files,
     skipping any sub-module boundaries.
   - A package that has neither a tool file nor any rule files is an error and fails the
     build immediately.

5. **Recurse.** If a resolved package's module contains its own tool file, that tool file is
   added to the work queue. Both import paths and tool-file paths are de-duplicated, so
   diamond dependencies and shared modules are handled correctly.

6. **Load rules.** Rule files collected from all discovered packages are parsed and merged
   into the active rule set.

The entire traversal is bounded by the set of import paths reachable from the starting tool
files. Packages outside that reachable set are never loaded.

## Rule Source Precedence

The full precedence model is documented in [Rule Sources and Precedence](configuration.md#rule-sources-and-precedence).
In short: `OTELC_RULES` > `--rules` > tool files > embedded defaults. Each source entirely
replaces those below it; there is no merging.

## Errors and Diagnostics

**Both config file names present in the same module.** Only one is allowed. Remove
`otelc.tool.go` and keep `otel.instrumentation.go`.

**Non-blank import in the tool file.** Every import must use the blank identifier
(`_ "path"`). Named or dot imports are rejected at load time.

**Imported package is not part of a module.** Run `go mod tidy` to ensure the module graph
is consistent.

**Imported package is not an instrumentation package.** The package provides neither a
nested tool file nor any `*.otelc.yml` rule files. Remove the import or verify you have the
correct package path.

**Package load failure.** Run `go mod download` and confirm `go build ./...` succeeds before
retrying. See [Troubleshooting](troubleshooting.md#package-resolution-failures) for more.

## Composing Instrumentation Packages

Instrumentation packages can re-use other instrumentation packages by including their own
tool file. `otelc` discovers these nested tool files recursively.

The `test/apps/gincustom` fixture in this repository demonstrates two-level composition:

**`test/apps/gincustom/otel.instrumentation.go`** — the application's tool file:

```go
//go:build tools

package tools

import (
    _ "go.opentelemetry.io/otelc/test/apps/gincustom/instrumentation"
)
```

**`test/apps/gincustom/instrumentation/otel.instrumentation.go`** — the intermediate
package's tool file, which activates the standard HTTP server instrumentation:

```go
//go:build tools

package tools

import (
    _ "go.opentelemetry.io/otelc/instrumentation/net/http/server"
)
```

**`test/apps/gincustom/instrumentation/otelc.yaml`** — a custom rule for Gin, shipped
alongside the intermediate package:

```yaml
gin_custom:
  target: github.com/gin-gonic/gin
  where:
    func: New
  do:
    - inject_hooks:
        after: afterNew
        path: go.opentelemetry.io/otelc/test/apps/gincustom/instrumentation
```

When `otelc` processes the application's tool file, it:

1. Resolves `test/apps/gincustom/instrumentation` and finds `otelc.yaml` there.
2. Finds `instrumentation/otel.instrumentation.go` in the same module — recurses.
3. Resolves `instrumentation/net/http/server` and finds its `*.otelc.yml` rules.
4. Loads both the custom gin rule and the standard HTTP server rules.

This pattern lets library authors or platform teams distribute pre-configured instrumentation
bundles that applications activate with a single blank import.

## See Also

- [Instrumentation Rules](rules.md) — rule schema, `target`, `where`, `do` reference.
- [Configuration and Fine-Tuning](configuration.md) — how to scope and filter instrumentation.
- [ADR-0003: Structured Rule Schema](adr/0003-structured-rule-schema.md) — the frozen
  `target`/`where`/`do` schema decision.
- [Issue #567](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/567) —
  the tracking issue for the import-driven configuration model.
