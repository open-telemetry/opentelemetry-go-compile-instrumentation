# Multi-stage Dockerfile for DB client with OpenTelemetry compile-time instrumentation
# Stage 1: Build the otelc tool
FROM golang:1.26-alpine@sha256:c2a1f7b2095d046ae14b286b18413a05bb82c9bca9b25fe7ff5efef0f0826166 AS otelc-builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Copy the entire project to build the otelc tool
COPY go.mod go.sum ./
COPY . .

# Build the otelc tool
RUN go build -o /otelc ./tool/cmd

# Stage 2: Build the DB client with instrumentation
FROM golang:1.26-alpine@sha256:c2a1f7b2095d046ae14b286b18413a05bb82c9bca9b25fe7ff5efef0f0826166 AS app-builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy the otelc tool from previous stage
COPY --from=otelc-builder /otelc /usr/local/bin/otelc

# Copy DB client source
COPY demo/app/db/client /app

# Download dependencies
RUN go mod download

# Build the app with otelc instrumentation
RUN otelc go build -o client .

# Stage 3: Runtime image
FROM alpine:3.23@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

# Install ca-certificates for TLS and timezone data
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy the instrumented binary
COPY --from=app-builder /app/client .

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser && \
    chown -R appuser:appuser /app

USER appuser

# Environment variables for OpenTelemetry
ENV OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317 \
    OTEL_EXPORTER_OTLP_PROTOCOL=grpc \
    OTEL_SERVICE_NAME=db-client \
    OTEL_RESOURCE_ATTRIBUTES="service.namespace=demo,service.version=1.0.0" \
    OTEL_LOG_LEVEL=info

ENTRYPOINT ["./client"]
CMD ["-addr", "http://db-server:8081", "-count", "10000"]
