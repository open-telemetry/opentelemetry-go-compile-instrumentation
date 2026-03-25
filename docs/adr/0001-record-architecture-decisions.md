# 1. Record Architecture Decisions

Date: 2026-03-19

## Status

Accepted

## Context

As the OpenTelemetry Go compile-time instrumentation project grows and more contributors join the SIG, we need a lightweight way to capture significant architectural decisions with their context and rationale. Without this, it becomes difficult to understand *why* things are the way they are, leading to repeated debates or inadvertent reversals of past decisions.

## Decision

We will use Architecture Decision Records (ADRs) as described by Michael Nygard in his article [Documenting Architecture Decisions](http://thinkrelevance.com/blog/2011/11/15/documenting-architecture-decisions).

ADRs will be stored in `docs/adr/` as numbered Markdown files. The `adr-tools` CLI (`npryce/adr-tools`) is available for creating and linking records. A `.adr-dir` file at the repository root points tools to the correct directory.

New ADRs should be created for:

- Significant changes to the instrumentation API or hook model
- Adoption or rejection of external dependencies
- Changes to the two-phase build process (setup/instrument)
- Decisions about semantic convention support
- Any decision the SIG discusses and reaches consensus on

Use `make adr-new "Title of Decision"` to create a new ADR from the template.

## Consequences

- Architectural decisions are discoverable alongside the code.
- Contributors can understand the reasoning behind the current design without reading meeting notes.
- New decisions require a short writeup, which is a small but deliberate overhead.
- ADRs are immutable records: superseded decisions are marked as such rather than deleted.
