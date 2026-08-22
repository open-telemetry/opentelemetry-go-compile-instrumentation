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

- Trampoline generation now re-scopes a recovered generic constraint to the receiver's type parameter names, so an inter-parameter constraint on a renamed receiver (`type M[K any, V ~[]K]` used as `func (m M[A, B]) ...`) produces compilable code instead of referring to a name it never declares. ([#1169](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/1169))

<!-- Released section -->
<!-- Don't change this section unless doing release -->