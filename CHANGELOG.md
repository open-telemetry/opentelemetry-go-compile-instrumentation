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

- `redis`: instrument the shards a `redis.Ring` is built with. `NewRing` creates
  a shard for every entry in `RingOptions.Addrs` during construction, before the
  hook runs, and `OnNewNode` only fires for shards created after it is
  registered, so shards configured up front were silently left uninstrumented.
  Rings configured this way now emit per-command client spans where they
  previously emitted none, which will look like new traffic to a telemetry
  pipeline. If you worked around this by attaching the hook to ring shards
  yourself, remove that code: otherwise the hook runs twice and each command
  records two identical spans.
  ([#1098](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/1098))
