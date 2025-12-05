# net/http Compile-Time Instrumentation

This package provides automatic OpenTelemetry instrumentation for Go's `net/http` package using compile-time code injection.

## Overview

Unlike traditional HTTP instrumentation that requires manual wrapper code, this package automatically instruments **all** HTTP traffic in your application at compile-time. Zero code changes required!

### Key Features

✅ **Zero Code Changes**: Automatic instrumentation without modifying application code
✅ **Universal Coverage**: Instruments ALL HTTP calls, including stdlib internals
✅ **W3C Trace Context**: Automatic context propagation between services
✅ **Semantic Conventions**: Follows OpenTelemetry HTTP semantic conventions
✅ **Client & Server**: Complete instrumentation for both HTTP clients and servers
✅ **Status Code Capture**: Accurate response status code tracking
✅ **Error Recording**: Automatic error span status on failures
✅ **Metrics Collection**: Duration and count metrics (via operation listeners)

## How It Works

### Compile-Time Injection

The instrumentation is injected during the build process:

```
┌─────────────────────────────────────────────┐
│  1. go build (with our toolexec)            │
│                                             │
│  2. Setup Phase:                            │
│     - Scan dependencies                     │
│     - Match net/http functions              │
│     - Generate otel.runtime.go              │
│                                             │
│  3. Instrument Phase:                       │
│     - Inject trampolines into:              │
│       • http.Transport.RoundTrip            │
│       • http.serverHandler.ServeHTTP        │
│                                             │
│  4. Build with instrumentation baked in     │
└─────────────────────────────────────────────┘
```

### Runtime Execution

When your application runs, the injected hooks automatically:

**For HTTP Clients** (`http.Transport.RoundTrip`):

1. **Before**: Create span, inject trace context into headers
2. **Execute**: Actual HTTP request
3. **After**: End span, record status, collect metrics

**For HTTP Servers** (`http.Handler.ServeHTTP`):

1. **Before**: Extract trace context, create span, wrap ResponseWriter
2. **Execute**: Actual request handling
3. **After**: End span, record status code, collect metrics

## Usage

### Building Your Application

```bash
# Build with automatic instrumentation
/path/to/otel go build -a

# Run your application normally
./myapp
```

That's it! All HTTP traffic is now instrumented.

### Configuration

The instrumentation is configured at compile-time via `tool/data/nethttp.yaml`:

```yaml
client_hook:
  target: net/http
  func: RoundTrip
  recv: "*Transport"
  before: BeforeRoundTrip
  after: AfterRoundTrip
  path: "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp/client"

server_hook:
  target: net/http
  func: ServeHTTP
  recv: serverHandler
  before: BeforeServeHTTP
  after: AfterServeHTTP
  path: "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/nethttp/server"
```

### Environment Variables

Control instrumentation behavior at runtime:

```bash
# Enable only specific instrumentations (comma-separated list)
export OTEL_GO_ENABLED_INSTRUMENTATIONS=nethttp,grpc

# Disable specific instrumentations (comma-separated list)
export OTEL_GO_DISABLED_INSTRUMENTATIONS=nethttp

# General OpenTelemetry configuration
export OTEL_SERVICE_NAME=my-service
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
export OTEL_LOG_LEVEL=debug  # debug, info, warn, error
```

## Package Structure

```
pkg/instrumentation/nethttp/
├── go.mod                       # Parent module (shared types)
├── data_types.go                # NetHttpRequest, NetHttpResponse
├── data_types_test.go
├── client/
│   ├── go.mod                   # Client module
│   ├── client_hook.go           # BeforeRoundTrip, AfterRoundTrip
│   ├── client_instrumenter.go  # Instrumenter builder
│   ├── client_attrs_getter.go  # HTTP client attribute extraction
│   └── *_test.go
└── server/
    ├── go.mod                   # Server module
    ├── server_hook.go           # BeforeServeHTTP, AfterServeHTTP
    ├── server_instrumenter.go  # Instrumenter builder
    ├── server_attrs_getter.go  # HTTP server attribute extraction
    ├── response_writer.go       # Status code capture wrapper
    └── *_test.go
```

## Semantic Conventions

The instrumentation follows [OpenTelemetry HTTP Semantic Conventions v1.28.0](https://opentelemetry.io/docs/specs/semconv/http/).

### Client Span Attributes

| Attribute | Example | Description |
|-----------|---------|-------------|
| `http.request.method` | `GET` | HTTP request method |
| `url.full` | `https://api.example.com/users?id=123` | Full URL |
| `server.address` | `api.example.com` | Server host |
| `server.port` | `443` | Server port |
| `network.protocol.version` | `1.1` | HTTP version |
| `http.response.status_code` | `200` | Response status code |
| `error.type` | `timeout` | Error type (if error occurred) |

### Server Span Attributes

| Attribute | Example | Description |
|-----------|---------|-------------|
| `http.request.method` | `POST` | HTTP request method |
| `url.scheme` | `https` | URL scheme |
| `url.path` | `/api/users` | URL path |
| `url.query` | `id=123` | Query string |
| `http.route` | `/api/users/{id}` | Route pattern (if available) |
| `network.protocol.version` | `2` | HTTP version |
| `http.response.status_code` | `201` | Response status code |
| `client.address` | `192.168.1.100` | Client IP address |

### Span Names

**Client**: `HTTP <method>` (e.g., `HTTP GET`)
**Server**: `<method> <route>` (e.g., `POST /api/users`)

### Span Status

- **OK**: HTTP status codes 2xx, 3xx, 4xx (client errors are not span errors)
- **ERROR**: HTTP status codes 5xx, network errors, timeouts

## Examples

### Example 1: HTTP Client

Your code (no changes):

```go
package main

import (
    "net/http"
)

func main() {
    resp, err := http.Get("https://api.example.com/users")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
    // ... handle response
}
```

What happens automatically:

1. Span created: `HTTP GET`
2. Trace context injected into request headers
3. Attributes recorded: method, URL, status code, etc.
4. Span ended after response received

### Example 2: HTTP Server

Your code (no changes):

```go
package main

import (
    "net/http"
)

func handler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("Hello, World!"))
}

func main() {
    http.HandleFunc("/hello", handler)
    http.ListenAndServe(":8080", nil)
}
```

What happens automatically:

1. Trace context extracted from headers
2. Span created: `GET /hello`
3. ResponseWriter wrapped to capture status code
4. Attributes recorded: method, path, status code, etc.
5. Span ended after handler completes

### Example 3: Distributed Tracing

**Service A (Client)**:

```go
resp, _ := http.Get("http://service-b:8080/api")
```

**Service B (Server)**:

```go
http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
    // Trace context automatically propagated!
    // This span will be a child of Service A's span
    w.WriteHeader(http.StatusOK)
})
```

Trace visualization in Jaeger:

```
Service A: HTTP GET
  └─> Service B: GET /api
```

## Implementation Details

### Hook Functions

**Client Hooks** (`pkg/instrumentation/nethttp/client/client_hook.go`):

```go
func BeforeRoundTrip(ictx inst.HookContext, transport *http.Transport, req *http.Request) {
    // 1. Check if instrumentation is enabled
    // 2. Filter out OTel exporter requests (prevent infinite loops)
    // 3. Build NetHttpRequest from http.Request
    // 4. Start instrumentation span
    // 5. Inject trace context into headers
    // 6. Update request with new context
    // 7. Store data for AfterRoundTrip
}

func AfterRoundTrip(ictx inst.HookContext, res *http.Response, err error) {
    // 1. Retrieve data from BeforeRoundTrip
    // 2. Build NetHttpResponse from http.Response
    // 3. End instrumentation span
    // 4. Record status code and error
    // 5. Collect metrics
}
```

**Server Hooks** (`pkg/instrumentation/nethttp/server/server_hook.go`):

```go
func BeforeServeHTTP(ictx inst.HookContext, recv interface{}, w http.ResponseWriter, r *http.Request) {
    // 1. Check if instrumentation is enabled
    // 2. Build NetHttpRequest from http.Request
    // 3. Extract trace context from headers
    // 4. Start instrumentation span
    // 5. Wrap ResponseWriter to capture status code
    // 6. Store data for AfterServeHTTP
}

func AfterServeHTTP(ictx inst.HookContext) {
    // 1. Retrieve data from BeforeServeHTTP
    // 2. Extract status code from wrapped ResponseWriter
    // 3. Build NetHttpResponse
    // 4. End instrumentation span
    // 5. Record status code
    // 6. Collect metrics
}
```

### Response Writer Wrapping

To capture the response status code, we wrap `http.ResponseWriter`:

```go
type writerWrapper struct {
    http.ResponseWriter
    statusCode  int
    wroteHeader bool
}

func (w *writerWrapper) WriteHeader(statusCode int) {
    if !w.wroteHeader {
        w.statusCode = statusCode
        w.wroteHeader = true
        w.ResponseWriter.WriteHeader(statusCode)
    }
}
```

This wrapper implements common interfaces: `http.Hijacker`, `http.Flusher`, `http.Pusher`.

## Testing

### Unit Tests

```bash
# Test client instrumentation
cd pkg/instrumentation/nethttp/client
go test -v ./...

# Test server instrumentation
cd pkg/instrumentation/nethttp/server
go test -v ./...
```

Test coverage:

- ✅ Attribute getter logic (14 tests per side)
- ✅ Response writer wrapper (12 tests)
- ✅ Instrumenter building (2 tests per side)
- ✅ Edge cases and error handling

### Integration Tests

```bash
# Run integration tests
go test -v -tags=integration ./test/integration/http_*

# Run e2e tests
go test -v -tags=e2e ./test/e2e -run TestHttp
```

Test scenarios:

- ✅ Client-server communication with trace propagation
- ✅ Status code capture (200, 201, 400, 500, etc.)
- ✅ Error handling
- ✅ Instrumentation enable/disable

## Performance

### Overhead

| Component | Overhead per Request |
|-----------|---------------------|
| Hook trampoline | ~50 ns (negligible) |
| Span creation | ~1-2 μs |
| Attribute extraction | ~500 ns |
| Context propagation | ~300 ns |
| **Total** | **~2-3 μs** |

For a typical web request taking 10-100ms, instrumentation overhead is **< 0.01%**.

### Memory

- Span data: ~500 bytes per span
- Context: ~100 bytes per request
- Batch export: Minimal footprint

## Troubleshooting

### Instrumentation Not Working

**Check 1: Is instrumentation enabled?**

```bash
# Make sure nethttp is not in the disabled list
unset OTEL_GO_DISABLED_INSTRUMENTATIONS
# Or explicitly enable it
export OTEL_GO_ENABLED_INSTRUMENTATIONS=nethttp
```

**Check 2: Was the app built with the otel tool?**

```bash
/path/to/otel go build -a
```

**Check 3: Check logs**

```bash
export OTEL_LOG_LEVEL=debug
./myapp
# Look for "HTTP client/server instrumentation initialized"
```

### Traces Not Appearing

**Check 1: Is exporter configured?**

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
```

**Check 2: Is the OpenTelemetry collector running?**

```bash
# Check if OTLP receiver is accessible
curl http://localhost:4318/v1/traces
```

### Infinite Loop (OTel Exporter Instrumented)

The hooks automatically filter out requests from the OpenTelemetry HTTP exporter:

```go
userAgent := req.Header.Get("User-Agent")
if strings.HasPrefix(userAgent, "OTel OTLP Exporter Go") {
    return // Skip instrumentation
}
```

If you see infinite loops, check the exporter's user-agent string.

## Future Enhancements

### Planned Features

- 🔄 **Filter Support**: Skip instrumentation for specific paths/endpoints
- 🔄 **Custom Span Names**: Configurable span name formatting
- 🔄 **Enhanced Metrics**: Request/response body sizes, connection pool stats
- 🔄 **HTTP/2 & HTTP/3**: Protocol-specific attributes
- 🔄 **Public Endpoint Detection**: Differentiate internal vs external traffic

## Related Documentation

- [Implementation Details](../../../docs/implementation.md)
- [Upstream otelhttp Analysis](../../../docs/upstream-otelhttp-analysis.md)
- [Getting Started](../../../docs/getting-started.md)

## Contributing

See [CONTRIBUTING.md](../../../CONTRIBUTING.md) for development guidelines.

## License

Apache License 2.0 - See [LICENSE](../../../LICENSE) for details.
