# Multi-stage Dockerfile for gRPC client with OpenTelemetry compile-time instrumentation
# Stage 1: Build the otelc tool
FROM golang:1.26-alpine@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1 AS otelc-builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Copy the entire project to build the otelc tool
COPY go.mod go.sum ./
COPY . .

# Build the otelc tool
RUN go build -o /otelc ./tool/cmd

# Stage 2: Build the gRPC client with instrumentation
FROM golang:1.26-alpine@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1 AS app-builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy the otelc tool from previous stage
COPY --from=otelc-builder /otelc /usr/local/bin/otelc

# Copy both client and server to maintain the go.mod replace directive structure
COPY demo/app/grpc/server /server
COPY demo/app/grpc/client /app

# Download dependencies
RUN go mod download

# Build the app with otelc instrumentation
RUN otelc go build -o client .

# Stage 3: Runtime image
FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

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
    OTEL_SERVICE_NAME=grpc-client \
    OTEL_RESOURCE_ATTRIBUTES="service.namespace=demo,service.version=1.0.0" \
    OTEL_LOG_LEVEL=info

ENTRYPOINT ["./client"]
