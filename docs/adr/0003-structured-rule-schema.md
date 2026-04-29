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
decision and the alternatives considered, so future contributors do not
re-litigate the question.

## Considered Options

### Option A — Keep the flat schema (status quo before PR #377)

```yaml
server_hook:
  target: net/http
  func: ServeHTTP
  recv: serverHandler
  before: BeforeServeHTTP
  after: AfterServeHTTP
  path: github.com/example/nethttp/server
```

**Pros**

- minimal YAML;
- zero migration cost.

**Cons**

- selector and modifier fields share a level; readers must learn which is
  which;
- no obvious slot for file-level predicates;
- rule type is inferred from field presence (priority-ordered discriminator);
- cannot express `one-of` / `not` composition without inventing parallel
  fields.

### Option B — 3-tier model (Package → File → Point)

```yaml
hook_serve:
  target: net/http # tier 1
  version: "v1.0.0,v2.0.0" # tier 1
  where: # tier 2 — file predicate only
    has_func: init
  func: ServeHTTP # tier 3
  recv: serverHandler # tier 3
  before: BeforeServeHTTP
  after: AfterServeHTTP
  path: github.com/example/nethttp/server
```

**Pros**

- clean conceptual layering;
- file predicate has its own home (`where`);
- familiar shape for readers used to the Orchestrion `Point` model.

**Cons**

- introduces a mental model — three tiers — that contributors must learn
  before writing a rule;
- duplicates names across tiers (`func` at tier 3 vs `has_func` at tier 2),
  which is exactly the ambiguity flat schemas had, just renamed;
- selectors at tier 3 still mix with modifiers at the top level;
- the boundary between "what counts as tier 2" and "what counts as tier 3"
  was contested in review and never reached consensus.

### Option C — Action block + minimal `where.file`

```yaml
hook_serve:
  target: net/http
  where:
    has_func: init # file-level predicates only
  inject_hooks: # rule type + selectors + modifiers
    func: ServeHTTP
    recv: serverHandler
    before: BeforeServeHTTP
    after: AfterServeHTTP
    path: github.com/example/nethttp/server
```

**Pros**

- excellent per-rule locality: one block per rule type, one doc page per
  rule type;
- `where` is unambiguously file-level.

**Cons**

- cannot express `one-of` across point selectors without introducing a new
  composition layer;
- modifier-specific docs duplicate selector tables (each rule type page
  redocuments `func`, `recv`, etc.);
- mixes selectors and modifiers inside the same action block — the very
  ambiguity Option B was trying to remove.

### Option D — 2-tier `where` (selectors) + `do` (modifiers) — chosen

```yaml
hook_serve:
  target: net/http
  version: "v1.0.0,v2.0.0"
  where:
    func: ServeHTTP
    recv: serverHandler
    file:
      has_func: init
  do:
    - inject_hooks:
        before: BeforeServeHTTP
        after: AfterServeHTTP
        path: github.com/example/nethttp/server
```

**Pros**

- selectors and modifiers never mix: `where` is read-only intent,
  `do` is write intent;
- package selection (`target`, `version`) stays visually obvious at the top
  of every rule;
- rule type is _declared_, not inferred — the modifier name in `do` is the
  discriminator, eliminating field-presence priority logic;
- composition (`all-of`, `one-of`, `not`) has one home (`where`) and one
  meaning (boolean over selectors);
- multi-modifier rules (e.g. add two struct fields, or a hook plus a raw
  injection) become natural by listing modifiers in `do`;
- the SQL analogy (`WHERE … DO …`) is familiar to anyone who writes queries.

**Cons**

- slightly more verbose YAML than Option A (extra indentation, explicit
  `where`/`do` labels);
- the parser must normalize the structured shape to the existing internal
  flat representation at the YAML boundary.

## Decision

Adopt **Option D**.

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

### Top-level fields

| Key       | Required | Meaning                                                            |
| --------- | -------- | ------------------------------------------------------------------ |
| `target`  | yes      | Package selector. Import path or special package target.           |
| `version` | no       | Version range `start,end`. Lower bound inclusive, upper exclusive. |
| `where`   | no       | Non-package selectors and selector composition.                    |
| `do`      | yes      | Ordered modifier list (or single-modifier map; see below).         |
| `imports` | no       | `alias: path` map merged into instrumented files.                  |
| `name`    | no       | Explicit rule name; defaults to the YAML key.                      |

### `where` semantics

- Flat selector keys inside `where` are an implicit `all-of`.
- Explicit qualifier keys `all-of`, `one-of`, `not` may appear at any
  position to compose nested selector groups.
- Point selector keys recognized at the top of `where`:
  `func`, `recv`, `struct`, `function_call`, `directive`, `kind`,
  `identifier`.
- File-level predicates live under `where.file`.
- `target` and `version` MUST NOT appear inside `where`. They are package
  selectors and stay top-level so package matching reads obviously at the
  top of every rule.

### `where.file` semantics

- Predicate keys: `has_func`, `recv`, `has_struct`, `has_directive`.
  Combinator keys: `all-of`, `one-of`, `not`.
- `recv` is a _modifier of_ `has_func` (it narrows the function match to a
  specific receiver type) and not an independent leaf. The bare key matches
  the rule-level `recv` selector for symmetry.
- Exactly one leaf predicate must be active per `where.file` node.
  Compositions are expressed via `all-of` / `one-of` / `not`.
- Today the setup phase executes only leaf predicates (`has_func`,
  `has_struct`); combinators and `has_directive` are validated but return
  descriptive errors at build time. They are reserved schema surface and
  are wired up in follow-up PRs.

### `do` semantics

`do` accepts two YAML shapes; both normalize to the same ordered internal
list:

```yaml
# Sequence form — canonical, supports one or more modifiers.
do:
  - inject_hooks:
      before: BeforeOpen
      path: github.com/example/sql

# Map form — sugar for a single modifier.
do:
  inject_hooks:
    before: BeforeOpen
    path: github.com/example/sql
```

Rules:

- the sequence form is the canonical form used in all in-repo examples and
  documentation;
- each list item is a single-key map whose key names the modifier;
- duplicate modifier kinds are allowed; declaration order is preserved and
  is the application order;
- `do` must not be missing or empty.

### Modifier names → rule types

| Modifier            | Internal rule type  |
| ------------------- | ------------------- |
| `inject_hooks`      | `InstFuncRule`      |
| `inject_code`       | `InstRawRule`       |
| `add_struct_fields` | `InstStructRule`    |
| `add_file`          | `InstFileRule`      |
| `wrap_call`         | `InstCallRule`      |
| `expand_directive`  | `InstDirectiveRule` |
| `assign_value`      | `InstDeclRule`      |

Rule type comes from the modifier key, not from a priority order over
selector fields.

### Special `target` values

- `target: main` — valid; setup matches against the compile-time
  `-p` package import path, and `main` participates naturally.
- `target: test_main` — not currently supported; the dependency model has
  no test-binary discriminator. Reserved for future work.
- Wildcards in `target` are out of scope of this ADR and are tracked
  separately.

### Invalid shapes

```yaml
# target inside where is rejected
invalid_target_in_where:
  where:
    target: net/http
    func: Serve
  do:
    - inject_hooks:
        before: BeforeServe
        path: github.com/example/nethttp

# empty do is rejected
invalid_empty_do:
  target: net/http
  where:
    func: Serve
  do: []

# multi-key do item is rejected
invalid_multi_key_do_item:
  target: net/http
  where:
    func: Serve
  do:
    - inject_hooks:
        before: BeforeServe
        path: github.com/example/nethttp
      inject_code:
        raw: println("bad")

# malformed where.file is rejected
invalid_where_file_shape:
  target: net/http
  where:
    file: init
  do:
    - add_file:
        file: helpers.go
        path: github.com/example/helpers
```

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
