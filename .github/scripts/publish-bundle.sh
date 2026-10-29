#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o nounset
set -o pipefail

readonly BUNDLE='tool/data/otelc-bundle.tgz'
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR

check_pull_request() {
  if [[ "${CALLER_WORKFLOW_REF:-}" == "${REPO}/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/main" ]]; then
    bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"
  fi
  local current reference base_ref_uri
  current="$(gh api "repos/${REPO}/pulls/${PR_NUMBER}")"
  base_ref_uri="$(jq -nr --arg ref "${BASE_REF}" '$ref | @uri')"
  reference="$(gh api "repos/${REPO}/git/ref/heads/${base_ref_uri}")"
  if ! jq -e --arg head "${EXPECTED_HEAD_SHA}" --arg base "${BASE_SHA}" \
    --arg branch "${HEAD_REF}" --arg base_ref "${BASE_REF}" --arg repository "${HEAD_REPO_FULL}" \
    --argjson reference "${reference}" \
    '.state == "open" and .head.sha == $head
     and .head.ref == $branch and .base.ref == $base_ref and .head.repo.full_name == $repository
     and $reference.object.type == "commit" and $reference.object.sha == $base' \
    <<<"${current}" >/dev/null; then
    printf 'The pull request closed or changed. No update will be pushed.\n' >&2
    return 1
  fi
  if [[ "${HEAD_REPO_FULL}" != "${REPO}" ]] && ! jq -e '.maintainer_can_modify == true' <<<"${current}" >/dev/null; then
    local repository
    repository="$(gh api "repos/${HEAD_REPO_FULL}")"
    if ! jq -e '.permissions.push == true' <<<"${repository}" >/dev/null; then
      printf 'The fork author must permit maintainer edits before publication.\n' >&2
      return 1
    fi
  fi
}

main() {
  : "${GH_TOKEN:?A publisher token is required}"
  : "${REPO:?REPO must be set}"
  : "${PR_NUMBER:?PR_NUMBER must be set}"
  : "${HEAD_REPO_FULL:?HEAD_REPO_FULL must be set}"
  : "${HEAD_REF:?HEAD_REF must be set}"
  : "${EXPECTED_HEAD_SHA:?EXPECTED_HEAD_SHA must be set}"
  : "${BASE_REF:?BASE_REF must be set}"
  : "${BASE_SHA:?BASE_SHA must be set}"
  : "${BOT_LOGIN:?BOT_LOGIN must be set}"
  : "${BOT_ID:?BOT_ID must be set}"
  : "${GENERATED_BUNDLE:?GENERATED_BUNDLE must be set}"
  if [[ ! "${REPO}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ||
    ! "${HEAD_REPO_FULL}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ||
    ! "${PR_NUMBER}" =~ ^[1-9][0-9]*$ || ! "${BOT_ID}" =~ ^[0-9]+$ ||
    ! "${BASE_SHA}" =~ ^[0-9a-f]{40}$ || ! "${EXPECTED_HEAD_SHA}" =~ ^[0-9a-f]{40}$ ]]; then
    printf 'Invalid publication metadata.\n' >&2
    return 1
  fi
  git check-ref-format "refs/heads/${HEAD_REF}"
  git check-ref-format "refs/heads/${BASE_REF}"
  [[ "$(git rev-parse HEAD)" == "${EXPECTED_HEAD_SHA}" ]]
  local merge_head
  merge_head="$(git rev-parse -q --verify MERGE_HEAD || true)"
  if [[ -n "${merge_head}" ]]; then
    [[ "${merge_head}" == "${BASE_SHA}" ]]
  else
    git merge-base --is-ancestor "${BASE_SHA}" HEAD
  fi
  check_pull_request
  if [[ ! -f "${GENERATED_BUNDLE}" || -L "${GENERATED_BUNDLE}" ||
    ! -d tool || -L tool || ! -d tool/data || -L tool/data || -L "${BUNDLE}" ]]; then
    printf 'The bundle and its destination must be regular paths, not symbolic links.\n' >&2
    return 1
  fi
  cp "${GENERATED_BUNDLE}" "${BUNDLE}"
  git add -- "${BUNDLE}"
  if [[ -z "${merge_head}" ]] && git diff --cached --quiet; then
    printf 'The bundle is already up to date.\n'
    return
  fi
  git -c core.hooksPath=/dev/null -c commit.gpgsign=false \
    -c "user.name=${BOT_LOGIN}" -c "user.email=${BOT_ID}+${BOT_LOGIN}@users.noreply.github.com" \
    commit -m "chore: merge ${BASE_REF} and regenerate otelc-bundle.tgz"
  git merge-base --is-ancestor "${EXPECTED_HEAD_SHA}" HEAD
  check_pull_request
  local authorization
  authorization="$(printf 'x-access-token:%s' "${GH_TOKEN}" | base64 | tr -d '\n')"
  # The lease rejects concurrent updates. The ancestor check forbids history rewrites.
  GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=http.https://github.com/.extraheader \
    GIT_CONFIG_VALUE_0="AUTHORIZATION: basic ${authorization}" \
    GIT_CONFIG_KEY_1=core.hooksPath GIT_CONFIG_VALUE_1=/dev/null \
    git push --force-with-lease="refs/heads/${HEAD_REF}:${EXPECTED_HEAD_SHA}" \
    origin "HEAD:refs/heads/${HEAD_REF}"
}

main "$@"
