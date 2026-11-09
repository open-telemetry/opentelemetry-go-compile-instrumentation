# clickhouse-go v2 instrumentation

Compile-time OpenTelemetry instrumentation for the native API of
[`github.com/ClickHouse/clickhouse-go/v2`](https://github.com/ClickHouse/clickhouse-go).

`clickhouse.Open` returns a `driver.Conn` that does not go through
`database/sql`, so the `database/sql` instrumentation does not see its calls.
This module traces them. Connections opened with `clickhouse.OpenDB` keep using
the `database/sql` instrumentation and are not traced twice.

## What is instrumented

| Call on a native conn                                | Span name                | `db.operation.name`                       |
| ---------------------------------------------------- | ------------------------ | ----------------------------------------- |
| `Query`, `QueryRow`, `Select`, `Exec`, `AsyncInsert` | `{operation} {database}` | first keyword of the query, e.g. `SELECT` |
| `Ping`                                               | `PING {database}`        | `PING`                                    |
| `PrepareBatch`                                       | `{operation} {database}` | first keyword of the query, e.g. `INSERT` |
| `Send` of a batch prepared with `PrepareBatch`       | `{operation} {database}` | first keyword of the query, e.g. `INSERT` |

Every span is a CLIENT span, a child of the span in the call's context, with
`db.system.name=clickhouse`, `db.query.text`, and, when known, `db.namespace`,
`server.address` and `server.port`. The address is recorded only when the conn
has a single address, since with several the hook cannot tell which one served
the call. A failed call records the error, sets the span status to `Error`, and
sets `error.type` and `db.response.status_code` to the server exception code,
or `error.type=_OTHER` when the error did not come from the server.

A batch gets one span when it is sent, whatever the number of rows appended to
it: `Append` is not hooked. The `Send` span is a sibling of the `PrepareBatch`
span, under the context passed to `PrepareBatch`.

`Select` calls `Query` in recent releases; only the `Select` span is recorded.

The span of `Query` ends when `Query` returns, before the caller reads the rows,
as for `database/sql`.

Not instrumented: `Batch.Flush`, `Batch.Abort`, `QueryFormat`, `InsertFormat`,
and conn lifecycle (`Open`, `Close`).

## Supported versions

`v2.32.0` and later.

## Enable / disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=clickhouse   # allow-list mode
export OTEL_GO_DISABLED_INSTRUMENTATIONS=clickhouse  # turn off only this library
```

Instrumentation key: `CLICKHOUSE` (case-insensitive).
