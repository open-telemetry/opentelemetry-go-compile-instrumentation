<!--
Thank you for contributing to OpenTelemetry Go Compile Instrumentation!

INSTRUCTIONS:
1. Fill in the description and motivation sections below
2. Complete the checklist before submitting
3. Ensure PR title follows conventional commits format (enforced by CI)

PR TITLE FORMAT (required):
  type(scope): description

  Types: feat, fix, docs, chore, refactor, test, release
  Scopes: tool, pkg, demo, test, docs

  Examples:
  - feat(pkg): add gRPC client instrumentation
  - fix(tool): resolve hook signature matching issue
  - docs(api): update instrumenter interface documentation
  - refactor(pkg): simplify attribute extractor composition

BEFORE SUBMITTING:
  make format  # Format Go code and YAML files
  make lint    # Run all linters
  make test    # Run all tests (unit + integration + e2e)

For detailed contribution guidelines, see CONTRIBUTING.md
For available make targets, run: make help
-->

## Description

<!-- What changes does this PR introduce? -->

## Motivation

<!-- Why is this change needed? What problem does it solve? -->

Fixes #<!-- issue number -->

---

## Checklist

- [ ] PR title follows [conventional commits](https://www.conventionalcommits.org/) format
- [ ] Code formatted: `make format`
- [ ] Linters pass: `make lint`
- [ ] Tests pass: `make test`
- [ ] Tests added for new functionality
- [ ] Documentation updated (if applicable)
