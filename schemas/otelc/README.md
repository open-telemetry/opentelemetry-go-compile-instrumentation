# otelc semantic-convention registry

This directory is an [OTel Weaver](https://github.com/open-telemetry/weaver)
schema registry that describes the telemetry the compile-time instrumentations
in this project emit. It is the machine-readable, CI-validated contract of "what
otelc produces".

```
schemas/otelc/
├── registry_manifest.yaml   # registry metadata + pinned upstream semconv dependency
├── groups/                  # one file per instrumentation (metrics, spans, attributes)
│   └── http.yaml            # net/http client & server telemetry
└── .deps/                   # pre-fetched upstream semconv (git-ignored, generated)
```

- Telemetry that comes from the upstream OpenTelemetry semantic conventions is
  referenced with `ref:`.
- Telemetry that is specific to a library (not covered upstream) is declared
  locally with `id:`.

## Validate locally

Requires [Docker](https://www.docker.com/) (or set `OCI_BIN=podman`) and `jq`.

```bash
make lint-schema
```

This fetches the pinned upstream semconv into `.deps/` and runs
`weaver registry check --future` against the registry.

See [`docs/semantic-conventions.md`](../../docs/semantic-conventions.md) for the
full workflow, including how to add a new instrumentation's telemetry.
