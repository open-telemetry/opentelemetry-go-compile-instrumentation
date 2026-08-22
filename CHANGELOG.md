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

- database/sql spans created by a before hook are now always ended, even if instrumentation is disabled mid-operation. The after hooks and `instrumentEnd` re-checked `Enable()` and returned before `span.End()`, so a span created while enabled leaked if instrumentation was disabled before the operation completed. This mirrors the net/http fix in [#1094](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1094). ([#1206](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1206))

<!-- Released section -->
<!-- Don't change this section unless doing release -->