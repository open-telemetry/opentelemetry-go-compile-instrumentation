# Filters `weaver registry check` JSON diagnostics down to the ones that must
# fail `make lint-schema`.
#
# The only allowlisted finding is DuplicateMetricName between our own registry
# (`registry_id: "otelc"`) and the upstream OpenTelemetry dependency
# (`registry_id: "opentelemetry"`).
#
# Why this is expected, not a bug:
#   otelc's instrumentations emit standard upstream metrics (e.g.
#   `http.client.request.duration`). We deliberately re-declare each emitted
#   metric under `schemas/otelc/groups/` so the registry is an explicit,
#   machine-readable contract of exactly what otelc produces. weaver has no
#   first-class way to *reference* a metric defined in a dependency the way
#   `ref:` references an attribute, so re-declaring is the only way to express
#   "otelc emits this upstream metric", and weaver then reports the mirror as a
#   DuplicateMetricName. This is the same limitation OBI works around for
#   `dns.lookup.duration` (https://github.com/open-telemetry/weaver/issues/1578);
#   we generalize the exception because otelc mirrors upstream metrics by design.
#
# The exception is narrow: it fires ONLY when the duplicate is between exactly
# our registry and the upstream dependency. Any other diagnostic is kept and
# fails the lint, including:
#   - a metric declared twice *within* otelc's own groups
#     (provenances would be ["otelc", "otelc"])
#   - unresolved `ref:` to a non-existent attribute
#   - `--future` findings such as missing examples on string attributes
#   - any non-DuplicateMetricName error
map(select(
  (
    (.error.DuplicateMetricName? // null) as $dup
    | $dup != null
      and (($dup.provenances // []) | length) == 2
      and (([$dup.provenances[].registry_id] | sort) == ["opentelemetry", "otelc"])
  ) | not
))
