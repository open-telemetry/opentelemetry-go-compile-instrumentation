# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The full list of changes can be found in the compare view for the respective release at <https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/releases>.

## Unreleased

### Added

### Changed

- ⚠️ **Breaking Change:** Template variable `{{ FuncName }}` should now be accessed with `{{ .FuncName }}`. ([#729](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/729))

### Deprecated

### Removed

### Fixed

- `redis.Ring` shards configured up front via `RingOptions.Addrs` are now instrumented. Previously only shards added after client construction were hooked, so pre-configured shards (the common case) emitted no spans at all. ⚠️ Rings that emitted nothing before now emit per-command client spans, and any manual `AddHook`/`ForEachShard` workaround should be removed to avoid duplicate spans. ([#1098](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1098))

<!-- Released section -->
<!-- Don't change this section unless doing release -->