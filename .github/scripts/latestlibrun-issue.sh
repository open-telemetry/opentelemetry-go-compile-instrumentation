#!/bin/bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -EeufCo pipefail

# Open or update a GitHub issue when the LatestLibRun workflow fails.
#
# Dedup strategy: search for open issues labelled `latestlibrun-failure`.
# If one exists, append a comment linking the new failed run.
# If none exists, create a new issue with the label.
#
# Required environment variables:
#   GH_TOKEN  - GitHub token with issues:write permission
#   RUN_URL   - URL of the failing workflow run

: "${GH_TOKEN:?GH_TOKEN must be set}"
: "${RUN_URL:?RUN_URL must be set}"

LABEL="latestlibrun-failure"
TODAY="$(date -u +%Y-%m-%d)"
TITLE="LatestLibRun failed on main (${TODAY})"

existing_issue="$(gh issue list \
  --label "${LABEL}" \
  --state open \
  --json number \
  --jq '.[0].number // empty')"

if [[ -n "${existing_issue}" ]]; then
  echo "Appending comment to existing issue #${existing_issue}"
  gh issue comment "${existing_issue}" \
    --body "LatestLibRun failed again: ${RUN_URL}"
else
  echo "Creating new issue with label '${LABEL}'"
  gh issue create \
    --title "${TITLE}" \
    --label "${LABEL}" \
    --body "$(cat <<EOF
The [LatestLibRun workflow](${RUN_URL}) failed on \`main\`.

This means the latest release of an upstream instrumented library introduced a **runtime behavior change** (removed function, changed signature, dropped instrumentation hook point) that breaks existing integration tests.

## Remediation

1. Identify the failing test and the upstream library version that caused the break.
2. Cap the existing rule's version range in the relevant \`pkg/instrumentation/.../*.yaml\` file.
3. Open a new rule entry for the new version range and update the hook implementation.
4. Close this issue once the fix lands on \`main\`.

See [docs/testing.md](https://github.com/${GITHUB_REPOSITORY}/blob/main/docs/testing.md#latestlibrun-tests) for full remediation steps.
EOF
)"
fi
