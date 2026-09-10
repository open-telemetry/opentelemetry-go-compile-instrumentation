# rabbitmq/amqp091-go instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/rabbitmq/amqp091-go`](https://github.com/rabbitmq/amqp091-go).

| Target | Span | Notes |
| --- | --- | --- |
| `(*Channel).PublishWithDeferredConfirm` | `{exchange} send` | Covers `Publish`, `PublishWithContext`, and `PublishWithDeferredConfirmWithContext`. |
| `(*Channel).Consume` / `ConsumeWithContext` | `{queue} process` or `{queue} receive` | The delivery channel is replaced. Manual-ack spans end on `Delivery.Ack` / `Nack` / `Reject`. Auto-ack spans end when the Delivery is read. |
| `(*Channel).Get` | same as Consume | One delivery. |

W3C `traceparent` is injected into `Publishing.Headers` and extracted from `Delivery.Headers`.

`PublishWithContext`'s context is dropped by the library before `PublishWithDeferredConfirm`. The send span parents from the GLS span (inbound HTTP or gRPC) when one exists. Publisher confirms do not extend the send span.

Span names use `{exchange} send` and `{queue} process|receive`. The routing key is `messaging.rabbitmq.destination.routing_key` only: keys such as `order.{id}` must not enter the span name. Empty exchange is `(default)`, not `amq.default`, so the name stays stable and readable. Official `{exchange}:{routing key}` destination names are not used for the same cardinality reason.

`(*Channel).connection` is unexported, so these spans do not set `server.address`. `Channel.Ack` / `Channel.Nack` bypass `Delivery` and do not end process spans. `Delivery.Ack(multiple=true)` ends only that Delivery's span.

Each `Consume` starts one extra goroutine that reads the library channel and writes a wrapped one. Closing the consume channel ends leftover process spans.

This instrumentation is trace-only. Ack/nack metrics are a follow-up.

### Enable / disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=amqp
export OTEL_GO_DISABLED_INSTRUMENTATIONS=amqp
```

Instrumentation key: `AMQP` (case-insensitive).

## Supported versions

- Module: `github.com/rabbitmq/amqp091-go`
- Minimum bound: **v1.10.0** (`ConsumeWithContext`).

## Tests

```bash
go test -C instrumentation/github.com/rabbitmq/amqp091-go ./...

# Integration (requires: make build; Docker)
go -C test test -tags=integration -run TestAmqpClient ./integration/
```
