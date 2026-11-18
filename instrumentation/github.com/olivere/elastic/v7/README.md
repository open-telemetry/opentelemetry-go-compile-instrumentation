# olivere/elastic v7 instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/olivere/elastic/v7`](https://github.com/olivere/elastic).

`(*Client).PerformRequest` is the shared HTTP path for Search, Index, Bulk,
Delete, and the rest of the public API. Each call becomes one CLIENT span.

`NewClient` sniff and healthcheck call the inner `http.Client` directly.
They do not go through `PerformRequest`. `NewSimpleClient` skips those loops.

| Target | Span name | Attributes |
| --- | --- | --- |
| `(*Client).PerformRequest` | `{operation} {index}` | `db.system.name=elasticsearch`, `db.operation.name`, `db.collection.name` (index), `db.operation.batch.size` (bulk, mget), HTTP method/path, `db.response.status_code` |

`db.collection.name` comes from the request path, so `/orders/_bulk` gets
`orders`. A bare `/_bulk` or `/_mget` names the index per item and gets no
collection. `db.operation.batch.size` is the number of bulk actions or mget
docs, and is set only for two or more.

A `Get` of a missing document returns 404 with `"found":false`. The span
keeps `db.response.status_code=404` but is not marked as an error, the
same way `Exists` ignores 404. A 404 such as `index_not_found_exception`
is still an error.

Inner `net/http` client spans are suppressed so the datastore hop is not
duplicated as a generic GET/POST. The host is chosen inside `PerformRequest`
after the before hook, so this span does not set `server.address` or
`url.full`.

### Enable / disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=elastic   # allow-list mode
export OTEL_GO_DISABLED_INSTRUMENTATIONS=elastic  # turn off only this library
```

Instrumentation key: `ELASTIC` (case-insensitive).

## Supported versions

- Module: `github.com/olivere/elastic/v7`
- Minimum bound: **v7.0.0** (`PerformRequest(ctx, PerformRequestOptions)`).

The package is archived upstream. This hook covers the v7 line still in
wide use. The maintained OpenSearch client is a separate gap.

## Tests

```bash
go test ./instrumentation/github.com/olivere/elastic/v7/...

# Integration (requires: make build)
go -C test test -tags=integration -run TestElasticClient ./integration/
```
