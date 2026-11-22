# NATS (github.com/nats-io/nats.go) Instrumentation

Compile-time OpenTelemetry instrumentation for [`github.com/nats-io/nats.go`](https://github.com/nats-io/nats.go).

## Overview

This module provides compile-time auto-instrumentation for applications using the NATS Go client. It intercepts message publishing, synchronous request-reply patterns, and asynchronous subscription handlers, extracting and injecting W3C trace context headers (`traceparent`) to deliver end-to-end distributed tracing across NATS-connected services.

| Target | Hook Point | Span Kind | Span Name | Description |
| --- | --- | --- | --- | --- |
| `(*nats.Conn).Publish` | Before / After | Producer | `{subject} send` | Traces publishing to a subject |
| `(*nats.Conn).PublishMsg` | Before / After | Producer | `{subject} send` | Injects W3C headers into `*nats.Msg` and traces publishing |
| `(*nats.Conn).Request` | Before / After | Producer | `{subject} send` | Traces synchronous request-reply |
| `(*nats.Conn).RequestWithContext` | Before / After | Producer | `{subject} send` | Traces synchronous request-reply using caller context |
| `(*nats.Conn).RequestMsg` | Before / After | Producer | `{subject} send` | Injects W3C headers into `*nats.Msg` and traces request |
| `(*nats.Conn).RequestMsgWithContext` | Before / After | Producer | `{subject} send` | Injects W3C headers into `*nats.Msg` and traces request |
| `(*nats.Conn).Subscribe` | Before | Consumer | `{subject} process` | Wraps `nats.MsgHandler` callback to extract trace context and record process spans |
| `(*nats.Conn).QueueSubscribe` | Before | Consumer | `{subject} process` | Wraps `nats.MsgHandler` for queue groups and records subscription queue name |

## Semantic Conventions

Telemetry conforms to OpenTelemetry Messaging Semantic Conventions (`semconv/v1.37.0`):

| Span Type | Name | Attributes |
| --- | --- | --- |
| Publish / Send | `{subject} send` | `messaging.system: "nats"`, `messaging.operation.name: "send"`, `messaging.operation.type: "send"`, `messaging.destination.name`, `messaging.message.body.size`, `server.address`, `server.port` |
| Process | `{subject} process` | `messaging.system: "nats"`, `messaging.operation.name: "process"`, `messaging.operation.type: "process"`, `messaging.destination.name`, `messaging.destination.subscription.name` (for queue subscribers), `messaging.message.body.size`, `server.address`, `server.port` |

On errors, the span records the error event and sets span status to `Error` with the error description.

## Context Propagation

- For `PublishMsg`, `RequestMsg`, and `RequestMsgWithContext`, W3C `traceparent` (and optional baggage) is injected into `*nats.Msg.Header`.
- For `Subscribe` and `QueueSubscribe`, the handler extracts incoming trace context from `msg.Header` and sets the incoming span context as the parent of the consumer process span.

## Enable / Disable

```bash
# Enable specifically
export OTEL_GO_ENABLED_INSTRUMENTATIONS=nats

# Disable specifically
export OTEL_GO_DISABLED_INSTRUMENTATIONS=nats
```

Instrumentation key: `NATS` (case-insensitive).

## Tests

```bash
go test -C instrumentation/github.com/nats-io/nats.go -v ./...
```
