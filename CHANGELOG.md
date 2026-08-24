# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The full list of changes can be found in the compare view for the respective release at <https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/releases>.

## Unreleased

### Added

- Compile-time instrumentation for `google.golang.org/genai`, emitting GenAI client spans for Gemini Developer API and Vertex AI `generateContent` calls. ([#1153](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1153))

### Changed

- ⚠️ **Breaking Change:** Template variable `{{ FuncName }}` should now be accessed with `{{ .FuncName }}`. ([#729](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/729))

### Deprecated

### Removed

### Fixed

<!-- Released section -->
<!-- Don't change this section unless doing release -->