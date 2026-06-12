# CNCF Security Best Practices — Gap Analysis

This document evaluates `opentelemetry-go-compile-instrumentation` against the
[CNCF Security Guidelines for New Projects](https://contribute.cncf.io/projects/best-practices/security/)
and records the current security posture, identified gaps, and proposed
remediation steps.

> **Last reviewed:** 2026-06

---

## 1. Securing the Code Repository

### 1.1 Access Management

| Control | Status | Notes |
| --- | --- | --- |
| Role-based access control (RBAC) | ✅ In place | Managed at org level; roles follow least-privilege |
| Strong authentication (SSH keys / PAT) | ✅ In place | Enforced by GitHub org policy |
| Two-factor / Multi-factor authentication | ✅ In place | Required for all org members |

### 1.2 Branch Protection

| Control | Status | Notes |
| --- | --- | --- |
| PRs required for changes to `main` | ✅ In place | Enforced via branch protection rules |
| At least one reviewer required | ✅ In place | Configured in repository settings |
| `CODEOWNERS` for automatic review requests | ✅ In place | `.github/CODEOWNERS` |
| Signed commits | ⚠️ Partial | Not currently enforced; recommended for maintainers |

**Action:** Evaluate enforcing GPG/SSH commit signing for merges to `main`.
[Follow-up issue recommended]

### 1.3 Managing Contributions

| Control | Status | Notes |
| --- | --- | --- |
| Issue templates | ✅ In place | `.github/ISSUE_TEMPLATE/` |
| Pull request template | ✅ In place | `.github/PULL_REQUEST_TEMPLATE.md` |
| Secret scanning | ✅ In place | GitHub Advanced Security secret scanning enabled at org level |
| Code scanning (SAST) | ⏳ Pending | CodeQL workflow proposed in PR #536 (not yet merged) |
| Dependency vulnerability scanning | ✅ In place | Renovate Bot (`renovate.json5`) + FOSSA (`fossa.yml`) |

---

## 2. Self-Assessment

| Control | Status | Notes |
| --- | --- | --- |
| CNCF TAG Security self-assessment | ❌ Missing | Recommended for projects approaching incubation |

**Action:** Produce a security self-assessment following the
[CNCF TAG Security template](https://github.com/cncf/tag-security/blob/main/community/assessments/guide/self-assessment.md).
This requires input from all maintainers and is recommended before the project
applies for CNCF incubation. [Follow-up issue recommended]

---

## 3. SECURITY.md

| Control | Status | Notes |
| --- | --- | --- |
| Repo-level `SECURITY.md` | ✅ Added | `SECURITY.md` at repository root (this PR) |
| Security contacts listed | ✅ Added | Maintainers via `CODEOWNERS`; TC via mailing list |
| Vulnerability reporting process | ✅ Added | GitHub private advisory + mailing list |
| Embargo / coordinated disclosure policy | ✅ Added | 90-day default; see `SECURITY.md` |
| Security notifications template | ⚠️ Deferred | Can be added when the first advisory is published |

---

## 4. Incident Response

| Control | Status | Notes |
| --- | --- | --- |
| Documented incident response process | ⚠️ Partial | Basic SLA captured in `SECURITY.md`; full runbook not yet written |

**Action:** Document a full incident response runbook covering triage,
replication, CVE assignment, patch coordination, and disclosure steps. A
template is available at the
[CNCF TAG Security GitHub repository](https://github.com/cncf/tag-security/blob/main/community/resources/project-resources/templates/incident-response.md).
[Follow-up issue recommended]

---

## 5. OpenSSF Best Practices Badging

| Control | Status | Notes |
| --- | --- | --- |
| OpenSSF Best Practices badge | ❌ Not registered | Registration is free and self-certified |
| OpenSSF Scorecard (automated) | ✅ In place | `.github/workflows/ossf-scorecard.yml` runs weekly |

**Action:** Register the project at
<https://bestpractices.coreinfrastructure.org/en> and work toward at minimum a
**Passing** badge. The Scorecard workflow already satisfies several criteria
automatically. [Requires maintainer action — admin login to bestpractices.coreinfrastructure.org]

---

## Summary of Recommended Follow-up Issues

| # | Action | Owner |
| --- | --- | --- |
| 1 | Evaluate enforcing GPG/SSH commit signing on `main` | Maintainers |
| 2 | Produce CNCF TAG Security self-assessment document | Maintainers + contributors |
| 3 | Write full incident response runbook | Maintainers |
| 4 | Register project on OpenSSF Best Practices badge platform | Maintainer (admin action) |
| 5 | Enable GitHub private vulnerability reporting in repository settings (may not be active yet) | Maintainer (admin action) |
