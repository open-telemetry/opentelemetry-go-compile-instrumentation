# Go instrumentation configuration metadata

An instrumentation module may declare `configurations` in its `metadata.yaml`.
Each source entry has exactly one of `name` or `ref`. An inline definition uses
`name` as an OpenTelemetry path relative to `instrumentation/development`, and
`type` as one of `boolean`, `string`, `int`, `map`, `list`, or `structured_list`.
There are no separate flat/Java property names or `declarative_*` fields.

```yaml
configurations:
  - name: go.my_instrumentation.enabled
    type: boolean
    description: Whether this instrumentation records telemetry.
    default: "true"
  - ref: general.http.client.request_captured_headers
```

Refs select whole catalog definitions; inline overrides are rejected, including
explicitly empty or null fields. `schema` is required for `structured_list` and
describes each item's object properties. Property defaults and examples retain
their native YAML types. Top-level defaults are stored as strings; boolean and
numeric YAML scalars are normalized to their source spelling. An omitted default
differs from an explicitly empty string. `default_behavior` is optional prose and
must never be interpreted as a machine-readable default.
Collection defaults encode YAML inside the string, for example `default: "[]"`
for a list or `default: "{}"` for a map. Structured-list defaults are validated
against the item schema, including required keys and property value types.
Inline definitions may set `deprecated: true` and optionally name a canonical
`replaced_by` path. A replacement is valid only when the old entry is deprecated;
deprecated entries may have no replacement. Reference entries use the complete
catalog definition and cannot override its deprecation metadata.

## Shared catalog

`catalog.yaml` is embedded by otelc. The Go watcher reads that same file from the
otelc revision it scans, resolves refs, and preserves the original `ref` alongside
the resolved fields in inventory. Source-entry exclusivity does not apply to
those resolved records.

`general.*` is generated offline from `upstream-instrumentation.yaml`, copied
byte-for-byte from OpenTelemetry Configuration **v1.2.0**, commit
`3b04ae78576ec407854d734eb4da4f78a67f902c`, `schema/instrumentation.yaml`.
The source SHA-256 and the generated catalog contents are checked by tests.
Names preserve property paths: `instrumentation/development` is the enclosing
root, so `/development` is not added to each general leaf. The snapshot also
retains upstream constraints for subsequent application-configuration validation;
this metadata catalog projects leaf names, types, descriptions, and default behavior.

`general-compatibility.yaml` records deprecated `general.*` paths. For a path
still present upstream, the entry supplies its type and deprecation metadata;
generation preserves the upstream description and default behavior. A retired
path must include its complete definition. The generator compares the previous
catalog with its output and fails if a prior path would disappear without a
compatibility entry. Add otelc-owned `go.*` definitions, including any deprecated
ones, to `go-configurations.yaml`; all three sources are combined by `go generate`.

After changing either source file, regenerate and test the catalog:

```sh
go generate ./tool/internal/configuration
make format/yaml
go test ./tool/internal/configuration
```

Updating the upstream release requires updating the source snapshot, provenance
constants, and checksum test together, reviewing changes to types and paths, and
regenerating the catalog. No production `go.*` definitions are invented here.

Environment names derive from the resolved `name`: strip `general.`, retain `go.`,
and omit a terminal `/development` marker if the property actually has one.
Resolving configuration metadata does not enable instrumentation or load user
configuration values. Runtime environment-source precedence belongs to that
separate loading step.
