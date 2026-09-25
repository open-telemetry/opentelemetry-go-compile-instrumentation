# rabbitmq/amqp091-go instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/rabbitmq/amqp091-go`](https://github.com/rabbitmq/amqp091-go).

Limits and attributes are documented here. The `hook.go` package comment
points at this file.

| Target | Span | Notes |
| --- | --- | --- |
| `(*Channel).PublishWithDeferredConfirm` | `{exchange} send` | Also covers `Publish`, `PublishWithContext`, and `PublishWithDeferredConfirmWithContext`. |
| `(*Channel).Consume` / `ConsumeWithContext` | `{queue} process` or `{queue} receive` | The after hook replaces the delivery channel. Manual-ack spans end on `Delivery` or `Channel` Ack, Nack, or Reject, including `multiple=true`. Auto-ack spans end when the Delivery is read. |
| `(*Channel).Get` | same as Consume | One delivery. If Get is never acked, the process span stays open until the channel is torn down. |
| `(*Channel).shutdown` | n/a | The single point every channel teardown path (explicit `Close`, a lost connection, or a server-initiated close) funnels through. Ends any process spans still awaiting acknowledgement on this channel (Consume or Get) and drops the channel's entries from the internal publish/ack tracking maps, so a torn-down channel is not held onto forever. |

The hook injects W3C `traceparent` into a copy of `Publishing.Headers` and
extracts it from `Delivery.Headers`. It does not change the caller's header
map. The copy is shallow. Only top-level keys are added.

The library drops the `PublishWithContext` context before
`PublishWithDeferredConfirm`. The WithContext before-hooks store that
context so the send span uses it as the parent. If that context has no span,
the send span uses the GLS span (inbound HTTP or gRPC).

`publishParents` holds one context per `*Channel`. Two goroutines calling
`PublishWithContext` on the same Channel can use each other's context as
the parent. amqp091-go already tells callers not to publish concurrently on
one Channel. Use one Channel per goroutine, or publish one at a time.

The send span ends when the client accepts the write. It does not wait for
a broker confirm. This hook does not set a `messaging.kafka.async`
attribute. RabbitMQ confirms use `Channel.Confirm`, which is a separate
optional API. Span duration is the client hand-off, not delivery.

Span names are `{exchange} send` and `{queue} process` or `{queue} receive`.
The routing key is only `messaging.rabbitmq.destination.routing_key`. Keys
such as `order.{id}` must not go in the span name. An empty exchange is
`(default)`, not `amq.default`. A real exchange named `(default)` looks the
same as the empty default exchange. Official `{exchange}:{routing key}`
destination names are not used for the same reason.

`(*Channel).connection` is unexported, so these spans do not set
`server.address`.

Auto-ack uses operation `receive` and ends when the Delivery is taken from
the channel. That duration is the read, not user processing. Holding a
process span until the consume channel closes would include idle time.

A successful `Nack` or `Reject` ends the process span as Unset. A failed
inner call still records `codes.Error`.

Each `Consume` starts one extra goroutine. The library delivery channel is
unbuffered. The wrapper is unbuffered too, so the consumer still blocks the
library the same way. Closing the consume channel ends leftover process
spans.

This instrumentation is trace-only. Ack, nack, and sent metrics are a
follow-up.

### Enable / disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=amqp
export OTEL_GO_DISABLED_INSTRUMENTATIONS=amqp
```

Instrumentation key: `AMQP` (case-insensitive).

## Supported versions

- Module: `github.com/rabbitmq/amqp091-go`
- Minimum bound: **v1.9.0** (`ConsumeWithContext` landed in v1.9.0).

## Tests

```bash
go test -C instrumentation/github.com/rabbitmq/amqp091-go ./...

# Integration (requires: make build; Docker)
go -C test test -tags=integration -run TestAmqpClient ./integration/
```
