# linodego v2 instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/linode/linodego/v2`](https://github.com/linode/linodego).

## What is instrumented

| Layer | Target | Span name | Scope |
|-------|--------|-----------|--------|
| Public API | Exported `*Client` methods with `(context.Context, …) (…, error)` | `linodego.<Method>` | `go.opentelemetry.io/otelc/instrumentation/github.com/linode/linodego/v2` |
| HTTP request | `(*Client).doRequest` | `{METHOD} {endpoint}` | same |

Client configuration helpers (`SetToken`, `SetBaseURL`, …) are not instrumented.

### Metrics

- **`linodego.client.operation.duration`** (histogram, seconds) — public API method latency.
- Labels (moderate cardinality only): `server.address`, `code.function.name`, `http.response.status_code` (when known).
- **Not** labeled by URL paths that embed resource IDs (those stay on spans).
- Per-request HTTP duration is left to net/http instrumentation / request spans.

### Enable / disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=linodego   # allow-list mode
export OTEL_GO_DISABLED_INSTRUMENTATIONS=linodego  # turn off only this library
```

Instrumentation key: `LINODEGO` (case-insensitive).

## Supported versions

- Module: `github.com/linode/linodego/v2`
- Public-method rules are generated against **v2.4.1** and apply to **v2.4.1+** (`version: v2.4.1` minimum bound).
- `doRequest` is hooked for **v2.0.0+**.

When bumping the dependency, regenerate hooks (below) and update:

- `go.mod` require for `github.com/linode/linodego/v2`
- `//go:generate` version in `generate.go`
- Integration test app `test/apps/linodegoclient/go.mod`

## Regenerating public-method hooks

```bash
cd instrumentation/github.com/linode/linodego/v2
go run ./gen -version v2.4.1   # or: go generate ./...
go test ./...
```

This rewrites:

- `public_methods_gen.go` — unique `BeforeX` / `AfterX` wrappers
- `public_methods.otelc.yaml` — one inject_hooks rule per method

Unique hook names are required so otelc’s `//go:linkname` stubs do not redeclare
the same symbol across multiple source files in the `linodego` package.

## Tests

```bash
# Unit
go test ./instrumentation/github.com/linode/linodego/v2/...

# Integration (requires: make build)
go -C test test -tags=integration -run TestLinodegoClient ./integration/
```

The integration app lives in `test/apps/linodegoclient` and exercises List/Get
across regions, account, volumes, and instances against an in-process mock API.
