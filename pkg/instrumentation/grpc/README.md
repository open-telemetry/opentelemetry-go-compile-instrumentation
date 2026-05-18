# gRPC Compile-Time Instrumentation

This package provides automatic OpenTelemetry instrumentation for `google.golang.org/grpc` using compile-time code injection and the upstream `otelgrpc` stats handlers.

## Overview

Unlike traditional gRPC instrumentation that requires manually adding interceptors or stats handlers, this package automatically instruments **all** gRPC traffic in your application at compile-time. Zero code changes required!

### Key Features

✅ **Zero Code Changes**: Automatic instrumentation without modifying application code
✅ **Universal Coverage**: Instruments ALL gRPC calls, including internal services
✅ **W3C Trace Context**: Automatic context propagation via gRPC metadata
✅ **Semantic Conventions**: Uses upstream `otelgrpc` RPC semantic conventions
✅ **Client & Server**: Complete instrumentation for both gRPC clients and servers
✅ **Status Code Capture**: Accurate gRPC status code tracking
✅ **Error Recording**: Automatic error span status on failures
✅ **Metrics Collection**: Duration and message size metrics from `otelgrpc`
✅ **Dual API Support**: Both modern (`NewClient`) and legacy (`DialContext`) client APIs

## How It Works

### Compile-Time Injection

The instrumentation is injected during the build process:

```
┌─────────────────────────────────────────────┐
│  1. go build (with our toolexec)            │
│                                             │
│  2. Setup Phase:                            │
│     - Scan dependencies                     │
│     - Match google.golang.org/grpc          │
│     - Generate otelc.runtime.go              │
│                                             │
│  3. Instrument Phase:                       │
│     - Inject trampolines into:              │
│       • grpc.NewServer                      │
│       • grpc.NewClient                      │
│       • grpc.DialContext                    │
│                                             │
│  4. Build with instrumentation baked in     │
└─────────────────────────────────────────────┘
```

### Runtime Execution

When your application runs, the injected hooks automatically:

**For gRPC Servers** (`grpc.NewServer`):

1. **Before**: Inject stats.Handler into server options
2. **otelgrpc Stats Handler**:
   - `TagRPC`: Extract trace context, create server span
   - `HandleRPC`: End span, record status, collect metrics
3. **Result**: Fully instrumented gRPC server

**For gRPC Clients** (`grpc.NewClient` / `grpc.DialContext`):

1. **Before**: Inject stats.Handler into dial options
2. **otelgrpc Stats Handler**:
   - `TagRPC`: Create client span, inject trace context into metadata
   - `HandleRPC`: End span, record status, collect metrics
3. **Result**: Fully instrumented gRPC client

## Usage

### Building Your Application

```bash
# Build with automatic instrumentation
/path/to/otelc go build -a

# Run your application normally
./myapp
```

That's it! All gRPC traffic is now instrumented.

### Configuration

Control instrumentation behavior at runtime:

```bash
# Enable only specific instrumentations (comma-separated list)
export OTEL_GO_ENABLED_INSTRUMENTATIONS=grpc,nethttp

# Disable specific instrumentations (comma-separated list)
export OTEL_GO_DISABLED_INSTRUMENTATIONS=grpc

# General OpenTelemetry configuration
export OTEL_SERVICE_NAME=my-service
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
export OTEL_LOG_LEVEL=debug  # debug, info, warn, error
```

## Semantic Conventions

The instrumentation delegates span and metric production to upstream [`otelgrpc`](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc). Attribute names and metric instruments therefore follow the semantic convention mode supported by that package.

### Client Span Attributes

| Attribute | Example | Description |
|-----------|---------|-------------|
| `rpc.system.name` | `grpc` | RPC system identifier |
| `rpc.method` | `myapp.UserService/GetUser` | Full RPC method name |
| `rpc.response.status_code` | `OK` | Canonical gRPC status code |
| `server.address` | `api.example.com` | Server host |
| `server.port` | `50051` | Server port |

### Server Span Attributes

| Attribute | Example | Description |
|-----------|---------|-------------|
| `rpc.system.name` | `grpc` | RPC system identifier |
| `rpc.method` | `myapp.UserService/CreateUser` | Full RPC method name |
| `rpc.response.status_code` | `OK` | Canonical gRPC status code |
| `client.address` | `192.168.1.100` | Client IP address |
| `client.port` | `54321` | Client port |

### Metrics

**Duration**:

- `rpc.client.call.duration` - Client RPC duration
- `rpc.server.call.duration` - Server RPC duration

**Message Sizes**:

- `rpc.client.request.size` - Outbound message size
- `rpc.client.response.size` - Inbound message size
- `rpc.server.request.size` - Inbound message size
- `rpc.server.response.size` - Outbound message size

### Span Names

**Client**: `<package.Service>/<Method>` (e.g., `myapp.UserService/GetUser`)
**Server**: `<package.Service>/<Method>` (e.g., `myapp.UserService/CreateUser`)

### Span Status

- **OK**: gRPC status code 0 (OK), 1 (Canceled), 3 (InvalidArgument), etc.
- **ERROR**: Status codes indicating server errors (Unknown, DeadlineExceeded, Unimplemented, Internal, Unavailable, DataLoss)

Status mapping is handled by upstream `otelgrpc`.

## Examples

### Example 1: gRPC Client

Your code (no changes):

```go
package main

import (
    "context"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func main() {
    // Using modern API (v1.63+)
    conn, err := grpc.NewClient(
        "localhost:50051",
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        panic(err)
    }
    defer conn.Close()

    // Create client and make RPC calls
    client := pb.NewGreeterClient(conn)
    resp, err := client.SayHello(context.Background(), &pb.HelloRequest{Name: "World"})
    // ... handle response
}
```

What happens automatically:

1. Stats handler injected into dial options
2. Span created: `helloworld.Greeter/SayHello`
3. Trace context injected into gRPC metadata
4. Attributes recorded: RPC system, RPC method, status, and server address
5. Metrics collected: duration, message sizes
6. Span ended after response received

### Example 2: gRPC Server

Your code (no changes):

```go
package main

import (
    "context"
    "google.golang.org/grpc"
    "net"
)

type server struct {
    pb.UnimplementedGreeterServer
}

func (s *server) SayHello(ctx context.Context, req *pb.HelloRequest) (*pb.HelloReply, error) {
    return &pb.HelloReply{Message: "Hello " + req.Name}, nil
}

func main() {
    lis, _ := net.Listen("tcp", ":50051")
    s := grpc.NewServer()
    pb.RegisterGreeterServer(s, &server{})
    s.Serve(lis)
}
```

What happens automatically:

1. Stats handler injected into server options
2. Trace context extracted from incoming metadata
3. Span created: `helloworld.Greeter/SayHello`
4. Attributes recorded: RPC system, RPC method, status, and client address
5. Metrics collected: duration, message sizes
6. Span ended after handler completes

### Example 3: Legacy Client API

For applications using the older `DialContext` API (before v1.63):

```go
// Works automatically with legacy API too!
conn, err := grpc.DialContext(
    ctx,
    "localhost:50051",
    grpc.WithInsecure(),
    grpc.WithBlock(),
)
```

Both `NewClient` and `DialContext` are instrumented identically.

### Example 4: Distributed Tracing

**Service A (Client)**:

```go
conn, _ := grpc.NewClient("service-b:50051", grpc.WithInsecure())
client := pb.NewUserServiceClient(conn)
resp, _ := client.GetUser(ctx, &pb.GetUserRequest{Id: "123"})
```

**Service B (Server)**:

```go
func (s *server) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.User, error) {
    // Trace context automatically propagated!
    // This span will be a child of Service A's span
    return &pb.User{Id: req.Id, Name: "Alice"}, nil
}
```

Trace visualization in Jaeger:

```
Service A: myapp.UserService/GetUser [CLIENT]
  └─> Service B: myapp.UserService/GetUser [SERVER]
```

### Example 5: Streaming RPCs

Both unary and streaming RPCs are automatically instrumented:

```go
// Client streaming
stream, _ := client.RecordRoute(ctx)
for _, point := range points {
    stream.Send(point)  // Each message recorded in metrics
}
resp, _ := stream.CloseAndRecv()

// Server streaming
stream, _ := client.ListFeatures(ctx, &pb.Rectangle{})
for {
    feature, err := stream.Recv()
    if err == io.EOF {
        break
    }
    // Each message recorded in metrics
}
```

## Testing

```bash
# Run all tests
make test

# Run integration tests
make test-integration

# Run e2e tests
make test-e2e
```

## Troubleshooting

### Instrumentation Not Working

**Check 1: Is instrumentation enabled?**

```bash
# Make sure grpc is not in the disabled list
unset OTEL_GO_DISABLED_INSTRUMENTATIONS
# Or explicitly enable it
export OTEL_GO_ENABLED_INSTRUMENTATIONS=grpc
```

**Check 2: Was the app built with the otelc tool?**

```bash
/path/to/otelc go build -a
```

**Check 3: Check logs**

```bash
export OTEL_LOG_LEVEL=debug
./myapp
# Look for "gRPC server/client instrumentation initialized"
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

### No Metrics Being Recorded

Metrics are created by upstream `otelgrpc`. Ensure your metrics pipeline is configured:

```bash
export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://localhost:4317
```

### Status Code Mapping

gRPC status codes are mapped to OTel span status by upstream `otelgrpc` according to semantic conventions:

**Server Errors** (span.SetStatus Error):

- Code 2 (Unknown)
- Code 4 (DeadlineExceeded)
- Code 12 (Unimplemented)
- Code 13 (Internal)
- Code 14 (Unavailable)
- Code 15 (DataLoss)

**Client Errors** (span.SetStatus Error):

- All non-OK codes (1-16)

See the upstream `otelgrpc` package for complete mapping logic.

## Comparison with Manual Instrumentation

### Without Compile-Time Instrumentation

Manual instrumentation requires modifying code:

```go
import "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

// Server
s := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()),  // Manual addition
)

// Client
conn, _ := grpc.NewClient(
    target,
    grpc.WithStatsHandler(otelgrpc.NewClientHandler()),  // Manual addition
)
```

### With Compile-Time Instrumentation

Zero code changes:

```go
// Server
s := grpc.NewServer()  // Automatically instrumented

// Client
conn, _ := grpc.NewClient(target)  // Automatically instrumented
```

**Benefits**:

- ✅ No code changes needed
- ✅ Instruments ALL gRPC services (including dependencies)
- ✅ Can't forget to add instrumentation
- ✅ Centralized configuration
- ✅ Easy to enable/disable at runtime

## Related Documentation

- [Implementation Details](../../../docs/api-design-and-project-structure.md)
- [Semantic Conventions](../../../docs/semantic-conventions.md)
- [Getting Started](../../../README.md)

## References

- [Upstream otelgrpc](https://github.com/open-telemetry/opentelemetry-go-contrib/tree/main/instrumentation/google.golang.org/grpc/otelgrpc)
- [OTel RPC Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/rpc/)
- [gRPC Stats Handler](https://pkg.go.dev/google.golang.org/grpc/stats)

## Contributing

See [CONTRIBUTING.md](../../../CONTRIBUTING.md) for development guidelines.

## License

Apache License 2.0 - See [LICENSE](../../../LICENSE) for details.
