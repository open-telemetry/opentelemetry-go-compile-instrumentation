---
applyTo: "tool/**"
excludeAgent: "cloud-agent"
---

# Reviewing `tool/`

otelc has to see the build the way the `go` command it wraps does. Most problems found in review
here are places where the two disagree.

## Fidelity with the go command

- Code that loads the user's packages, runs `go list` on them or interprets the user's arguments
  has to see the build the way the user's `go` command does: the same build flags, build tags,
  working directory (`-C`) and environment, including `GOFLAGS`. Loads that only resolve
  instrumentation packages don't need the user's flags. Reuse the existing argument and flag
  handling in `tool/internal/setup` and `tool/internal/pkgload` instead of parsing arguments again.
- Check flag handling against the `go` command itself (`go help build`, `go help testflag`, and
  cmd/go for the Go version in `go.mod`), not against assumptions: both the `-flag value` and
  `-flag=value` forms, boolean flags, `GOFLAGS` syntax, and arguments meant for the test binary.
- A nil error from `packages.Load` doesn't mean every package loaded. Check each used package's
  `Errors`.
- Lookups by import path have to account for the ways the compiler can remap imports, such as
  vendored packages and `importmap` entries in the importcfg.
- Each `packages.Load` or `go list` call starts a subprocess. Flag new calls in loops that could
  be batched, and repeated loads of the same directory.

## Rules (`tool/internal/rule`, `tool/internal/setup`)

- Invalid rule YAML must fail at load time with an error that names the rule, not load and then
  silently match nothing. Check null list entries, empty strings, case or whitespace variants of
  special values such as `$root`, and YAML aliases.
- Every place that parses rule YAML, including the matcher in `tool/internal/setup/pin.go`, must
  accept and reject the same input.
- A rule must not be applied twice when several roots or globs match the same package.
- Changes to the rule schema or to matching need a matching update to `docs/rules.md`.

## AST rewriting (`tool/internal/instrument`, `tool/internal/ast`)

- Generated identifiers and import qualifiers must be valid where they're inserted. Check
  shadowing by parameters and locals, aliased imports (`import f "fmt"`), and the same path
  imported under two aliases. Reporting a conflict is better than generating code that compiles
  but refers to the wrong thing.
- A rewrite must only touch code the rule added, not user code embedded in the same template
  through `{{ . }}`.
- `AstParser.FindPosition` returns a zero `token.Position` for nodes the parser didn't produce,
  including cloned nodes. Code that looks up positions after a replacement has to handle that.

## Errors and exits

- Create errors with `ex.New` or `ex.Newf`, wrap standard-library and third-party errors once with
  `ex.Wrap` or `ex.Wrapf`, then `return err`. Flag wrapping that repeats context the message
  already has.
- `ex.Fatal`, `ex.Fatalf`, `util.Assert` and `util.AssertType` exit the process. They must not be
  reachable from user input such as rule files, flags or the source being compiled. Return an
  error instead.

## Tests

- Golden tests in `tool/internal/instrument/testdata/golden/` should cover the new behavior, and
  each fixture's comments and `rules.yml` should describe what it actually tests.
- Tests of stateful setup steps should start each simulated `otelc` run with fresh state, the way
  a second invocation would.
