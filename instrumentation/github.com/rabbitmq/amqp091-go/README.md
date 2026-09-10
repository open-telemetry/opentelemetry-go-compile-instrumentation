# rabbitmq/amqp091-go instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/rabbitmq/amqp091-go`](https://github.com/rabbitmq/amqp091-go).

| Target | Span | Notes |
| --- | --- | --- |
| `(*Channel).PublishWithDeferredConfirm` | `{exchange} send` | Covers `Publish`, `PublishWithContext`, and `PublishWithDeferredConfirmWithContext`. |
| `(*Channel).Consume` / `ConsumeWithContext` | `{queue} process` or `{queue} receive` | The delivery channel is replaced. Manual-ack spans end on `Delivery` or `Channel` Ack / Nack / Reject, including `multiple=true`. Auto-ack spans end when the Delivery is read (server already settled). |
| `(*Channel).Get` | same as Consume | One delivery. |

W3C `traceparent` is injected into a **copy** of `Publishing.Headers` and extracted from `Delivery.Headers`. The caller's header map is not mutated.

`PublishWithContext`'s context is dropped by the library before `PublishWithDeferredConfirm`. The WithContext before-hooks stash that context so the send span parents from it. If the API context has no span, the GLS span (inbound HTTP or gRPC) is used. Publisher confirms do not extend the send span.

Span names use `{exchange} send` and `{queue} process|receive`. The routing key is `messaging.rabbitmq.destination.routing_key` only: keys such as `order.{id}` must not enter the span name. Empty exchange is `(default)`, not `amq.default`, so the name stays stable and readable. Official `{exchange}:{routing key}` destination names are not used for the same cardinality reason.

`(*Channel).connection` is unexported, so these spans do not set `server.address`.

Auto-ack uses operation `receive` and ends when the Delivery is taken from the channel. The server has already acknowledged the message, so there is no later Ack. That read is the finish indicator. A process span held until the consume channel closes would include idle time and would be the wrong duration.

Successful `Nack` / `Reject` end the process span as Unset. They are finish signals, not failed operations. A failed inner call still records `codes.Error`.

Each `Consume` starts one extra goroutine that reads the library channel and writes a wrapped one. Closing the consume channel ends leftover process spans.

This instrumentation is trace-only. Ack/nack/sent metrics are a follow-up.

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
