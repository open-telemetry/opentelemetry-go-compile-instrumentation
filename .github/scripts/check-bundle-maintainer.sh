#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o nounset
set -o pipefail

main() {
  : "${REPO:?REPO must be set}"
  [[ "${REPO}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]
  if [[ "${CALLER_WORKFLOW_REF:-}" == "${REPO}/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/main" ]]; then
    if [[ "${RUN_ATTEMPT:-1}" != 1 ]]; then
      printf 'Start a new scanner run instead of retrying automatic repairs.\n' >&2
      return 1
    fi
    return
  fi
  if [[ -n "${CALLER_WORKFLOW_REF:-}" &&
    "${CALLER_WORKFLOW_REF}" != "${REPO}/.github/workflows/regenerate-bundle.yaml@refs/heads/main" ]]; then
    printf 'This workflow is not an authorized bundle repair caller.\n' >&2
    return 1
  fi
  : "${COMMENTER:?COMMENTER must be set}"
  local -a actors=("${COMMENTER}")
  if [[ "${RUN_ATTEMPT:-1}" != 1 ]]; then
    : "${TRIGGERING_ACTOR:?TRIGGERING_ACTOR must be set on reruns}"
    actors+=("${TRIGGERING_ACTOR}")
  fi
  local actor permission
  for actor in "${actors[@]}"; do
    [[ "${actor}" =~ ^[A-Za-z0-9-]+$ ]]
    permission="$(gh api "repos/${REPO}/collaborators/${actor}/permission")"
    # GitHub reports maintain access as permission=write, so inspect role_name.
    if ! jq -e '.role_name == "maintain" or .role_name == "admin"' <<<"${permission}" >/dev/null; then
      printf '%s must have the maintain or admin role to regenerate bundles.\n' "${actor}" >&2
      return 1
    fi
  done
}

main "$@"
