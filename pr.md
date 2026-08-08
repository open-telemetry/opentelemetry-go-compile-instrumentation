## test(integration): assert server span is the trace root in propagation tests

Fixes #<issue-number-once-filed> (see `issue.md`)

### What

Adds a `require.True(t, <span>.ParentSpanID().IsEmpty(), ...)` assertion to
three `test/integration/` propagation tests, mirroring the fix already
applied to the `test/e2e/` suite in #963:

- `test/integration/grpcserver_dbclient_test.go` — asserts `grpcServerSpan`
  (the gRPC frontend) is the trace root.
- `test/integration/httpserver_dbclient_test.go` — asserts `httpServerSpan`
  is the trace root.
- `test/integration/ginserver_httpclient_test.go` — asserts `ginServerSpan`
  is the trace root.

### Why

These tests previously only asserted *relative* parent/child span links
(e.g. "SQL client's parent is the gRPC server"), never that the chain
actually terminates at the initiating span. A bug that attached a bogus
non-empty `ParentSpanID` to the root span (e.g. an incorrectly extracted
parent from an unrelated context/header) would pass unnoticed. The assertion
style matches existing precedent in `test/integration/otel_sdk_test.go:46`
and the `test/e2e/` fix from #963.

Unlike the `test/e2e/` versions of these tests, `test/integration/` has no
separate instrumented client app driving the call — the test process itself
is the (uninstrumented) client — so the root span here is the *server* span
rather than a client span.

### Test plan

- [x] `go build -tags=integration ./integration/...` (from `test/` module)
- [x] `go vet -tags=integration ./integration/...`
- [x] `gofmt -l` on changed files (clean)
- [ ] `go test -tags=integration ./integration/...` — requires Docker
      (Postgres/Kafka fixtures) not available in this environment; relies on
      CI's integration job to execute.

### Scope

Test-only change, 3 lines added across 3 files. No production code touched.
