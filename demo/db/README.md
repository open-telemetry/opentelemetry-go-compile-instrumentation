# Database (MySQL) Demo

This directory contains a demo HTTP server backed by MySQL and a client that exercises CRUD operations, demonstrating OpenTelemetry compile-time instrumentation for `database/sql` and `net/http`.

## Structure

- `server/` - HTTP server with MySQL backend
  - `main.go` - REST API for user CRUD operations (Create, Read, Update, Delete, Bulk Create with transactions)
- `client/` - HTTP client that runs CRUD cycles against the server

## What Gets Instrumented

The compile-time instrumentation automatically instruments:

- **`database/sql`** - All database operations (Open, Ping, Query, Exec, Prepare, BeginTx, Commit, Rollback) produce spans with DB semantic conventions
- **`net/http`** - Both the server HTTP handlers and the client HTTP requests produce spans with HTTP semantic conventions

This means a single request from the client generates a trace that includes:
1. HTTP client span (client making the request)
2. HTTP server span (server handling the request)
3. One or more DB client spans (server querying MySQL)

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check (pings MySQL) |
| POST | `/user/create` | Create a user |
| GET | `/users` | List all users (optional `?name=` filter) |
| GET | `/user?id=` | Get user by ID |
| PUT | `/user/update` | Update a user |
| DELETE | `/user/delete?id=` | Delete a user |
| POST | `/users/bulk` | Bulk create users (uses transaction) |

## Running with Docker Compose

From the `demo/infrastructure/docker-compose` directory:

```bash
docker compose up -d
```

This starts MySQL, the DB server, the DB client, and the full observability stack (Jaeger, Prometheus, Grafana, OTel Collector).

## Running Locally

### Prerequisites

- Go 1.24.0 or higher
- MySQL 8.0+ running locally

### Start MySQL

```bash
docker run -d --name mysql-demo \
  -e MYSQL_ROOT_PASSWORD=root \
  -e MYSQL_DATABASE=demodb \
  -e MYSQL_USER=demo \
  -e MYSQL_PASSWORD=demo \
  -p 3306:3306 \
  mysql:8.0
```

### Start the Server

```bash
cd server
go build -o server .
./server -dsn "demo:demo@tcp(localhost:3306)/demodb?parseTime=true"
```

### Run the Client

```bash
cd client
go build -o client .
./client -addr http://localhost:8081 -count 5
```

## Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | `8081` | HTTP server port |
| `-dsn` | `demo:demo@tcp(mysql:3306)/demodb?parseTime=true` | MySQL DSN |
| `-log-level` | `info` | Log level (debug, info, warn, error) |

## Client Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | `http://localhost:8081` | Server address |
| `-count` | `10` | Number of CRUD cycles |
| `-log-level` | `info` | Log level (debug, info, warn, error) |
