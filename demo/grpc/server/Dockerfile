# Multi-stage Dockerfile for gRPC server with OpenTelemetry compile-time instrumentation
# Stage 1: Build the otel tool
FROM golang:1.24-alpine@sha256:06545cc1ff10ddf04aebe20db1352ec7c96d1e43135767931c473557d0378202 AS otel-builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Copy the entire project to build the otel tool
COPY go.mod go.sum ./
COPY . .

# Build the otel tool
RUN go build -o /otel ./tool/cmd

# Stage 2: Build the gRPC server with instrumentation
FROM golang:1.24-alpine@sha256:06545cc1ff10ddf04aebe20db1352ec7c96d1e43135767931c473557d0378202 AS app-builder

WORKDIR /app

# Install protobuf compiler and dependencies
RUN apk add --no-cache git protobuf-dev protoc

# Install protoc-gen-go and protoc-gen-go-grpc
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.4 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

# Copy the otel tool from previous stage
COPY --from=otel-builder /otel /usr/local/bin/otel

# Copy demo app source
COPY demo/grpc/server /app

# Generate protobuf code
RUN mkdir -p pb && \
    protoc --go_out=pb --go_opt=paths=source_relative \
           --go-grpc_out=pb --go-grpc_opt=paths=source_relative \
           greeter.proto

# Download dependencies
RUN go mod download

# Build the app with otel instrumentation
RUN otel go build -o server .

# Stage 3: Runtime image
FROM alpine:3.23@sha256:51183f2cfa6320055da30872f211093f9ff1d3cf06f39a0bdb212314c5dc7375

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

# Expose gRPC port
EXPOSE 50051

# Environment variables for OpenTelemetry
ENV OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317 \
    OTEL_EXPORTER_OTLP_PROTOCOL=grpc \
    OTEL_SERVICE_NAME=grpc-server \
    OTEL_RESOURCE_ATTRIBUTES="service.namespace=demo,service.version=1.0.0" \
    OTEL_LOG_LEVEL=info

# Health check (if health endpoint is implemented)
# HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
#   CMD grpc_health_probe -addr=:50051 || exit 1

ENTRYPOINT ["./server"]
CMD ["-port", "50051"]
