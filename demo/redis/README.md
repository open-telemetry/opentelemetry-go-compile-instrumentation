# Redis Demo

This directory contains a Redis client implementation for demonstrating OpenTelemetry compile-time instrumentation with `go-redis/v9`.

## Structure

- `client/` - Redis client implementation
  - `main.go` - Client code with multiple Redis operations (strings, hashes, lists, pipelines)

## Prerequisites

- Go 1.24.0 or higher
- A running Redis server (default: `localhost:6379`)

## Building

```bash
cd client
go mod tidy
go build -o client .
```

## Running

### Start a Redis Server

You can use Docker to quickly start a Redis server:

```bash
docker run -d --name redis -p 6379:6379 redis:7-alpine
```

### Run the Client

#### Basic Usage

```bash
cd client
./client
# Runs one iteration of all Redis operations (strings, hashes, lists, pipeline)
```

#### Multiple Iterations

```bash
./client -count=10
# Runs 10 iterations with 500ms delay between each
```

#### Custom Options

```bash
# Connect to a different Redis address
./client -addr=redis.example.com:6380

# Use a specific database index
./client -db=1

# Provide a password
./client -password=mysecret

# Set log level (debug, info, warn, error; default: info)
./client -log-level=debug

# Combine options
./client -addr=localhost:6380 -db=2 -count=5 -log-level=debug
```

## Operations

The demo client performs the following Redis operations in each iteration:

### String Commands
- **SET** - Set a key with a value and TTL
- **GET** - Retrieve the value of a key
- **EXISTS** - Check if a key exists
- **EXPIRE** - Set an expiration on a key
- **TTL** - Get the remaining time to live of a key
- **DEL** - Delete a key

### Hash Commands
- **HSET** - Set multiple hash fields
- **HGET** - Get a specific hash field
- **HGETALL** - Get all fields and values
- **HDEL** - Delete a hash field

### List Commands
- **LPUSH** - Push values to the head of a list
- **LLEN** - Get the length of a list
- **RPOP** - Remove and return the last element
- **LRANGE** - Get a range of elements

### Pipeline
- Executes multiple SET and GET commands in a single pipeline for batched operations

## Structured Logging

The client uses Go's structured logging (`log/slog`) with JSON output:

```json
{
  "time": "2025-11-04T15:42:06.495367+01:00",
  "level": "INFO",
  "msg": "SET",
  "key": "demo:1:greeting",
  "value": "Hello OpenTelemetry #1"
}
```

## Docker Compose

The Redis demo is integrated into the full observability stack. See `demo/infrastructure/docker-compose/` for running with Jaeger, Prometheus, Grafana, and the OTel Collector:

```bash
cd demo/infrastructure/docker-compose
docker compose up -d redis redis-client
```
