# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Added

- `net/http`: support overriding known HTTP methods via
  `OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS` (comma-separated, case-sensitive full
  override of the default set). ([#1012](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1012))

### Changed

- ⚠️ **Breaking Change:** `net/http` no longer treats `QUERY` as a default known
  HTTP method. Such requests now record `http.request.method=_OTHER` (span name
  token `HTTP`) with `http.request.method_original=QUERY`. Restore `QUERY` (or
  other non-RFC methods) by setting a full override, for example
  `OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS=CONNECT,DELETE,GET,HEAD,OPTIONS,PATCH,POST,PUT,TRACE,QUERY`.
  ([#1012](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1012))

### Deprecated

### Removed

### Fixed
