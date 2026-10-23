# IBM/sarama instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/IBM/sarama`](https://github.com/IBM/sarama), the Kafka client used
across the Go ecosystem (formerly Shopify/sarama).

`NewClient` is the construction path shared by `NewSyncProducer`,
`NewAsyncProducer` and `NewConsumer`. Hooking it once appends the
OpenTelemetry interceptors to the `*sarama.Config`, so every producer and
consumer built from that config is traced automatically. `NewConsumerGroup`
and `NewConsumerGroupFromClient` are hooked separately so each consumer group
gets its own interceptor carrying `messaging.consumer.group.name`, even when
several groups share one `*Config` or one `Client`.

| Target | Span name | Attributes |
| --- | --- | --- |
| `NewClient` → `Producer.Interceptors` (`OnSend`), span ended at `returnSuccesses`/`returnError(s)` | `{topic} send` | `messaging.system=kafka`, `messaging.operation.name=send`, `messaging.destination.name`, `messaging.kafka.message.key`, `messaging.message.body.size`, `server.address`/`server.port` |
| `NewClient` → `Consumer.Interceptors` (`OnConsume`) | `{topic} receive` | `messaging.system=kafka`, `messaging.operation.name=receive`, `messaging.destination.name`, `messaging.destination.partition.id`, `messaging.kafka.offset`, `messaging.kafka.message.key`, `messaging.message.body.size`, `server.address`/`server.port` |

The `OnConsume` callback fires when a fetched message is delivered to the
`Messages` channel, so consumer spans are created and ended inside `OnConsume`
and do not cover the application handler's processing time.

Producer spans are **not** ended in `OnSend`:

- A span is opened on the message's first dispatch and the W3C trace context
  is injected into its headers then.
- It stays open until the broker result reaches sarama's
  `(*asyncProducer).returnSuccesses`, `returnErrors` or `returnError`, where
  it ends (with the error recorded on failure). Those are the terminal paths
  for every partition/broker producer, for both sync and async producers, so
  the span duration is the real broker round trip and `SendMessage` errors /
  partial batch failures are observable.
- The dispatcher re-runs producer interceptors at every retry level and also
  routes its internal `fin` control messages through the same interceptor
  loop. An [add_file](#add_file) compiled into sarama reports whether a
  dispatch is the first one (`msg.retries == 0 && msg.flags == 0`), so
  retries and control messages start no span and inject nothing. This does
  not rely on the presence of a `traceparent` header, which is unreliable with
  other propagators (e.g. B3) or pre-existing headers.

A producer span is a root span unless the application puts a
`context.Context` in `ProducerMessage.Metadata`, which is then used as its
parent. The trace context is injected into the message headers on the
producer side and extracted on the consumer side, so a producer and its
consumer on the same topic are linked across processes when both are
instrumented with OpenTelemetry (this package or any other, e.g. the Java
agent).

### add_file

`first_dispatch.go` carries a `//go:build ignore` tag in this repo and is
copied into `github.com/IBM/sarama` at instrumentation time. It registers a
predicate over two unexported `ProducerMessage` fields with this package via
`//go:linkname`, so the instrumented sarama package needs no import edge back
to the instrumentation module. The instrumentation package builds and tests
without it: when the predicate has not been registered, `OnSend` falls back
to pointer-keyed dedupe.

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
  points, the `NewClient` constructor and the `asyncProducer` return paths,
  all present since v1.40.0).

## Tests

```bash
go -C instrumentation/github.com/IBM/sarama test ./...
```
