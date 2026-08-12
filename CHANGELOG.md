# Changelog

All notable changes to this project are documented in this file.

## Unreleased

### Breaking

- `net/http`: `QUERY` is no longer treated as a default known HTTP method.
  Requests that use `QUERY` now record `http.request.method=_OTHER` (span name
  token `HTTP`) with `http.request.method_original=QUERY`.
  To keep treating `QUERY` (or other non-RFC methods such as `PROPFIND`) as known,
  set a full override via `OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS`, for example:

  ```bash
  OTEL_INSTRUMENTATION_HTTP_KNOWN_METHODS=CONNECT,DELETE,GET,HEAD,OPTIONS,PATCH,POST,PUT,TRACE,QUERY
  ```

  This list fully replaces the defaults; it is not additive. (#1012)
