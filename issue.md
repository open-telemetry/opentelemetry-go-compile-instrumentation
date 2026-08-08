## Problem

Three `//go:build integration` propagation tests under `test/integration/` assert
parent/child span relationships but never assert that the *initiating* span is
actually the trace root. This is the same coverage gap that was fixed for the
`test/e2e/` suite in #963 (fixing #960) — it just wasn't fixed for the sibling
`test/integration/` suite, which has near-identical test bodies.

Without this assertion, a bug that gives the root span a bogus non-empty
`ParentSpanID` (e.g. a stray injected parent context, or a propagator that
incorrectly extracts a parent from an unrelated header) would slip through
these tests undetected, since only the *relative* parent/child links are
checked, never that the chain terminates.

The codebase already has precedent for this exact assertion style in
`test/integration/otel_sdk_test.go:46`:
```go
require.True(t, workerSpan.ParentSpanID().IsEmpty(), ...)
```

## Affected tests

- `test/integration/grpcserver_dbclient_test.go` — `grpcServerSpan` (the gRPC
  frontend) is the trace-initiating span (the test calls it directly via an
  uninstrumented `grpc.NewClient`), but nothing asserts it's the root.
- `test/integration/httpserver_dbclient_test.go` — same gap for
  `httpServerSpan`.
- `test/integration/ginserver_httpclient_test.go` — same gap for
  `ginServerSpan`.

(Note: unlike the `test/e2e/` versions, these `test/integration/` tests have
no separate instrumented client app — the test process itself makes the
initial call — so the root span here is the *server* span, not a client
span.)

## Proposed fix

Add one `require.True(t, <span>.ParentSpanID().IsEmpty(), "<span> must be the
trace root")` assertion to each of the three tests, following the exact
pattern used in #963 and in `otel_sdk_test.go`.
