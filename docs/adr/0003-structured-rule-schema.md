# 3. Structured Rule Schema

Date: 2026-04-29

## Status

Accepted

## Context

Rule YAML files describe compile-time instrumentation rules. Every rule answers
three questions:

1. **Which package?** — package import path and version range.
2. **What inside that package?** — a function, struct, call site, directive,
   or named declaration; optionally further constrained by file-level
   predicates.
3. **What modification do we apply?** — inject hooks, raw code, struct fields,
   files, call wrappers, directive expansions, or value assignments.

The original schema was a flat bag of fields where matching and modification
keys lived at the same level and rule type was inferred from which key was
present. As the number of join-point types and modifier strategies grew, the
flat shape became:

- ambiguous about which field is a selector vs. a modifier;
- unable to express "instrument X only in files that look like Y" without
  inventing yet another single-purpose field;
- unable to express boolean composition (`one-of`, `not`) over selectors —
  a real need surfaced by the dd-trace-go instrumentation set;
- lossy: rule type was inferred from field presence rather than declared,
  which forced a priority order over fields and made errors hard to read.

Reviewer discussion on PR
[open-telemetry/opentelemetry-go-compile-instrumentation#377](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pull/377)
explored several shapes before consensus emerged. This ADR records the
decision so future contributors do not re-litigate the question.

## Decision

Adopt a **2-tier `where`/`do` schema** for all instrumentation rules.

A rule has exactly the following top-level shape:

```yaml
rule_name:
  target: <package selector>          # required
  version: <version range>            # optional
  where:                              # optional; non-package selectors
    <selector keys>
    file:
      <file predicate keys>
  do:                                 # required; modifier(s)
    - <modifier name>:
        <modifier keys>
  imports:                            # optional; injected imports
    <alias>: <path>
  name: <explicit name>               # optional; YAML key is the canonical name
```

Key properties of this design:

- `target` and `version` stay at the top level as package-scope selectors —
  they answer "which packages?" at a glance on every rule.
- `where` carries all non-package selectors (read intent). Flat keys inside
  `where` are an implicit `all-of`. File-level predicates live under
  `where.file`. Composition sub-groups (`one-of`, `not`) may appear at any
  depth inside `where`.
- `do` carries modifiers only (write intent). The modifier name in `do`
  declares the rule type — no field-presence priority order needed.
- Selectors and modifiers never appear at the same level. The SQL analogy
  (`WHERE … DO …`) makes the split self-documenting.

See [docs/rules.md](../rules.md) for the full field reference, valid/invalid
shape examples, and per-rule-type documentation.

## Consequences

Positive:

- package selection is visually obvious on every rule;
- selectors and modifiers each have an explicit, single home;
- rule type is declared rather than inferred;
- multi-modifier rules and selector composition both have a natural shape;
- the parser change is confined to a single normalization boundary; rule
  structs, constructors, validators, and JSON serialization are unchanged.

Tradeoffs:

- YAML is more verbose than the flat format (one extra indentation level
  for selectors and modifiers);
- contributors must learn the `where`/`do` split — though the SQL analogy
  shortens that learning curve.

Implementation note:

- Both the structured shape and the legacy flat shape are accepted at the
  YAML boundary. The legacy flat form is retained only as a parsing
  passthrough so that older inline test strings continue to work; all
  in-repo YAML files use the structured shape.
- Broader execution of qualifier composition (`all-of`, `one-of`, `not`)
  remains follow-up work; the schema surface is locked here.
