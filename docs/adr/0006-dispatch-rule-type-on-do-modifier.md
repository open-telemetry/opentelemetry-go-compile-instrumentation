# 6. Dispatch Rule Type on the `do` Modifier Name

Date: 2026-07-16

## Status

Accepted

Amends the implementation note of [ADR-0003](0003-structured-rule-schema.md).
The `where`/`do` schema surface is unchanged; this record covers only how that
schema is parsed internally.

## Context

[ADR-0003](0003-structured-rule-schema.md) adopted the two-tier `where`/`do`
schema and stated that the modifier name in `do` declares the rule type. Branch
0 of that work shipped the schema but implemented parsing as a translation
layer (`tool/internal/rule/normalize.go`): it hoisted `where` selectors and
`do` modifier payloads back into the original flat field bag, discarded the
modifier name, and re-inferred the rule type from which fields were present.
Each rule went through two YAML round-trips: unmarshal into a map, normalize,
re-marshal, then unmarshal again into the concrete rule struct.

This was a deliberate scope limiter for branch 0, but it left the modifier
name — the thing ADR-0003 says declares the rule type — unused, and it forced a
field-presence priority order. For example, `inject_code` and `inject_hooks`
both match on the `where.func` selector and were told apart only by checking for
the `raw` field before the `func` field.

Maintainer review on
[PR #377](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/377)
flagged the cost of keeping both shapes. The same selectors and modifiers were
represented in the structured types and again in the flat structs, doubling the
surface where they could drift, and `WhereDef` carried rule-specific selector
fields that normalization always emptied before they reached the file-predicate
filter.
[Issue #541](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/541)
tracked removing the translation layer.

A related goal shaped the decision: we intend to generate a JSON Schema for the
rule YAML from Go struct tags so editors can validate rule files as they are
written. That is only practical if the structured `where`/`do` types are the
faithful, typed representation of the surface rather than an intermediate that
is flattened away.

## Decision

Parse structured rule YAML directly into typed rules, dispatching on the `do`
modifier name.

- Add a typed payload struct per modifier (`HookAction`, `CodeAction`,
  `CallAction`, `StructAction`, `FileAction`, `DirectiveAction`, `DeclAction`)
  and a `DoDef` discriminated union over them, symmetric with `WhereDef`.
  Exactly one modifier is set per `do` entry, and that entry selects the rule
  type.
- Parse each rule entry once into its base fields, its `where` node, and its
  `do` list, then dispatch on the set `DoDef` field to the matching rule
  constructor. Each constructor decodes the `where` selectors and copies the
  typed modifier payload into the concrete rule, with no flat re-marshal.
- Delete `normalize.go` and its tests. The single parser lives in
  `rule.ParseRules`; both the setup phase and the instrumentation golden-test
  harness call it, so there is no second copy of parsing to drift.

The concrete rule structs (`InstFuncRule` and the rest) remain the internal
execution representation; replacing them was out of scope. The typed payloads
and `DoDef` are the structured, schema-ready layer a future JSON Schema
generator can reflect over.

## Consequences

Positive:

- the modifier name declares the rule type, as ADR-0003 intended, so no
  field-presence priority order remains;
- there is one parse path and one definition of each selector and modifier, so
  the drift ADR-0003's schema set out to prevent is also closed in the
  implementation;
- the golden-test harness no longer keeps a private copy of parsing and rule
  type inference;
- the typed `do` payloads give a later JSON Schema generator a surface to
  reflect over without further schema changes;
- net reduction in code.

Tradeoffs:

- the concrete rule structs still carry the same fields as their modifier
  payloads (for example `HookAction.Before` and `InstFuncRule.Before`), bridged
  by an explicit copy in each constructor. This is one typed assignment per rule
  type instead of the previous map-and-marshal translation, and it keeps the
  execution representation stable;
- the legacy flat YAML shape is no longer accepted at the parser. All in-repo
  rule files already use the structured shape; the flat passthrough only kept
  older inline test strings working, and those were migrated.

This amends ADR-0003's implementation note, which stated that the parser change
was confined to a single normalization boundary and that the flat form was
retained as a passthrough. The `where`/`do` schema described in ADR-0003 is
unchanged, and the golden instrumentation output is byte-identical.
