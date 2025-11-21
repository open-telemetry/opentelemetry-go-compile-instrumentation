# Multi-stage Dockerfile for HTTP server with OpenTelemetry compile-time instrumentation
# Stage 1: Build the otel tool
FROM golang:1.25-alpine@sha256:d3f0cf7723f3429e3f9ed846243970b20a2de7bae6a5b66fc5914e228d831bbb AS otel-builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Copy the entire project to build the otel tool
COPY go.mod go.sum ./
COPY . .

# Build the otel tool
RUN go build -o /otel ./tool/cmd

# Stage 2: Build the HTTP server with instrumentation
FROM golang:1.25-alpine@sha256:d3f0cf7723f3429e3f9ed846243970b20a2de7bae6a5b66fc5914e228d831bbb AS app-builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy the otel tool from previous stage
COPY --from=otel-builder /otel /usr/local/bin/otel

# Copy HTTP server source
COPY demo/http/server /app

# Download dependencies
RUN go mod download

# Build the app with otel instrumentation
RUN otel go build -o server .

# Stage 3: Runtime image
FROM alpine:3.22@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412

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
EXPOSE 8080

# Environment variables for OpenTelemetry
ENV OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317 \
    OTEL_EXPORTER_OTLP_PROTOCOL=grpc \
    OTEL_SERVICE_NAME=http-server \
    OTEL_RESOURCE_ATTRIBUTES="service.namespace=demo,service.version=1.0.0" \
    OTEL_LOG_LEVEL=info

ENTRYPOINT ["./server"]
CMD ["-port", "8080"]
