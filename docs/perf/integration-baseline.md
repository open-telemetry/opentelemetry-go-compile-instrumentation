# Integration test performance baseline

Issue #676 tracks reducing and controlling integration test wall-clock time so future test additions do not require CI timeout increases.

## CI baseline

Source: GitHub Actions run `29241290051` on `main`, queried with:

```sh
gh run view 29241290051 --repo open-telemetry/opentelemetry-go-compile-instrumentation \
  --json jobs -q '.jobs[]|select(.name|contains("Integration"))|{name,started:.startedAt,completed:.completedAt}'
```

| Job | Started | Completed | Duration |
| --- | --- | --- | --- |
| Coverage Integration Tests | 2026-07-13T10:02:16Z | 2026-07-13T10:14:31Z | 12m15s |
| Integration Tests (go windows-latest amd64) | 2026-07-13T10:02:16Z | 2026-07-13T10:20:13Z | 17m57s |
| Integration Tests (go ubuntu-latest amd64) | 2026-07-13T10:02:15Z | 2026-07-13T10:15:12Z | 12m57s |
| Integration Tests (go ubuntu-22.04-arm arm64) | 2026-07-13T10:02:19Z | 2026-07-13T10:11:24Z | 9m05s |
| Integration Tests (go macos-latest arm64) | 2026-07-13T10:02:16Z | 2026-07-13T10:08:37Z | 6m21s |

The Windows job is the limiting leg.

## Top-level test duration baseline

Per-test subtests complete in milliseconds. The top-level integration tests spend most of their runtime in `otelc go build -a`.

| Test | Duration |
| --- | ---: |
| TestVendoredBuild | 273.9s |
| TestAutoPin_RemovesGeneratedToolFile | 175.9s |
| TestRedisClient | 168.0s |
| TestMongoClient | 165.5s |
| TestDBClient | 162.4s |
| TestExplicitInstrumentationSelection | 157.2s |
| TestHTTPServer | 147.9s |
| Other top-level integration tests | ~130-140s |
| TestHTTPClient/basic subtest | 0.43s |

## Root cause

`test/testutil/infra.go` runs `otelc go build -a`, which forces recompilation of the standard library and the full dependency tree. `tool/internal/setup/setup.go` points each app at an empty app-local `.otelc-build/gocache`, so each app rebuilds from a cold cache.

## Planned measurements

- Phase 1: compare CI per-shard durations after workflow-level sharding.
- Phase 2: compare local and CI cold-vs-warm durations after per-app cache persistence.

## Local baseline

Local baseline command:

```sh
DOCKER_HOST=unix://$HOME/.colima/otel-issue-676/docker.sock \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
/usr/bin/time -p make test-integration 2>&1 | tee /tmp/baseline-local.log
```

The first run without explicit Docker socket environment failed in `TestKafkaClient` with
`panic: rootless Docker not found`; the explicit Colima socket makes testcontainers use the
isolated `otel-issue-676` Docker profile.

Successful local baseline:

- `real 487.74`
- `user 1169.11`
- `sys 460.70`
- Go package elapsed: `444.84s`

Selected top-level local durations from `gotest-integration.log`:

| Test | Duration |
| --- | ---: |
| TestK8SClient | 440.89s |
| TestKafkaClient | 378.17s |
| TestGRPCServer | 207.02s |
| TestMongoClient | 207.01s |
| TestGRPCClient | 206.68s |
| TestHTTPServer | 206.38s |
| TestOtelSDKSpanFromContext | 206.24s |
| TestExplicitInstrumentationSelection | 204.91s |
| TestAutoPin_RemovesGeneratedToolFile | 203.06s |
| TestHTTPClient | 42.98s |

## Local cache measurement

Command:

```sh
OTELC_TEST_GOCACHE=/tmp/otelc-test-gocache-phase2 \
DOCKER_HOST=unix://$HOME/.colima/otel-issue-676/docker.sock \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
/usr/bin/time -p make test-integration
```

Results:

| Run | real | Go package elapsed | Top-level tests passed |
| --- | ---: | ---: | ---: |
| Cold per-app cache | 261.12s | 226.018s | 24 |
| Warm per-app cache | 154.63s | 121.287s | 24 |

Targeted correctness checks passed with `OTELC_TEST_GOCACHE` set:

- `go -C test test -count=1 -tags integration -run '^TestExplicitInstrumentationSelection$' ./integration/...`
- `go -C test test -count=1 -tags integration -run '^(TestVendoredBuild|TestAutoPin_RemovesGeneratedToolFile)$' ./integration/...`
