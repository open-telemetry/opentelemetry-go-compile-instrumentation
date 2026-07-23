# otelc semantic-convention registry

This directory is an [OTel Weaver](https://github.com/open-telemetry/weaver)
schema registry that describes the telemetry the compile-time instrumentations
in this project emit. It is the machine-readable, CI-validated contract of "what
otelc produces".

```
schemas/otelc/
├── registry_manifest.yaml   # registry metadata + pinned upstream semconv dependency
├── groups/                  # one file per instrumentation (metrics, spans, attributes)
│   ├── http.yaml            # net/http client & server metrics
│   ├── grpc.yaml            # google.golang.org/grpc client & server metrics + spans
│   ├── database-sql.yaml    # database/sql client spans
│   ├── redis.yaml           # redis/go-redis (v9) client spans
│   ├── kafka.yaml           # segmentio/kafka-go producer & consumer spans
│   ├── k8s.yaml             # k8s.io/client-go informer spans
│   ├── openai.yaml          # openai/openai-go GenAI client spans
│   ├── anthropic.yaml       # anthropics/anthropic-sdk-go GenAI client spans
│   ├── mongo.yaml           # go.mongodb.org/mongo-driver client spans
│   ├── gin.yaml             # gin-gonic/gin server-span enrichment
│   ├── otel-sdk.yaml        # go.opentelemetry.io/otel* — Go runtime metrics
│   ├── logs.yaml            # log, log/slog, logrus — no telemetry (correlation only)
│   └── runtime.yaml         # runtime — no telemetry (GLS context propagation)
└── .deps/                   # pre-fetched upstream semconv (git-ignored, generated)
```

- Telemetry that comes from the upstream OpenTelemetry semantic conventions is
  referenced with `ref:`.
- Telemetry that is specific to a library (not covered upstream) is declared
  locally with `id:`.

## Instrumentation coverage

Every instrumentation module — every `instrumentation/**/otelc.yaml` — is
declared here, so this table is the answer to "what does otelc emit for library
X". Modules that emit no telemetry of their own still get a file, with
`groups: []` and a comment saying why; a missing row, not an empty one, is what
signals an undeclared instrumentation.

| Instrumentation module                              | Contract file       | Emits                                                                  |
| --------------------------------------------------- | ------------------- | ---------------------------------------------------------------------- |
| `net/http/client`, `net/http/server`                | `http.yaml`         | HTTP client + server metrics                                           |
| `google.golang.org/grpc/{client,server}`            | `grpc.yaml`         | RPC client + server metrics and spans                                  |
| `database/sql`                                      | `database-sql.yaml` | DB client spans                                                        |
| `github.com/redis/go-redis/v9`                      | `redis.yaml`        | DB client spans                                                        |
| `github.com/segmentio/kafka-go/{producer,consumer}` | `kafka.yaml`        | Messaging producer + consumer spans                                    |
| `k8s.io/client-go`                                  | `k8s.yaml`          | Informer spans                                                         |
| `github.com/openai/openai-go` (v1/v2/v3)            | `openai.yaml`       | GenAI client spans                                                     |
| `github.com/anthropics/anthropic-sdk-go`            | `anthropic.yaml`    | GenAI client spans                                                     |
| `go.mongodb.org/mongo-driver/mongo`                 | `mongo.yaml`        | DB client spans                                                        |
| `github.com/gin-gonic/gin`                          | `gin.yaml`          | `http.route` on the enclosing `net/http` server span                   |
| `go.opentelemetry.io/otel/init`                     | `otel-sdk.yaml`     | Go runtime metrics (`go.*`)                                            |
| `go.opentelemetry.io/otel`                          | `otel-sdk.yaml`     | nothing — guards the global tracer provider                            |
| `go.opentelemetry.io/otel/sdk/trace`                | `otel-sdk.yaml`     | nothing — maintains the GLS span chain                                 |
| `go.opentelemetry.io/otel/trace`                    | `otel-sdk.yaml`     | nothing — GLS fallback for `SpanFromContext`                           |
| `log`, `log/slog`, `github.com/sirupsen/logrus`     | `logs.yaml`         | nothing — injects `trace_id`/`span_id` into the application's own logs |
| `runtime`                                           | `runtime.yaml`      | nothing — goroutine-local context propagation                          |

## Validate locally

Requires [Docker](https://www.docker.com/) (or set `OCI_BIN=podman`) and `jq`.

```bash
make lint-schema
```

This fetches the pinned upstream semconv into `.deps/` and runs
`weaver registry check --future` against the registry.

See [`docs/semantic-conventions.md`](../../docs/semantic-conventions.md) for the
full workflow, including how to add a new instrumentation's telemetry.
