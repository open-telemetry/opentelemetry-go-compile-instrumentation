---
applyTo: "tool/**"
excludeAgent: "cloud-agent"
---

# Reviewing `tool/`

otelc has to see the build the way the `go` command it wraps does. Most problems found in review
here are places where the two disagree.

## Fidelity with the go command

- `packages.Load` calls that need to see packages the way the user's build does (the build
  targets, or local packages that build tags can affect) should get the user's build flags through
  `extractBuildFlags` in `tool/internal/setup`. It keeps `-C`, `-overlay`, `-tags`, `-mod`,
  `-modfile` and boolean flags such as `-race`. Loads that only resolve instrumentation packages
  (`config.go`, `store.go`) don't need them.
- `classifyArgs` in `tool/internal/setup/args.go` classifies command-line arguments. New code
  should use it instead of splitting arguments again.
- `-C` becomes `packages.Config.Dir` in `loadDirFromBuildFlags` (`tool/internal/pkgload`).
  `go` changes to the `-C` directory before anything else, so relative paths in other flags are
  resolved against it.
- In `GOFLAGS`, value flags are only valid as `-flag=value`. Entries are split like
  `cmd/internal/quoted.Split` (see `tool/util/go.go`): any whitespace, including `\n` and `\r`,
  separates entries, and quotes have no escapes.
- `go test` has its own flags (`go help testflags`). Arguments after `-args` go to the test
  binary; with `--`, the `--` itself is passed along too. Argument handling has to cover both.
- A nil error from `packages.Load` doesn't mean every package loaded. Check `len(pkg.Errors) > 0`
  for each package that is used.
- The compiler can remap imports through `importmap` lines in the importcfg, for example vendored
  `vendor/golang.org/x/net/...` packages in the standard library. Lookups by import path have to
  apply that map.
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
