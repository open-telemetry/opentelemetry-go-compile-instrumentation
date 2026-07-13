# Security Policy

## Reporting a Vulnerability

The OpenTelemetry project follows a coordinated vulnerability disclosure model.
**Please do not report security vulnerabilities through public GitHub issues.**

### Preferred Method — GitHub Private Vulnerability Reporting

Use GitHub's built-in
[private vulnerability reporting](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/security/advisories/new)
feature to open a confidential security advisory directly in this repository.
The maintainers will be notified and can begin triage without public exposure.

### Alternative — Security Mailing List

You can also send a report to the OpenTelemetry security mailing list:
**<security@opentelemetry.io>**

Encrypt your message with the
[OpenTelemetry PGP key](https://github.com/open-telemetry/.github/blob/main/SECURITY.md)
when submitting sensitive details.

## Security Model

This repository follows the
[OpenTelemetry organization security policy](https://github.com/open-telemetry/.github/blob/main/SECURITY.md).

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
Committee can be reached at **<security@opentelemetry.io>** for
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

## Security Notification Template

When a security advisory is ready for public disclosure, maintainers can use
the following template to notify affected users via GitHub Releases, the
mailing list, or the advisory itself.

```
Subject: [Security Advisory] <CVE-ID or short title> in opentelemetry-go-compile-instrumentation

Severity: <Critical | High | Medium | Low>
Affected versions: <e.g., < v0.5.1>
Fixed in: <e.g., v0.5.1>
CVE: <CVE-YYYY-NNNNN or "pending">

## Summary

<One-paragraph description of the vulnerability and its impact.>

## Affected Components

<List of affected packages or modules.>

## Mitigation

<Workarounds, if any.>

## Fix

Upgrade to <fixed version>. See the release notes at
<link to GitHub release>.

## Credits

We thank <reporter name / handle> for responsibly disclosing this issue.
```

## Additional Resources

- [OpenTelemetry Security Policy](https://github.com/open-telemetry/.github/blob/main/SECURITY.md)
- [CNCF Security Guidelines](https://contribute.cncf.io/projects/best-practices/security/)
- [OpenSSF Best Practices](https://bestpractices.coreinfrastructure.org/en)
- [OpenTelemetry sig-security](https://github.com/open-telemetry/sig-security)
