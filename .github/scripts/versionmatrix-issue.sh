#!/bin/bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -EeufCo pipefail

# Open or update a GitHub issue when the VersionMatrix workflow fails.
#
# Dedup strategy: search for open issues labelled `versionmatrix-failure`.
# If one exists, append a comment linking the new failed run.
# If none exists, create a new issue with the label.
#
# Required environment variables:
#   GH_TOKEN  - GitHub token with issues:write permission
#   RUN_URL   - URL of the failing workflow run

: "${GH_TOKEN:?GH_TOKEN must be set}"
: "${RUN_URL:?RUN_URL must be set}"

LABEL="versionmatrix-failure"
TODAY="$(date -u +%Y-%m-%d)"
TITLE="VersionMatrix failed on main (${TODAY})"

existing_issue="$(gh issue list \
  --label "${LABEL}" \
  --state open \
  --limit 1 \
  --json number \
  --jq '.[0].number // empty')"

if [[ -n "${existing_issue}" ]]; then
  echo "Appending comment to existing issue #${existing_issue}"
  gh issue comment "${existing_issue}" \
    --body "VersionMatrix failed again: ${RUN_URL}"
else
  echo "Creating new issue with label '${LABEL}'"
  gh issue create \
    --title "${TITLE}" \
    --label "${LABEL}" \
    --body "$(cat <<EOF
The [VersionMatrix workflow](${RUN_URL}) failed on \`main\`.

This means a declared version range in \`pkg/instrumentation/.../*.yaml\` does **not** work at one of its bounds: either the instrumented library version cannot be installed (another module in the build graph forces a different version) or the integration suite fails against it.

## Remediation

1. Identify the failing test and the library version that caused the break.
2. Fix the rule's version range in the relevant \`pkg/instrumentation/.../*.yaml\` file: raise the lower bound to the oldest version that actually works, or cap the range below the first version that breaks.
3. If a wider range should stay supported, split the rule and add an implementation for the versions the current hook cannot handle.
4. Close this issue once the fix lands on \`main\`.

See [docs/testing.md](https://github.com/${GITHUB_REPOSITORY}/blob/main/docs/testing.md#versionmatrix-tests) for full remediation steps.
EOF
)"
fi
