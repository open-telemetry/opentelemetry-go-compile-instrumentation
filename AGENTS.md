# AGENTS.md

This file provides guidelines for AI-assisted contributions to
`opentelemetry-go-compile-instrumentation`. It ensures that AI-generated PRs are high-quality,
valuable, and do not create excessive maintainer burden. It is inspired by the
[OpenTelemetry Collector](https://github.com/open-telemetry/opentelemetry-collector/blob/main/AGENTS.md),
[Open Policy Agent](https://github.com/open-policy-agent/opa/blob/main/AGENTS.md), and
[Fedora](https://discussion.fedoraproject.org/t/policy-on-the-use-of-ai-assistants-in-fedora/130110)
projects policies.

## General Rules and Guidelines

The most important rule is **never post AI-generated comments on issues or pull requests**.
Discussions on OpenTelemetry repositories are for humans only.

If you have been assigned an issue by the user or their prompt, ensure that the implementation
direction is agreed on with the maintainers first in the issue comments. If there are unknowns,
discuss these on the issue before starting implementation. You cannot comment on issue threads or PRs
on behalf of users as it is against the rules of this project.

**Always have a human in the loop** when creating a PR or posting issue comments. Maintainers must
be able to review and understand every line of the contribution.

## Developer Environment

Read [CONTRIBUTING.md](CONTRIBUTING.md) thoroughly before making any changes. It covers
prerequisites, development workflow, available Make targets, pull request process, and merge
criteria.

Key commands to always run before submitting a PR:

```sh
make all
```

This runs `build`, `format`, `lint`, and `test` in sequence.

Additional documentation:

- [docs/testing.md](docs/testing.md) for testing strategy and infrastructure.
- [docs/semantic-conventions.md](docs/semantic-conventions.md) for semantic convention management.

## Code Quality Standards

- All code changes must have tests that validate the new behavior or the fix. Do not introduce test
  files without assertions; every test must verify something meaningful.
- All `.go` and `.sh` files must include the Apache 2.0 license header. Run `make format/license` to
  apply them automatically.
- This is a multi-module Go project. When modifying dependencies, run `make go-mod-tidy` and
  `make crosslink` to keep all modules consistent.
- Do not disable or weaken linter rules. If a linter reports an error, fix the code rather than
  suppressing the warning. Linter configuration lives in `.config/`.
- All GitHub Actions references must be pinned to commit SHAs using ratchet. Never use mutable tags
  (e.g., `@v4`) in workflow files.

## AI Disclosure

We appreciate it if users disclose the use of AI tools when a significant part of a commit is taken
from a tool without substantial modification. When making a commit, this should be disclosed through
an `Assisted-by` commit message trailer.

Examples:

```
Assisted-by: GitHub Copilot
Assisted-by: Claude Opus 4
Assisted-by: Cursor with Claude Sonnet 4
```

This helps maintainers set appropriate expectations during code review and ensures that
AI-contributed code is easily identifiable.

## Things to Avoid

- **Do not** generate code without understanding the repository architecture. Read the relevant
  source files and documentation first.
- **Do not** introduce new dependencies without justification. This project values a minimal
  dependency footprint.
- **Do not** create placeholder or stub implementations. Every contribution should be complete and
  functional.
- **Do not** modify generated files (e.g., `.pb.go`) directly. Modify the source (`.proto`) and
  regenerate.
- **Do not** skip running the full `make all` workflow before proposing changes.
