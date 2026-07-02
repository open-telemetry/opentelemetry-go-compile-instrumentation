# Security Policy

## Reporting a Vulnerability

The OpenTelemetry project follows a coordinated vulnerability disclosure model.
**Please do not report security vulnerabilities through public GitHub issues.**

### Preferred Method — GitHub Private Vulnerability Reporting

If enabled by the repository maintainers, you can use GitHub's built-in
[private vulnerability reporting](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/security/advisories/new)
feature to open a confidential security advisory directly in this repository.
The maintainers will be notified and can begin triage without public exposure.
If the link above returns a 404, the feature has not yet been enabled — use
the mailing list method below instead.

### Alternative — Security Mailing List

You can also send a report to the OpenTelemetry security mailing list:
**<cncf-opentelemetry-tc@lists.cncf.io>**

Encrypt your message with the
[OpenTelemetry PGP key](https://github.com/open-telemetry/.github/blob/main/SECURITY.md)
when submitting sensitive details.

## Security Model

This repository inherits the
[OpenTelemetry organization security policy](https://github.com/open-telemetry/.github/blob/main/SECURITY.md).

When assessing the urgency of a security fix, **confidentiality** and
**integrity** are our primary concerns for all artifacts produced by this
project. Availability against denial-of-service attacks is explicitly out of
scope for the default threat model — users requiring availability guarantees
must configure authentication on their endpoints.

The following are **not** considered security vulnerabilities in this project:

- Denial-of-service by properly authenticated clients.
- Availability-related attacks against unauthenticated endpoints when the
  project documentation explicitly marks those endpoints as unauthenticated.

## Supported Versions

Security fixes are applied to the **latest released minor version** only.
Older minor releases do not receive backported security patches.

## Disclosure Timeline

| Step | Target SLA |
| --- | --- |
| Acknowledge receipt | 3 business days |
| Initial triage completed | 7 business days |
| Patch ready for review | 30 calendar days (may vary with severity) |
| Coordinated public disclosure | Agreed with reporter; default 90 days |

## Security Contacts

Security reports are reviewed by the project maintainers listed in
[`.github/CODEOWNERS`](.github/CODEOWNERS). The OpenTelemetry Technical
Committee can be reached at **<cncf-opentelemetry-tc@lists.cncf.io>** for
escalations.

## Security Practices in This Repository

The following controls are currently in place:

- **OSSF Scorecard** — runs weekly and on every push to `main`
  (`.github/workflows/ossf-scorecard.yml`).
- **Dependency management** — Renovate Bot keeps all dependencies up to date
  (`.github/renovate.json5`).
- **CodeQL static analysis** — a workflow to scan Go source code for known
  vulnerability patterns on every pull request is proposed in PR #536 and
  pending merge.
- **Workflow hardening** — all GitHub Actions workflows pin dependencies to
  commit SHAs and follow least-privilege permission models.
- **License compliance** — FOSSA checks run on every PR to verify dependency
  license compatibility.

## Additional Resources

- [OpenTelemetry Security Policy](https://github.com/open-telemetry/.github/blob/main/SECURITY.md)
- [CNCF Security Guidelines](https://contribute.cncf.io/projects/best-practices/security/)
- [OpenSSF Best Practices](https://bestpractices.coreinfrastructure.org/en)
- [OpenTelemetry sig-security](https://github.com/open-telemetry/sig-security)
