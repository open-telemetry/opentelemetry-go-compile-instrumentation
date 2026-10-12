# IBM/sarama instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/IBM/sarama`](https://github.com/IBM/sarama), the Kafka client used
across the Go ecosystem (formerly Shopify/sarama).

`NewClient` is the single construction path shared by `NewSyncProducer`,
`NewAsyncProducer`, `NewConsumer` and `NewConsumerGroup`. Hooking it once
appends the OpenTelemetry interceptors to the shared `*sarama.Config`, so every
producer and consumer built from that config is traced automatically.

| Target | Span name | Attributes |
| --- | --- | --- |
| `NewClient` → `Producer.Interceptors` (`OnSend`) | `{topic} send` | `messaging.system=kafka`, `messaging.operation.name=send`, `messaging.destination.name`, `messaging.kafka.message.key`, `messaging.message.body.size`, `server.address`/`server.port`, `messaging.kafka.async` |
| `NewClient` → `Consumer.Interceptors` (`OnConsume`) | `{topic} receive` | `messaging.system=kafka`, `messaging.operation.name=receive`, `messaging.destination.name`, `messaging.destination.partition.id`, `messaging.kafka.offset`, `messaging.kafka.message.key`, `messaging.message.body.size`, `server.address`/`server.port` |

The `OnSend` and `OnConsume` interceptor callbacks used here cannot observe
broker acknowledgment or application-handler completion:

- **Producer spans** are created and ended inside `OnSend`, when the message is
  first handed to the dispatcher. They reflect local hand-off to the client
  library, not confirmed delivery — `messaging.kafka.async` marks this so the
  span duration is not read as a broker round-trip. A producer span is a root
  span unless the application puts a `context.Context` in
  `ProducerMessage.Metadata`, which is then used as its parent.
- **Consumer spans** are created and ended inside `OnConsume`, when a fetched
  message is delivered to the `Messages` channel. They do not cover the
  application handler's processing time.

The trace context is injected into the message headers on the producer side and
extracted on the consumer side, so a producer and its consumer on the same
topic are linked across processes when both are instrumented with
OpenTelemetry (this package or any other, e.g. the Java agent).

### Enable / disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=sarama     # allow-list mode
export OTEL_GO_DISABLED_INSTRUMENTATIONS=sarama    # turn off only this library
```

Instrumentation key: `SARAMA` (case-insensitive).

## Supported versions

- Module: `github.com/IBM/sarama`
- Verified against: **v1.61.1** (uses only the long-standing
  `Config.Producer.Interceptors` / `Config.Consumer.Interceptors` extension
  points and the `NewClient` constructor).

## Tests

```bash
go -C instrumentation/github.com/IBM/sarama test ./...
```
