#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
readonly HOURLY_LIMIT=10
WORK_DIR=''

cleanup() {
  if [[ -n "${WORK_DIR}" ]]; then
    git worktree remove --force "${WORK_DIR}/checkout" >/dev/null 2>&1 || true
    rm -rf "${WORK_DIR}"
  fi
}
trap cleanup EXIT

recent_requests() {
  local runs id jobs prior requests='[]'
  # A scan starts within one hour and lasts at most ten minutes. Three hours covers the budget window.
  runs="$(gh api --method GET "repos/${REPO}/actions/workflows/autofix-bundle-conflicts.yaml/runs" \
    -f branch=main -f "created=>=$2" -f per_page=100 --paginate --slurp \
    | jq -ce --argjson current "${RUN_ID}" '
      if type == "array" and length > 0 and
        all(.[]; (.total_count | type) == "number" and .total_count <= 1000) then
        [.[].workflow_runs[]] as $runs |
        if ($runs | length) != (map(.total_count) | max) then
          error("Incomplete workflow run pages")
        else [$runs[] | select(.id != $current) | .id] end
      else error("Cannot verify the repair budget") end')" || return 1
  while IFS= read -r id; do
    [[ "${id}" =~ ^[1-9][0-9]*$ ]] || return 1
    jobs="$(gh api "repos/${REPO}/actions/runs/${id}/attempts/1/jobs?per_page=100" --paginate --slurp)" || return 1
    prior="$(jq -ce --arg since "$1" --argjson limit "${HOURLY_LIMIT}" '
      if type == "array" and length > 0 and
        all(.[]; (.jobs | type) == "array" and (.total_count | type) == "number") then .
      else error("Missing workflow job pages") end |
      [.[].jobs[]] as $jobs |
      if ($jobs | length) != (map(.total_count) | max) then
        error("Incomplete workflow job pages")
      elif ($jobs | length) > 0 and all($jobs[]; .conclusion == "skipped") then []
      else
        [$jobs[] | select(.name == "Select bundle repairs")] as $selection |
        if ($selection | length) != 1 then error("Missing repair selection history")
        elif any($selection[0].steps[]?;
          .name == "Select bounded bundle repairs" and .conclusion == "skipped") then []
        else
          [$selection[0].steps[]? | .name |
            select(startswith("Bundle repair selection: ")) |
            ltrimstr("Bundle repair selection: ") | fromjson] as $records |
          if ($records | length) == 1 then
            $records[0] as $record |
            if ($record | type) != "object" or
              ($record.at | type) != "number" or $record.at != ($record.at | floor) or
              $record.at <= 0 or $record.at > now or
              ($record.prs | type) != "array" or ($record.prs | length) > $limit or
              any($record.prs[]; type != "number" or . <= 0 or . != floor) or
              ($record.prs | unique | length) != ($record.prs | length) then
              error("Invalid repair selection record")
            elif $record.at >= ($since | fromdateiso8601) then $record.prs else [] end
          else error("Cannot prove the repair selection was empty") end
        end
      end' <<<"${jobs}")" || return 1
    requests="$(jq -cn --argjson previous "${requests}" --argjson added "${prior}" '$previous + $added')" || return 1
  done < <(jq -r '.[]' <<<"${runs}")
  printf '%s\n' "${requests}"
}

load_budget() {
  local since lookback run
  since="$(jq -nr 'now - 3600 | strftime("%Y-%m-%dT%H:%M:%SZ")')"
  lookback="$(jq -nr 'now - 10800 | strftime("%Y-%m-%dT%H:%M:%SZ")')"
  run="$(gh api "repos/${REPO}/actions/runs/${RUN_ID}")"
  if ! jq -e '.run_attempt == 1 and (.created_at | fromdateiso8601) <= now
    and (.created_at | fromdateiso8601) + 3600 > now' <<<"${run}" >/dev/null; then
    printf 'Start a new scanner run. This run is stale or is a retry.\n' >&2
    return 1
  fi
  AUTO_DEADLINE="$(jq -r '.created_at | fromdateiso8601 + 3600' <<<"${run}")"
  RECENT_REQUESTS="$(recent_requests "${since}" "${lookback}")"
}

main() {
  : "${REPO:?REPO must be set}"
  : "${GH_TOKEN:?GH_TOKEN must be set}"
  : "${RUN_ID:?RUN_ID must be set}"
  : "${GITHUB_OUTPUT:?GITHUB_OUTPUT must be set}"
  printf 'prs=[]\n' >>"${GITHUB_OUTPUT}"
  [[ "${REPO}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ && "${RUN_ID}" =~ ^[1-9][0-9]*$ ]]
  [[ "$#" == 0 || ("$#" == 1 && "$1" == '--budget') ]]
  if [[ "${1:-}" == '--budget' || -z "${RECENT_REQUESTS+x}" ]]; then
    load_budget
  fi
  [[ "${AUTO_DEADLINE:-}" =~ ^[1-9][0-9]{0,10}$ ]]
  ((AUTO_DEADLINE > $(jq -nr 'now | floor')))
  jq -e 'type == "array" and all(.[]; type == "number" and . > 0 and . == floor)' \
    <<<"${RECENT_REQUESTS}" >/dev/null
  local requests count remaining selected='[]'
  requests="${RECENT_REQUESTS}"
  count="$(jq 'length' <<<"${requests}")"
  remaining=$((HOURLY_LIMIT - count))
  if [[ "${1:-}" == '--budget' ]]; then
    {
      printf 'requests=%s\n' "${requests}"
      printf 'expires_at=%s\n' "${AUTO_DEADLINE}"
      printf 'remaining=%s\n' "$((remaining > 0 ? remaining : 0))"
    } >>"${GITHUB_OUTPUT}"
    return
  fi
  if ((remaining <= 0)); then
    printf 'Automatic repair limit reached: %s requests in the last hour.\n' "${count}"
    return
  fi

  git fetch --no-prune --no-tags origin 'refs/heads/main:refs/remotes/origin/main'
  local base prs
  base="$(git rev-parse refs/remotes/origin/main)"
  prs="$(gh pr list --repo "${REPO}" --state open --limit 200 \
    --json number,isDraft,mergeable,baseRefName \
    --jq '.[] | select(.isDraft == false and .mergeable == "CONFLICTING" and .baseRefName == "main") | .number')"
  WORK_DIR="$(mktemp -d)"
  local num meta head
  while IFS= read -r num; do
    ((AUTO_DEADLINE > $(jq -nr 'now | floor')))
    [[ -n "${num}" ]] || continue
    [[ "${num}" =~ ^[1-9][0-9]*$ ]]
    if jq -e --argjson pr "${num}" 'any(.[]; . == $pr)' <<<"${requests}" >/dev/null; then
      printf 'PR #%s has a request in the last hour. Skipping.\n' "${num}"
      continue
    fi
    meta="$(gh api "repos/${REPO}/pulls/${num}")"
    if ! jq -e --arg repository "${REPO}" \
      '.state == "open" and .draft == false and .base.ref == "main"
       and .head.repo != null and (.head.repo.full_name == $repository or .maintainer_can_modify == true)' \
      <<<"${meta}" >/dev/null; then
      printf 'PR #%s is not eligible for automatic repair.\n' "${num}"
      continue
    fi
    head="$(jq -r '.head.sha' <<<"${meta}")"
    [[ "${head}" =~ ^[0-9a-f]{40}$ ]]
    git fetch --no-prune --no-tags origin "refs/pull/${num}/head"
    [[ "$(git rev-parse FETCH_HEAD)" == "${head}" ]] || continue
    git -c core.hooksPath=/dev/null worktree add --detach "${WORK_DIR}/checkout" "${head}"
    if bash "${SCRIPT_DIR}/merge-bundle-branch.sh" "${WORK_DIR}/checkout" "${base}"; then
      if [[ "${DRY_RUN:-false}" == 'true' ]]; then
        printf 'Dry run: would request bundle repair for PR #%s.\n' "${num}"
      else
        selected="$(jq -c --argjson pr "${num}" --arg head "${head}" --arg base "${base}" \
          --argjson expires "${AUTO_DEADLINE}" \
          '. + [{pr_number:$pr, head_sha:$head, base_sha:$base, expires_at:$expires}]' <<<"${selected}")"
        printf 'Selected bundle repair for PR #%s.\n' "${num}"
      fi
      remaining=$((remaining - 1))
    else
      printf 'PR #%s has conflicts outside the bundle. Skipping.\n' "${num}"
    fi
    git worktree remove --force "${WORK_DIR}/checkout"
    ((remaining > 0)) || break
  done <<<"${prs}"
  {
    printf 'prs=%s\n' "${selected}"
    printf 'rate_record=%s\n' "$(jq -c '{at:(now | floor), prs:map(.pr_number)}' <<<"${selected}")"
  } >>"${GITHUB_OUTPUT}"
}

main "$@"
