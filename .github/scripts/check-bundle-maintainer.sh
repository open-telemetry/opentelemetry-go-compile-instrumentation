#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o nounset
set -o pipefail

check_automatic_request() {
  if [[ "${RUN_ATTEMPT:-1}" != 1 ]]; then
    printf 'Start a new scanner run instead of retrying automatic repairs.\n' >&2
    return 1
  fi
  local now current reference
  now="$(jq -nr 'now | floor')"
  if [[ ! "${AUTO_DEADLINE:-}" =~ ^[1-9][0-9]{0,10}$ ]] \
    || ((AUTO_DEADLINE <= now || AUTO_DEADLINE > now + 3600)); then
    printf 'The automatic request has no valid unexpired deadline.\n' >&2
    return 1
  fi
  if [[ -n "${PR_NUMBER:-}" ]]; then
    [[ "${PR_NUMBER}" =~ ^[1-9][0-9]*$ &&
      "${EXPECTED_HEAD_SHA:-}" =~ ^[0-9a-f]{40}$ && "${BASE_SHA:-}" =~ ^[0-9a-f]{40}$ ]]
    current="$(gh api "repos/${REPO}/pulls/${PR_NUMBER}")"
    if ! jq -e --arg head "${EXPECTED_HEAD_SHA}" --arg repository "${REPO}" '
      .state == "open" and .draft == false and .base.ref == "main"
      and .head.sha == $head and .head.repo != null
      and (.head.repo.full_name == $repository or .maintainer_can_modify == true)' \
      <<<"${current}" >/dev/null; then
      printf 'The automatic request changed or is no longer eligible.\n' >&2
      return 1
    fi
    reference="$(gh api "repos/${REPO}/git/ref/heads/main")"
    if ! jq -e --arg base "${BASE_SHA}" '.object.type == "commit" and .object.sha == $base' \
      <<<"${reference}" >/dev/null; then
      printf 'The captured base branch head is no longer current.\n' >&2
      return 1
    fi
  fi
}

main() {
  : "${REPO:?REPO must be set}"
  [[ "${REPO}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]
  if [[ "${CALLER_WORKFLOW_REF:-}" == "${REPO}/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/main" ]]; then
    check_automatic_request
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
