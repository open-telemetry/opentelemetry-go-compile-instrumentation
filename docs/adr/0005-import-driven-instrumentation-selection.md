# 5. Import-Driven Instrumentation Selection

Date: 2026-06-16

## Status

Accepted

## Context

Today, `otelc` loads all built-in instrumentation rules and matches them against the application's dependency graph. This makes instrumentation selection implicit: if a dependency is present and a matching rule exists, that instrumentation may be applied.

This model works for the built-in rule set, but it does not scale well to external instrumentations. While `otelc` already supports replacing the default rules through `--rules`, that mechanism is intended for supplying an alternate rule set rather than declaratively enabling a set of instrumentations. Users cannot explicitly declare which instrumentations they want to enable at compile time, and third-party modules cannot easily distribute their own instrumentation packages without being embedded into `otelc` itself.

The existing `tools.go` pattern used by Go projects provides a natural mechanism for declaring build-time dependencies. DataDog's Orchestrion uses the same approach through `orchestrion.tool.go`, where blank imports define the set of active instrumentations and recursively discover additional instrumentation packages.

## Decision

Adopt an import-driven model for instrumentation selection.

An application may declare enabled instrumentations in a module-root tool file. Two filenames are accepted:

* `otel.instrumentation.go` (canonical)
* `otelc.tool.go` (alias)

The file follows the standard `tools.go` pattern (`//go:build tools`) and contains blank imports for the instrumentation packages that should be enabled.

Instrumentation packages are resolved using `go/packages`. A package is considered an instrumentation package if it contains either an instrumentation tool file or one or more `*.otelc.yml` rule files. Instrumentation packages may themselves import other instrumentation packages, allowing instrumentation dependencies to be composed recursively.

Rule discovery is scoped to the instrumentation package's own module root and does not recurse into nested Go modules.

If no tool file exists, `otelc` preserves the current "clean-room" workflow by automatically generating a temporary instrumentation configuration from the application's dependency graph for the duration of the build. Users who want a persistent, source-controlled configuration can create one explicitly using `otelc pin`.

## Consequences

* Instrumentations become regular Go module dependencies, enabling third-party modules to distribute and version their own `otelc`-compatible instrumentations independently of the `otelc` repository. This provides a path toward a broader instrumentation ecosystem where users can choose, pin, and update instrumentation packages through standard Go module workflows.
* Instrumentation selection becomes explicit and visible in source control, rather than being inferred solely from the dependency graph.
* Instrumentation packages participate naturally in `go mod tidy` and existing dependency management workflows.
* The `otelc pin` command can create, update, and validate the tool file while preserving the existing zero-configuration experience through auto-pin.
* Tradeoff: `otelc` now loads the built-in default rules by first generating a temporary `otel.instrumentation.go` when no user-provided tool file exists, adding an extra intermediate step to the default rule-loading process. This moves dependency synchronization (`go mod tidy`) into the pinning stage, ensuring that rule matching always operates on the final dependency graph and avoiding stale rule sets after dependency changes. As a side effect, built-in and third-party instrumentations now share the same loading path while preserving the existing zero-configuration workflow.
