# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The full list of changes can be found in the compare view for the respective release at <https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/releases>.

## Unreleased

### Added

### Changed

- ⚠️ **Breaking Change:** Template variable `{{ FuncName }}` should now be accessed with `{{ .FuncName }}`. ([#729](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/729))
- Reduce per-request allocations in the `net/http` client and server hooks: hook state is passed via a typed struct instead of a `map[string]interface{}`, and debug log arguments are no longer built when debug logging is off. ([#1137](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1137))

### Deprecated

### Removed

### Fixed

<!-- Released section -->
<!-- Don't change this section unless doing release -->
