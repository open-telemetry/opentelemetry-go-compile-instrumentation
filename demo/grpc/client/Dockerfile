# Multi-stage Dockerfile for gRPC client with OpenTelemetry compile-time instrumentation
# Stage 1: Build the otel tool
FROM golang:1.23-alpine AS otel-builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Copy the entire project to build the otel tool
COPY go.mod go.sum ./
COPY . .

# Build the otel tool
RUN go build -o /otel ./cmd/otel

# Stage 2: Build the gRPC client with instrumentation
FROM golang:1.23-alpine AS app-builder

WORKDIR /app

# Install protobuf compiler and dependencies
RUN apk add --no-cache git protobuf-dev protoc

# Install protoc-gen-go and protoc-gen-go-grpc
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Copy the otel tool from previous stage
COPY --from=otel-builder /otel /usr/local/bin/otel

# Copy demo app source
COPY demo/grpc/client /app

# Generate protobuf code
RUN mkdir -p pb && \
    protoc --go_out=pb --go_opt=paths=source_relative \
           --go-grpc_out=pb --go-grpc_opt=paths=source_relative \
           greeter.proto

# Download dependencies
RUN go mod download

# Build the app with otel instrumentation
RUN otel go build -o client .

# Stage 3: Runtime image
FROM alpine:latest

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
