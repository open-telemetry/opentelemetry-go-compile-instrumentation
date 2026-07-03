# Multi-stage Dockerfile for DB server with OpenTelemetry compile-time instrumentation
# Stage 1: Build the otelc tool
FROM golang:1.26-alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS otelc-builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Copy the entire project to build the otelc tool
COPY go.mod go.sum ./
COPY . .

# Build the otelc tool
RUN go build -o /otelc ./tool/cmd/otelc

# Stage 2: Build the DB server with instrumentation
FROM golang:1.26-alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS app-builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy the otelc tool from previous stage
COPY --from=otelc-builder /otelc /usr/local/bin/otelc

# Copy DB server source
COPY demo/app/db/server /app

# Download dependencies
RUN go mod download

# Build the app with otelc instrumentation
RUN otelc go build -o server .

# Stage 3: Runtime image
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

# Install ca-certificates for TLS and timezone data
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy the instrumented binary
COPY --from=app-builder /app/server .

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser && \
    chown -R appuser:appuser /app

USER appuser

# Expose HTTP port
EXPOSE 8081

# Environment variables for OpenTelemetry
ENV OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317 \
    OTEL_EXPORTER_OTLP_PROTOCOL=grpc \
    OTEL_SERVICE_NAME=db-server \
    OTEL_RESOURCE_ATTRIBUTES="service.namespace=demo,service.version=1.0.0" \
    OTEL_LOG_LEVEL=info

ENTRYPOINT ["./server"]
CMD ["-port", "8081"]
