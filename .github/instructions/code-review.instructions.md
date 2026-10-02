---
applyTo: "**"
excludeAgent: "cloud-agent"
---

# Code review

otelc instruments Go programs at build time. `otelc go build` first runs a setup phase that
matches YAML rules against the packages in the build, then runs the build with `-toolexec` so
otelc can rewrite the AST of matched packages and inject hook code. `AGENTS.md` and
`CONTRIBUTING.md` describe how contributions are made.

## Look for

- **Behavior changes for users** of otelc or of the instrumented program: changed results,
  swallowed errors, panics, process exits, data races.
- **Silent failures**: a misconfiguration or failed load that is ignored, only logged to
  `.otelc-build/debug.log`, or ends with nothing instrumented and no message. Anything the user
  has to act on must be returned as an error or printed to stderr.
- **Unusual but real inputs**: paths with spaces or quotes, flags given as `-flag value` and as
  `-flag=value`, empty or `null` YAML values.
- **Tests that don't test the change**: the new branch is never reached, state is shared between
  cases or runs, an assertion would still pass with the fix reverted, or a substring check also
  matches wrong output.
- **Logic that can drift apart**: the same parsing or validation written in more than one place,
  or lists that have to be kept in sync by hand.
- **Documentation drift**: docs that no longer match the code, especially `docs/rules.md` after
  rule changes, and doc examples that wouldn't compile.
- **Nondeterminism** in generated code, logs or error messages, such as map iteration order or
  unsorted file lists.

## Don't comment on

CI already checks these:

- Diagnostics the `golangci-lint` CI run already reports for this PR (`.tools/golangci.yml`
  enables 77 linters, including `govet`, `errcheck`, `bodyclose`, `spancheck`, `copyloopvar` and
  `gosec`). A real problem is still worth raising if the linter doesn't report it.
- Formatting, typos, markdown lint, license headers, conventional-commit PR titles, an outdated
  `tool/data/otelc-bundle.tgz` or outdated golden files, test file names and the coverage floor.

Also skip:

- Problems that could only appear if the code changes later ("if someone adds…"). The project
  prefers the simplest change that is correct today.

## Before you claim something

- Read the code at the PR head. Don't report a case as unhandled when the code already handles it.
- The Go version is the `go` directive in `go.mod`. Since Go 1.22 each loop iteration has its own
  variables, so don't flag loop-variable capture.
- Check the Go documentation before claiming platform-specific behavior. For example,
  `os.Rename` replaces an existing file on Windows too.
- Code that keeps compatibility with released otelc versions is usually deliberate. Look for a
  comment explaining it before suggesting removal.

## How to comment

- One comment per problem. If the same problem appears elsewhere, list those places in that
  comment.
