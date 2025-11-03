# gRPC Demo

This directory contains a simple gRPC server and client implementation for demonstrating OpenTelemetry compile-time instrumentation.

## Structure

- `server/` - gRPC server implementation
  - `main.go` - Server code with unary and streaming RPC handlers (includes go:generate directives)
  - `greeter.proto` - Protocol buffer definitions
  - `generate.sh` - Alternative script to regenerate protobuf code
  - `pb/` - Generated protobuf and gRPC code
- `client/` - gRPC client implementation
  - `main.go` - Client code with support for both unary and streaming calls

## Prerequisites

- Go 1.23.0 or higher
- Protocol buffer compiler (protoc)
  - macOS: `brew install protobuf`
  - Linux: `apt-get install -y protobuf-compiler`
- Go plugins for protoc (already included in go.mod):
  - `google.golang.org/protobuf/cmd/protoc-gen-go`
  - `google.golang.org/grpc/cmd/protoc-gen-go-grpc`

## Building

### Server

```bash
cd server
go mod tidy
go build -o server .
```

### Client

```bash
cd client
go mod tidy
go build -o client .
```

## Running

### Start the Server

```bash
cd server
./server
# Server will listen on port 50051 by default
```

To use a different port:

```bash
./server -port=50052
```

### Run the Client

#### Unary RPC Call

```bash
cd client
./client
# Output: Greeting: Hello world
```

#### Streaming RPC Call

```bash
./client -stream=true
# Sends 5 messages and receives 5 responses
```

#### Custom Options

```bash
# Connect to a different address
./client -addr=localhost:50052

# Send a custom name
./client -name="OpenTelemetry"

# Combine options
./client -addr=localhost:50052 -name="Testing" -stream=true
```

## Regenerating Protocol Buffer Code

If you modify the `greeter.proto` file, regenerate the Go code using go generate (recommended):

```bash
cd server
go generate
```

Or use the provided script:

```bash
cd server
./generate.sh
```

Or manually:

```bash
cd server
mkdir -p pb
protoc --go_out=pb --go_opt=paths=source_relative \
       --go-grpc_out=pb --go-grpc_opt=paths=source_relative \
       greeter.proto
```

All methods will create the `pb/` directory (if it doesn't exist) and generate `greeter.pb.go` and `greeter_grpc.pb.go` in it.

## Service Definition

The gRPC service defines two methods:

1. **SayHello** - Unary RPC that accepts a name and returns a greeting
2. **SayHelloStream** - Bidirectional streaming RPC for multiple greetings

Both methods use the following message types:

- `HelloRequest` - Contains a name field
- `HelloReply` - Contains a message field with the greeting
