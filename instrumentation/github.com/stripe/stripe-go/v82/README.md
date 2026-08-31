# stripe-go v82 instrumentation

Compile-time OpenTelemetry instrumentation for
[`github.com/stripe/stripe-go/v82`](https://github.com/stripe/stripe-go).

## What is instrumented

One hook, on one function:

| Layer | Target | Span name | Scope |
|-------|--------|-----------|--------|
| Backend request | `(*BackendImplementation).requestWithRetriesAndTelemetry` | `{METHOD} {url.template}` | `go.opentelemetry.io/otelc/instrumentation/github.com/stripe/stripe-go/v82` |

stripe-go exposes over 200 per-resource clients and thousands of exported
methods. Every one of them reaches the network through a single unexported
chokepoint on the backend:

```text
Call ─┐
CallRaw ────────┐
CallMultipart ──┤
CallStreaming ──┼─► requestWithRetriesAndTelemetry
RawRequest ─────┘
```

Hooking that one function covers the whole SDK: v1 and v2 API modes, the
deprecated `client.API` aggregate, and the current `stripe.Client`. It does not
need updating when Stripe adds a resource.

The span covers the backend's internal retry loop, so a request retried three
times produces one span. When net/http client instrumentation is also enabled,
each attempt appears as a child `RoundTrip` span carrying its own timing.

### Span naming and cardinality

Span names and metric labels use `url.template`, which is the request path with
resource identifiers replaced by `{id}`:

```text
/v1/customers/cus_NffrFeUfNV2Hib/sources/card_1Mvo → /v1/customers/{id}/sources/{id}
/v1/payment_intents                                → /v1/payment_intents
/v2/core/events/evt_1MvoiJ2eZvKYlo2C               → /v2/core/events/{id}
```

The real path stays on the span as `url.path`, so the specific resource is still
recoverable. It is never used as a metric label.

Templating is shape-based rather than a route table. Stripe generates IDs as a
lowercase prefix, an underscore, and an opaque token containing digits or
uppercase letters. That shape separates `cus_NffrFeUfNV2Hib` from resource names
such as `payment_intents` without needing a table. The known gap is IDs whose
value the user chooses, such as plan, coupon, product and SKU IDs
(`/v1/coupons/summer`). Those do not match the shape and pass through
untemplated. Closing the gap needs a route table generated from Stripe's OpenAPI
spec, because `summer` is indistinguishable from a resource name.

### Attributes

`http.request.method`, `url.template`, `url.path`, `server.address`,
`server.port`, `http.response.status_code`, and `error.type`, plus three
Stripe-specific attributes: `stripe.request_id` (the `Request-Id` value Stripe
support keys off), `stripe.error.code`, and `stripe.error.type`.

The full emission contract is in
[`schemas/otelc/groups/stripe.yaml`](../../../../../schemas/otelc/groups/stripe.yaml).

### Metrics

- `stripe.client.request.duration` (histogram, seconds): logical request
  latency, retries included.
- Labels (bounded only): `server.address`, `server.port`, `http.request.method`,
  `url.template`, `http.response.status_code`.
- Not labeled by `url.path` or `stripe.request_id`. Both are unbounded, so they
  stay on spans.

### Enable / disable

```bash
export OTEL_GO_ENABLED_INSTRUMENTATIONS=stripe   # allow-list mode
export OTEL_GO_DISABLED_INSTRUMENTATIONS=stripe  # turn off only this library
```

Instrumentation key: `STRIPE` (case-insensitive).

## Layering

`hook.go` owns the hook lifecycle and is the only file that imports stripe-go.
Attribute and span-name policy lives in `semconv/`, which depends only on the
standard library and OpenTelemetry. `*stripe.Error` and `*stripe.V2RawError` are
converted to the version-neutral `semconv.APIError` at the boundary. A new
stripe-go major version therefore re-points this one file instead of re-deriving
the conventions.

## Supported versions

- Module: `github.com/stripe/stripe-go/v82`
- Rules apply to **v82.0.0+**.

Stripe ships a new major version roughly quarterly, each on a new module path.
[ADR-0004](../../../../../docs/adr/0004-instrumentation-ownership-and-compatibility.md)
sets the policy at the last two majors, so v81 lives in a sibling directory with
the same chokepoint and an identical `semconv/`. Following the `openai-go/v2`,
`v3` precedent, each major carries its own copy of `semconv/` instead of
importing a shared one, so a v81 user never pulls stripe-go v82 into their
build. Adding v83 means copying this directory and re-pointing the import path
in `hook.go`.

## Known limitations

- **Retry count is not reported.** The span deliberately covers the backend's
  internal retry loop, but the attempt counter is local to
  `requestWithRetriesAndTelemetry` and unreachable from a hook. So
  `http.request.resend_count` is not set, and a request retried three times
  looks like a clean one on this span alone. Enable net/http client
  instrumentation to see the individual attempts as child spans.
- **The rule has no upper version bound.** The hooked function is unexported, so
  a rename in a future v82 minor would silently drop instrumentation rather than
  fail the build. `make test-latestlibbuild` is the guard. This is the cost of
  covering the whole SDK from one chokepoint instead of about 800 public
  methods.
- **User-defined resource IDs are not templated.** See the span-naming section
  above.

## Tests

```bash
# Unit
go test ./instrumentation/github.com/stripe/stripe-go/v82/...

# Integration (requires: make build)
go -C test test -tags=integration -run TestStripeClient ./integration/
```

The integration app lives in `test/apps/stripeclient`. It exercises a v1 POST, a
v1 GET by ID, a v1 collection GET, and a v2 POST on a second backend instance
against an in-process mock Stripe API. All of them must arrive through the
single hook.
