# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The full list of changes can be found in the compare view for the respective release at <https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/releases>.

## Unreleased

### Added

### Changed

- ⚠️ **Breaking Change:** Template variable `{{ FuncName }}` should now be accessed with `{{ .FuncName }}`. ([#729](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/729))
- `otelc go build` no longer rewrites the tool file or runs `go mod tidy` during a re-pin when neither the tool file nor `go.mod` changed and `go.sum` is present. ([#TBD](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/TBD))

### Deprecated

### Removed

### Fixed

<!-- Released section -->
<!-- Don't change this section unless doing release -->