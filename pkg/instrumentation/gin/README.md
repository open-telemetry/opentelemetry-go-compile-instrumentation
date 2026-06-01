# Gin HTTP Server Instrumentation

Compile-time OpenTelemetry instrumentation for the [Gin](https://github.com/gin-gonic/gin) HTTP web framework.

## What is instrumented

| Function | Hook point | Description |
|---|---|---|
| `(*gin.Engine).ServeHTTP` | Before + After | Creates a server span for every incoming HTTP request |

## Span attributes

The following [HTTP semantic conventions](https://opentelemetry.io/docs/specs/semconv/http/http-spans/#http-server-span) are recorded on each span:

| Attribute | Notes |
|---|---|
| `http.request.method` | Normalised to uppercase |
| `url.path` | Request path |
| `url.scheme` | `http` or `https` |
| `server.address` | Host from the request |
| `server.port` | Port (when non-standard) |
| `client.address` | Peer or forwarded IP |
| `user_agent.original` | User-Agent header |
| `network.protocol.version` | e.g. `1.1` |
| `http.response.status_code` | Written by the handler |

## Enabling / disabling

The instrumentation respects the standard environment variables:

```sh
# Disable gin instrumentation
OTEL_GO_DISABLED_INSTRUMENTATIONS=gin

# Enable only gin instrumentation
OTEL_GO_ENABLED_INSTRUMENTATIONS=gin
```

## Usage

Build your Gin application with `otelc` instead of `go build`:

```sh
otelc go build ./...
```

No source code changes are required.

## Version support

| Gin version | Supported |
|---|---|
| v1.7.x – v1.10.x | Yes |
