<!--
Thank you for contributing to OpenTelemetry Go Compile Instrumentation!

INSTRUCTIONS:
1. Fill in the description and motivation sections below
2. Complete the checklist before submitting
3. Ensure PR title follows conventional commits format (enforced by CI)

PR TITLE FORMAT (required):
  type(scope): description

  Types: chore, doc, docs, feat, fix, release, refactor, test
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

If your PR adds a new user-facing instrumentation, please also submit a corresponding OpenTelemetry Registry PR:
https://opentelemetry.io/ecosystem/registry/adding/

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
- [ ] Tests follow [testing guidelines](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/blob/main/docs/testing.md)
- [ ] Documentation updated (if applicable)
- [ ] OpenTelemetry Registry updated (if applicable, see [registry guide](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/blob/main/docs/instrument-guide.md#4-register-the-instrumentation))
- [ ] This PR has content that I did not fully write myself.
  - [ ] I used AI and I have read and followed the [Generative AI Contribution Policy](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/blob/main/AI_POLICY.md).
- [ ] I have the experience and knowledge necessary to understand, review, and validate all content in this PR.[^I-know-my-stuff]

[^I-know-my-stuff]:
    Yes, I can answer maintainer questions about the content of this PR, without using AI.
