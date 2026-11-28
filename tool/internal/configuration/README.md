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

`general.*` is generated from OpenTelemetry Configuration **v1.2.0**, commit
`3b04ae78576ec407854d734eb4da4f78a67f902c`, `schema/instrumentation.yaml`.
`gen-config-catalog` fetches that immutable URL and verifies its SHA-256 before
generation. It projects the value constraints needed by application validation
into the catalog; the raw upstream schema is not embedded in the runtime
package. Tests exercise generation with small in-memory schema fixtures and do
not use the network.
Names preserve property paths: `instrumentation/development` is the enclosing
root, so `/development` is not added to each general leaf. The snapshot also
retains upstream constraints for subsequent application-configuration validation;
the catalog projects leaf names, types, descriptions, defaults, and the supported
value constraints.

`general-compatibility.yaml` (owned by the generator under
`tool/cmd/gen-config-catalog/`) records deprecated `general.*` paths. For a path
still present upstream, the entry supplies its type and deprecation metadata;
generation preserves the upstream description and default behavior. If an
upstream `general.*` path is retired, the generator carries its previous
definition forward as deprecated. Retired `go.*` paths must still be retained
explicitly in `tool/cmd/gen-config-catalog/go-configurations.yaml`; generation
fails if one would otherwise disappear. The generator combines both sources with
the pinned upstream schema.

After changing either source file, regenerate and test the catalog:

```sh
go generate ./tool/internal/configuration
make format/yaml
go test ./tool/internal/configuration
go test ./tool/cmd/gen-config-catalog
```

Catalog generation requires network access to fetch the pinned immutable upstream
schema. Tests use fixture schemas and remain offline. The generated `catalog.yaml`
is embedded by otelc; the source schemas and generator are not.

Updating the upstream release requires updating the pinned release and commit in
`definitions.go` and the schema checksum in `tool/cmd/gen-config-catalog/main.go`.
Review changes to types and paths, add compatibility entries for removed paths
where needed, regenerate the catalog, and run the generator and configuration
tests. No production `go.*` definitions are invented here.

Environment names derive from the resolved `name`: strip `general.`, retain `go.`,
and omit a terminal `/development` marker if the property actually has one.
Resolving configuration metadata does not enable instrumentation or load user
configuration values. Runtime environment-source precedence belongs to that
separate loading step.

## Application configuration validation

`otelc --instrumentation-config <path>` reads one application configuration at
setup. It does not search for a default file, and repeated uses of the flag are
rejected. `otelconf/x` v0.27.0 parses the SDK model and selected
instrumentation options; its environment substitutions are resolved during
setup. Declared runtime environment overrides remain a separate follow-up.

The application must use `file_format` major version 1 and a minor version no
newer than the pinned OpenTelemetry Configuration v1.2.0 schema. Selected values
in `instrumentation/development` are checked against module metadata and the
upstream value constraints projected into the generated catalog.
Selected fields omitted by the pinned `otelconf/x` model are rejected rather
than silently discarded. This parser/schema compatibility boundary must be
revisited when either pin changes.

Values declared by selected module metadata are validated and returned in a
normalized map for the later embedding phase. Configuration for unselected
options is warned about and ignored. If a configured option cannot be
validated while a selected module lacks metadata, setup reports the missing
module roots. Configuration never changes instrumentation selection.

When validation succeeds, selected values and metadata defaults are normalized
into a deterministic, versioned JSON snapshot. Setup embeds that snapshot in
the application through `hook.ConfigurationSnapshot()`; runtime does not read
the source configuration file or parse YAML. Selected behavioral metadata and
resolved values contribute to the build-cache identity. Source paths,
diagnostic provenance, and process-start environment overrides do not. Typed
rule accessors and runtime environment precedence are implemented by the
follow-up configuration issues.

## Metadata discovery during setup

Setup only considers metadata belonging to instrumentation modules whose rules
matched the build. It reads `metadata.yaml` from the directory containing each
matched module's `go.mod`; it does not search parent directories. This also
works when an external module is downloaded independently to the module cache.
Each selected module is read once, and equivalent definitions are deduplicated.

Missing metadata is not a setup error by itself. Setup returns resolved
definitions and selected module roots without metadata to the application
configuration validator, which can report a missing definition only when a
supplied option needs one. Discovery does not select or enable instrumentation.

When configuration includes an option that cannot be matched to selected
metadata while one or more selected modules lack `metadata.yaml`, validation
fails conservatively and names the missing module roots. A Go configuration
group is considered selected when selected metadata declares at least one
`go.<group>.*` option. Unknown options within such a group fail validation;
other Go groups are warned about and ignored as unselected. Group ownership is
derived from metadata paths rather than inferred from a Go module path.
