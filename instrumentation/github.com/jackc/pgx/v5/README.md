# jackc/pgx v5 instrumentation

Compile-time OpenTelemetry instrumentation for [`github.com/jackc/pgx/v5`](https://github.com/jackc/pgx).

## Overview

This module provides compile-time auto-instrumentation for applications using the native `pgx/v5` PostgreSQL driver and connection pool (`pgxpool`). It automatically intercepts connection and pool initialization to inject OpenTelemetry tracers without requiring code modifications or manual middleware configuration.

### Hooked Functions

| Target | Hook Point | Description |
| --- | --- | --- |
| `github.com/jackc/pgx/v5.connect` | Before | Injects tracer into `*pgx.ConnConfig` before connection construction (covers `pgx.Connect`, `pgx.ConnectConfig`, `pgx.ConnectWithOptions`) |
| `github.com/jackc/pgx/v5.ConnectConfig` | Before | Injects tracer into `*pgx.ConnConfig` before connection configuration is copied |
| `github.com/jackc/pgx/v5/pgxpool.NewWithConfig` | Before | Injects tracer into `*pgxpool.Config.ConnConfig` before pool creation (covers `pgxpool.New` and `pgxpool.NewWithConfig`) |

If user code already configures a custom `pgx.QueryTracer`, the instrumentation preserves it by chaining invocations so that all tracers run seamlessly.

## Telemetry Spans & Semantic Conventions

Traces conform to OpenTelemetry Database Semantic Conventions (`semconv/v1.37.0`):

| Scope | Span Name | Key Attributes |
| --- | --- | --- |
| Query | `{operation} {db.namespace}` | `db.system: "postgresql"`, `db.query.text`, `db.operation.name`, `db.namespace`, `server.address`, `server.port`, `db.user` |
| Batch | `BATCH {db.namespace}` | `db.system: "postgresql"`, `db.operation.name: "BATCH"`, `db.namespace`, `server.address`, `server.port` |
| CopyFrom | `COPY {table}` | `db.system: "postgresql"`, `db.operation.name: "COPY"`, `db.collection.name` (table), `db.namespace`, `server.address`, `server.port` |
| Prepare | `PREPARE {name}` | `db.system: "postgresql"`, `db.operation.name: "PREPARE"`, `db.query.text` (sanitized SQL statement), `db.namespace`, `server.address`, `server.port` |

On errors, the span records the error event and sets span status to `Error` with the error description.

### Query Sanitization

By default, literal values (single-quoted strings, dollar-quoted strings, and numeric literals) in queries are replaced with `?` to prevent exporting sensitive data (such as passwords or personal identifiable information) in telemetry spans. Positional parameter placeholders (e.g. `$1`, `$2`) are preserved.

To capture raw, unsanitized query text verbatim, set:
```bash
export OTEL_GO_INSTRUMENTATION_PGX_RAW_QUERY=true
```

## Enable / Disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=pgx   # allow-list mode
export OTEL_GO_DISABLED_INSTRUMENTATIONS=pgx  # disable only this library
```

Instrumentation key: `PGX` (case-insensitive).

## Supported Versions

- Module: `github.com/jackc/pgx/v5`
- Minimum bound: **v5.0.0**

## Tests

```bash
go test -v ./instrumentation/github.com/jackc/pgx/v5/...
```
