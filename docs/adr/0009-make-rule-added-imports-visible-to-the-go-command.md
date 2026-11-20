# 9. Make rule-added imports visible to the go command

Date: 2026-11-20

## Status

Proposed

## Context

Rules add imports to the packages they instrument (`imports:` on raw/call/lit/struct rules,
the imports of `add_file` sources, and `runtime` for `//otelc:ignore`, which goes into every
func-rule target once any package in the build uses the directive). The go command plans the
build before toolexec runs, so it never sees those imports: their archives are not in the
compile or link importcfg. Today toolexec fills the gap by building each missing package in a
nested `go list -export` and patching importcfg, plus tracking files and link recovery. This is
the instrument phase of the two-phase build in
[ADR-0002](0002-api-design-and-project-structure.md).

That stops working on Go 1.28. `go list -export` now returns `cmd/export` data that the compiler
rejects ([#1740](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1740), [golang/go#79427](https://github.com/golang/go/issues/79427)). The Go team considers building those packages in a separate
build unsound in general: test variants, PGO, compiler flags and link drift can all differ from
the real build. Their recommendation is to put the imports where the go command can see them,
with `-overlay`.

What `-overlay` can and cannot do (checked on go1.27 and gotip):

- it can add a file to a main module, workspace or GOROOT package, and the new import shows up
  in the importcfg;
- it cannot touch anything under GOMODCACHE (hard error). That includes GOROOT when the
  toolchain was downloaded through `GOTOOLCHAIN`;
- only the last `-overlay` flag counts.

[#1781](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1781) made this work for module cache packages too by replacing the module with a directory of
symlinks/overlay entries. It works, but costs a lot: a shadow module tree, cgo special cases,
`=> .otelc-build/modreplace/... (devel)` in buildinfo, and a go.mod edit on disk.

Instrumentation rules now live in opentelemetry-go-compile-contrib
([ADR-0007](0007-migrate-instrumentations-to-contrib.md)), so the rules this ADR constrains are
mostly maintained there.

## Decision

1. `otelc setup` collects every import a rule can add, per target package, and writes them to
   `.otelc-build/overlay.json` as an `otelc.imports.go` blank-import file in the package
   directory, merged with any `-overlay` the user passed. `otelc go build/test` passes
   `-overlay` itself.
2. Rules must not add an import to a package in the module cache that the package does not
   already import. Setup fails with an error naming the rule, the package and the import.
   Rules that need code from another package reach it with `//go:linkname`, which the contrib
   otel rules already do (`pkg/runtime` logger, signal hooks, SDK init).
   The same applies to GOROOT packages when GOROOT lives in the module cache.
3. otelc follows the same rule: `//otelc:ignore` calls `runtime.SuppressHooks`,
   `UnsuppressHooks` and `HooksSuppressed` by `//go:linkname` instead of importing `runtime`.
   otelc's runtime file marks them as linkname targets for the Go 1.23+ linkname check.
4. Drop-in mode (`go build -toolexec=...`) needs the `-overlay=` that setup prints, on build
   commands only. It must not go into `GOFLAGS`: every go command reads it then, so
   `go mod tidy` keeps requirements only otelc's imports need, and once `.otelc-build` is
   cleaned up every go command fails with `go: reading overlay: ... no such file or directory`.
5. Toolexec stops resolving packages. It only checks that every import it adds is already in
   the importcfg, and fails with an error pointing at setup/`-overlay` when one is missing.

## Rollout

1. The contrib otel rules add no imports to module cache packages (linkname refactor of
   `otel/init` and `otel/sdk/trace`, [opentelemetry-go-compile-contrib#56](https://github.com/open-telemetry/opentelemetry-go-compile-contrib/pull/56)). No behavior change.
2. `//otelc:ignore` uses linkname. Setup writes the overlay and `otelc go` passes it. Toolexec
   resolution stays as a fallback, so drop-in builds without `-overlay` keep working.
3. `fix(tool)!:` remove toolexec resolution. Drop-in builds need `-overlay`.

## Consequences

- Rule-added imports build in the same build as everything else, on every Go version
  including 1.28. No nested builds, no importcfg patching, no tracking files.
- About 4.9k lines of resolution code go away in step 3.
- Drop-in users must add `-overlay=` to their build command (breaking, step 3).
- Custom rules that add new imports to third-party packages stop working and must use
  `//go:linkname` instead. Setup reports it clearly.
- `go test`: the generated testmain takes no new imports, so rules targeting `main` must not
  add imports either (the contrib SDK init already uses linkname).
- This amends the instrument phase described in ADR-0002.
