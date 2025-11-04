# HTTP Demo

This directory contains a simple HTTP server and client implementation for demonstrating OpenTelemetry compile-time instrumentation.

## Structure

- `server/` - HTTP server implementation
  - `main.go` - Server code with multiple HTTP handlers
- `client/` - HTTP client implementation
  - `main.go` - Client code with support for different HTTP methods

## Prerequisites

- Go 1.23.0 or higher

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
# Server will listen on port 8080 by default
# With 10% fault injection rate and up to 500ms random latency
```

#### Server Configuration Options

```bash
# Use a different port
./server -port=8081

# Adjust fault injection rate (0.0 to 1.0, default: 0.1)
./server -fault-rate=0.2

# Adjust maximum random latency in milliseconds (default: 500)
./server -max-latency=1000

# Disable fault injection
./server -no-faults

# Disable artificial latency
./server -no-latency

# Set log level (debug, info, warn, error; default: info)
./server -log-level=debug

# Combine options
./server -port=8081 -fault-rate=0.3 -max-latency=200 -log-level=debug
```

#### Fault Injection Types

The server randomly simulates the following error conditions based on the fault rate:

1. **Internal Server Error (500)** - Simulates server-side errors
2. **Service Unavailable (503)** - Simulates temporary unavailability
3. **Request Timeout (408)** - Simulates slow processing with a 5-second delay

### Run the Client

#### Simple GET Request

```bash
cd client
./client
# Output: Response: Hello world
# Note: May occasionally fail due to server fault injection
```

#### POST Request

```bash
./client -method=POST
# Sends a POST request with JSON payload
```

#### Multiple Requests

```bash
./client -count=5
# Sends 5 consecutive requests
# Useful for testing fault injection and latency patterns
```

#### Custom Options

```bash
# Connect to a different address
./client -addr=http://localhost:8081

# Send a custom name
./client -name="OpenTelemetry"

# Set log level (debug, info, warn, error; default: info)
./client -log-level=debug

# Combine options
./client -addr=http://localhost:8081 -name="Testing" -method=POST -count=3 -log-level=debug
```

## API Endpoints

The HTTP server provides the following endpoints:

1. **GET /greet** - Returns a simple greeting message
   - Query parameter: `name` (optional, default: "world")
2. **POST /greet** - Accepts a JSON payload with a name and returns a personalized greeting
3. **GET /health** - Health check endpoint (no fault injection or latency)

### Request/Response Formats

**GET /greet** request:

```bash
curl "http://localhost:8080/greet?name=world"
```

**POST /greet** request:

```json
{
  "name": "world"
}
```

**Success Response** (both endpoints):

```json
{
  "message": "Hello world"
}
```

**Error Response** (when faults are injected):

```json
{
  "error": "internal server error"
}
```

## Features

### Structured Logging with slog

Both server and client use Go's structured logging (`log/slog`) with JSON output for better observability:

```json
{
  "time": "2025-11-04T15:42:06.495367+01:00",
  "level": "INFO",
  "msg": "received request",
  "method": "GET",
  "name": "world-1",
  "path": "/greet",
  "status_code": 200,
  "duration_ms": 94
}
```

**Log Levels:**

- **debug**: Detailed information including artificial latency values and request creation
- **info**: Standard operational logs (requests, responses, configuration)
- **warn**: Fault injection events and server errors
- **error**: Request failures and critical errors

**Key Benefits:**

- Machine-readable JSON format
- Structured fields for easy parsing and filtering
- Request duration tracking
- Correlation between client and server logs

### Artificial Latency

The server adds random latency (0 to `max-latency` milliseconds) to simulate network delays and processing time. This is useful for testing timeout handling and performance monitoring.

At debug level, each latency injection is logged:

```json
{"level":"DEBUG","msg":"adding artificial latency","latency_ms":18}
```

### Client: Fault Injection

Random fault injection simulates real-world failure scenarios:

- **10% default rate** (configurable via `-fault-rate`)
- Three types of faults: 500, 503, and 408 errors
- Helps test error handling and retry logic in clients
- Can be disabled with `-no-faults` flag
- All faults are logged with structured context

### Client Resilience

The client:

- Handles error responses gracefully
- Continues processing remaining requests if one fails
- Tracks success/failure counts
- Logs detailed error information with structured fields
- Measures and logs request duration for performance analysis
