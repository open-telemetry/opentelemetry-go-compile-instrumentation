# opensearch-go v5 instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/opensearch-project/opensearch-go/v5`](https://github.com/opensearch-project/opensearch-go).

`(*Client).Request` is the shared HTTP path for Search, Index, Bulk, Delete,
and the rest of the public API. Each call becomes one CLIENT span named
`{operation} {index}` with `db.system.name=opensearch`.

Node discovery and health checks live on the transport and never reach
`Request`, so they do not create OpenSearch spans. Independently enabled
`net/http` client instrumentation can still trace that traffic.

| Target | Span name | Attributes |
| --- | --- | --- |
| `(*Client).Request` | `{operation} {index}` | `db.system.name=opensearch`, `db.operation.name`, `db.collection.name` (index), HTTP method/path, `server.address`/`server.port`, `db.response.status_code` |

Inner `net/http` client spans are suppressed so the datastore hop is not
duplicated as a generic GET/POST.

The official [`osotel`](https://github.com/opensearch-project/opensearch-go/tree/main/osotel)
package ships low-cardinality RED *metrics*; this hook adds the *trace* side
with database semantics.

### Enable / disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=opensearch   # allow-list mode
export OTEL_GO_DISABLED_INSTRUMENTATIONS=opensearch  # turn off only this library
```

Instrumentation key: `OPENSEARCH` (case-insensitive).

## Supported versions

- Module: `github.com/opensearch-project/opensearch-go/v5`
- Minimum bound: **v5.0.0** (`(*Client).Request(*http.Request)`).

## Tests

```bash
go test ./instrumentation/github.com/opensearch-project/opensearch-go/v5/...
```
