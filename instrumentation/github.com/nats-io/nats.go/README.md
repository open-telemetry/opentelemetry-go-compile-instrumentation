# nats.go instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/nats-io/nats.go`](https://github.com/nats-io/nats.go), the official
Go client for [NATS](https://nats.io).

Every publish path — `Publish`, `PublishMsg`, `PublishRequest`, `Request`,
`RespondMsg` and the JetStream `Publish*` methods — funnels through the
unexported `(*Conn).publish` method. Hooking that single point emits one `send`
span per publish and appends the W3C trace context to the encoded NATS header
block, so the message carries it over the wire. Consuming is covered at the
callback boundary for async subscriptions and at `Subscription.NextMsg` for
sync pulls.

| Target | Span name | Attributes |
| --- | --- | --- |
| `(*Conn).publish` (before/after) | `{subject} send` | `messaging.system=nats`, `messaging.operation.name=send`, `messaging.destination.name`, `messaging.message.body.size`, `server.address`/`server.port` |
| `(*Conn).Subscribe` / `(*Conn).QueueSubscribe` (wrapped callback) | `{subject} process` | `messaging.system=nats`, `messaging.operation.name=process`, `messaging.destination.name`, `messaging.message.body.size`, `server.address`/`server.port` |
| `(*Subscription).NextMsg` (after) | `{subject} receive` | `messaging.system=nats`, `messaging.operation.name=receive`, `messaging.destination.name`, `messaging.message.body.size` |

The `send` span is created inside the internal publish call, before the client
hands the message to the connection's write loop, and ends immediately after; it
measures local hand-off, not server acknowledgment. Its parent is the active
span in goroutine-local storage — the natural propagation channel for this
client, whose publish APIs take no `context.Context` — falling back to a root
span.

The `process` span covers exactly the application callback's execution: the
instrumented callback extracts the trace context from the message headers,
starts the span, runs the application callback, and ends it. `NextMsg` spans
measure the fetch and end before the caller processes the message.

Trace context is injected at the byte level into the encoded NATS header block
(`NATS/1.0\r\n` …) only when the connected server advertises header support
(`Conn.HeadersSupported()`); an empty header block is upgraded to a header
publish, exactly like the community otelnats wrapper's `PublishMsg` approach.
The trace context therefore never appears in the application's own
`*nats.Msg.Header` after `PublishMsg` — it is observable by the consumer over
the wire. `ChanSubscribe` is intentionally not instrumented: replacing the
channel delivery would change the library's back-pressure and close semantics.

Subjects beginning with `$` are reserved for server control traffic
(`$SYS.*`, `$JS.API.*`, JetStream internal buckets) and produce no spans.

### Enable / disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=nats     # allow-list mode
export OTEL_GO_DISABLED_INSTRUMENTATIONS=nats    # turn off only this library
```

Instrumentation key: `NATS` (case-insensitive).

## Supported versions

- Module: `github.com/nats-io/nats.go`
- Verified against: **v1.54.0** (requires the `publish(subj, reply string,
  validateReply bool, hdr, data []byte)` signature, stable since **v1.48.0**).

## Tests

```bash
go -C instrumentation/github.com/nats-io/nats.go test ./...
```
