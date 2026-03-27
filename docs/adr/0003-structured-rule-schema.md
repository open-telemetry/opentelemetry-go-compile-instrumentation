# 3. Structured Rule Schema (where/do)

Date: 2026-03-26

## Status

Accepted

## Context

Rule YAML files define compile-time instrumentation rules. Each rule has two concerns:

- **Matching**: which code to target (`target`, `func`, `recv`, `struct`, `version`, …)
- **Modifying**: what transformation to apply (`before`, `after`, `path`, `raw`, `new_field`, …)

The previous format mixed both concerns at the same YAML level — a flat bag of fields. This made rules harder to scan and reason about, especially as the number of rule types and modifier strategies grows.

Example of the old flat format:

```yaml
server_hook:
  target: net/http
  func: ServeHTTP
  recv: serverHandler
  before: BeforeServeHTTP
  after: AfterServeHTTP
  path: "github.com/.../server"
```

## Decision

Introduce explicit `where` (selectors) and `do` (modifiers) groupings, with named modifier actions. The modifier name also determines the rule type, replacing the previous field-presence discriminator.

### New format

```yaml
server_hook:
  where:
    target: net/http
    func: ServeHTTP
    recv: serverHandler        # optional
    version: "v1.0.0,v2.0.0"  # optional
  do:
    inject_hooks:
      before: BeforeServeHTTP
      after: AfterServeHTTP
      path: "github.com/.../server"
```

**Top-level keys per rule:**

| Key | Description |
|---|---|
| `where` | All selector/matcher fields |
| `do` | Exactly one named modifier action with its parameters |
| `imports` | Stays at top level (consumed by both phases) |
| `name` | Optional; the YAML key already serves as name |

### Modifier names and rule types

| Modifier | Rule type | Selector fields |
|---|---|---|
| `inject_hooks` | `InstFuncRule` | `target`, `func`, `recv`, `version` |
| `inject_code` | `InstRawRule` | `target`, `func`, `recv`, `version` |
| `add_struct_fields` | `InstStructRule` | `target`, `struct`, `version` |
| `add_file` | `InstFileRule` | `target`, `version` |
| `wrap_call` | `InstCallRule` | `target`, `function_call`, `version` |
| `expand_directive` | `InstDirectiveRule` | `target`, `directive`, `version` |
| `assign_value` | `InstDeclRule` | `target`, `kind`, `identifier`, `version` |

### Rule type inference

Previously inferred from discriminator field presence (`struct` → StructRule, `func` + no `raw` → FuncRule, etc.).

Now inferred from the **modifier name** inside `do`. The single key inside the `do` map determines the rule type. This eliminates priority-based disambiguation.

### Implementation approach: flatten at the parsing boundary

The rule Go structs (`InstFuncRule`, `InstRawRule`, etc.) represent the **internal model**. The YAML format is the **external representation**. The new format is translated to the old flat format at the parsing boundary via `normalizeRule()`:

1. Parse YAML into `map[string]map[string]any`
2. Detect new format: check for `where` or `do` keys
3. Extract `where` fields, `do` modifier fields, and top-level `imports`/`name`
4. Flatten into a single `map[string]any` (identical to the old flat format)
5. Re-marshal to YAML bytes and pass to existing constructors unchanged

This means:

- **Zero changes** to rule struct YAML tags
- **Zero changes** to rule constructors or validation
- **Zero changes** to JSON serialization (setup → instrument phase)
- **All changes** confined to two parsing locations (`tool/internal/setup/match.go` and `tool/internal/instrument/instrument_test.go`) plus YAML files

### Backward compatibility

Clean break — old flat format is no longer used in YAML files. The `normalizeRule` function does pass through flat-format YAML unchanged (for backward compatibility in inline test strings), but no YAML files use the old format.

## Consequences

**Positive:**
- Rules are visually scannable — selectors vs. modifiers separated at a glance
- Modifier name makes intent explicit without needing to know the discriminator rules
- Extensible: future modifier types (`prepend_statements`, `modify_body`) slot in naturally
- Zero churn on internal model or JSON serialization format

**Negative:**
- YAML files are slightly more verbose (indentation depth +1 for all fields)
- Requires awareness of the normalization layer when debugging parsing issues
