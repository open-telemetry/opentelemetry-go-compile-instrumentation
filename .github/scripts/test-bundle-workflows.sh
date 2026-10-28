#!/usr/bin/env bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK_DIR="$(mktemp -d)"
readonly SCRIPT_DIR WORK_DIR
trap 'rm -rf "${WORK_DIR}"' EXIT

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

fixture() {
  local checkout
  checkout="$(mktemp -d "${WORK_DIR}/repo.XXXXXX")"
  git init --quiet --initial-branch=main "${checkout}"
  cd "${checkout}"
  git config user.name 'Test Author'
  git config user.email 'test@example.com'
  git config commit.gpgsign false
  mkdir -p tool/data
  printf 'original\0bundle' >tool/data/otelc-bundle.tgz
  printf 'original\n' >source.txt
  git add tool/data/otelc-bundle.tgz source.txt
  git commit --quiet -m initial
  git checkout --quiet -b contribution
  printf 'contribution\0bundle' >tool/data/otelc-bundle.tgz
  git commit --quiet -am contribution
  git checkout --quiet main
  printf 'base\0bundle' >tool/data/otelc-bundle.tgz
  printf 'base change\n' >base.txt
  git add tool/data/otelc-bundle.tgz base.txt
  git commit --quiet -m base
  BASE_SHA="$(git rev-parse HEAD)"
  git checkout --quiet contribution
}

test_source_conflict() {
  fixture
  git checkout --quiet main
  printf 'base source\n' >source.txt
  git commit --quiet -am 'base source'
  BASE_SHA="$(git rev-parse HEAD)"
  git checkout --quiet contribution
  printf 'contributor source\n' >source.txt
  printf '* merge=otelc-bundle\n' >.gitattributes
  git add source.txt .gitattributes
  git commit --quiet -m 'contributor source and attributes'
  if bash "${SCRIPT_DIR}/merge-bundle-branch.sh" . "${BASE_SHA}" >"${WORK_DIR}/conflict.log" 2>&1; then
    fail 'contributor attributes hid a source conflict'
  fi
  [[ "$(git diff --name-only --diff-filter=U)" == 'source.txt' ]] || fail 'the source conflict disappeared'
  printf 'PASS: contributor attributes cannot hide source conflicts\n'
}

test_authorization() {
  mkdir -p "${WORK_DIR}/bin"
  cat >"${WORK_DIR}/bin/gh" <<'MOCK'
#!/usr/bin/env bash
cat "${GH_RESPONSE}"
MOCK
  chmod +x "${WORK_DIR}/bin/gh"
  export PATH="${WORK_DIR}/bin:${PATH}" GH_RESPONSE="${WORK_DIR}/permission.json"
  export REPO=test/upstream COMMENTER=maintainer
  printf '{"permission":"write","role_name":"maintain"}\n' >"${GH_RESPONSE}"
  bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"
  printf '{"permission":"admin","role_name":"admin"}\n' >"${GH_RESPONSE}"
  bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"
  printf '{"permission":"write","role_name":"write"}\n' >"${GH_RESPONSE}"
  if bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"; then
    fail 'write access alone authorized a command'
  fi
  printf 'PASS: only maintainers and administrators can invoke the command\n'
  export COMMENTER='github-actions[bot]' RUN_ATTEMPT=1
  if bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"; then
    fail 'a bot name alone authorized a request'
  fi
  export CALLER_WORKFLOW_REF="${REPO}/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/main"
  bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"
  RUN_ATTEMPT=2
  if bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"; then
    fail 'an automatic rerun bypassed the request budget'
  fi
  RUN_ATTEMPT=1
  CALLER_WORKFLOW_REF="${REPO}/.github/workflows/autofix-bundle-conflicts.yaml@refs/heads/contribution"
  if bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"; then
    fail 'an untrusted workflow branch authorized a request'
  fi
  export COMMENTER=maintainer
  printf '{"role_name":"maintain"}\n' >"${GH_RESPONSE}"
  if bash "${SCRIPT_DIR}/check-bundle-maintainer.sh"; then
    fail 'a maintainer actor hid an untrusted reusable caller'
  fi
  unset CALLER_WORKFLOW_REF RUN_ATTEMPT
  printf 'PASS: automatic requests require the trusted caller and a new scan\n'
}

publication_fixture() {
  fixture
  EXPECTED_HEAD_SHA="$(git rev-parse HEAD)"
  export EXPECTED_HEAD_SHA BASE_SHA
  export REPO=test/upstream HEAD_REPO_FULL=test/fork HEAD_REF=contribution BASE_REF=main PR_NUMBER=1
  export BOT_LOGIN=test-bot BOT_ID=123 GH_TOKEN=test-token
  export GH_RESPONSE="${WORK_DIR}/pull.json"
  REMOTE="$(mktemp -d "${WORK_DIR}/remote.XXXXXX")/fork.git"
  git clone --quiet --bare . "${REMOTE}"
  git remote add origin "${REMOTE}"
  GENERATED_BUNDLE="$(mktemp "${WORK_DIR}/bundle.XXXXXX")"
  export GENERATED_BUNDLE
  printf 'regenerated\0bundle' >"${GENERATED_BUNDLE}"
  jq -n --arg head "${EXPECTED_HEAD_SHA}" --arg base "${BASE_SHA}" \
    '{state:"open", maintainer_can_modify:true, head:{sha:$head, ref:"contribution", repo:{full_name:"test/fork"}}, base:{sha:$base, ref:"main"}}' >"${GH_RESPONSE}"
  bash "${SCRIPT_DIR}/merge-bundle-branch.sh" . "${BASE_SHA}"
}

test_fork_publication() {
  publication_fixture
  printf '#!/usr/bin/env bash\ntouch "%s"\n' "${WORK_DIR}/hook-ran" >.git/hooks/pre-commit
  chmod +x .git/hooks/pre-commit
  bash "${SCRIPT_DIR}/publish-bundle.sh"
  [[ "$(git rev-parse HEAD^1)" == "${EXPECTED_HEAD_SHA}" ]] || fail 'the contribution parent changed'
  [[ "$(git rev-parse HEAD^2)" == "${BASE_SHA}" ]] || fail 'the base parent changed'
  [[ "$(git --git-dir="${REMOTE}" rev-parse contribution)" == "$(git rev-parse HEAD)" ]] || fail 'the fork was not updated'
  git --git-dir="${REMOTE}" show contribution:tool/data/otelc-bundle.tgz >"${WORK_DIR}/published"
  cmp "${GENERATED_BUNDLE}" "${WORK_DIR}/published" || fail 'the published bundle differs'
  [[ ! -e "${WORK_DIR}/hook-ran" ]] || fail 'a contributor hook ran during publication'
  printf 'PASS: publish the merged bundle to a fork without executing hooks\n'
  EXPECTED_HEAD_SHA="$(git rev-parse HEAD)"
  jq --arg head "${EXPECTED_HEAD_SHA}" '.head.sha = $head' "${GH_RESPONSE}" >"${GH_RESPONSE}.new"
  mv "${GH_RESPONSE}.new" "${GH_RESPONSE}"
  bash "${SCRIPT_DIR}/publish-bundle.sh"
  [[ "$(git rev-parse HEAD)" == "${EXPECTED_HEAD_SHA}" ]] || fail 'a duplicate request created a redundant commit'
  printf 'PASS: an unchanged bundle does not create another commit\n'
}

test_empty_merge() {
  publication_fixture
  git merge --abort
  git show main:base.txt >base.txt
  git add base.txt
  git commit --quiet -m 'include base change'
  EXPECTED_HEAD_SHA="$(git rev-parse HEAD)"
  git --git-dir="${REMOTE}" fetch --no-prune --quiet "${PWD}" refs/heads/contribution:refs/heads/contribution
  jq --arg head "${EXPECTED_HEAD_SHA}" '.head.sha = $head' "${GH_RESPONSE}" >"${GH_RESPONSE}.new"
  mv "${GH_RESPONSE}.new" "${GH_RESPONSE}"
  cp tool/data/otelc-bundle.tgz "${GENERATED_BUNDLE}"
  bash "${SCRIPT_DIR}/merge-bundle-branch.sh" . "${BASE_SHA}"
  git diff --cached --quiet || fail 'the empty-merge fixture has a staged diff'
  bash "${SCRIPT_DIR}/publish-bundle.sh"
  [[ "$(git rev-parse HEAD^2)" == "${BASE_SHA}" ]] || fail 'an empty staged diff lost the required merge'
  printf 'PASS: preserve the merge commit when the index equals the head\n'
}

test_repository_and_direct_access() {
  local scenario filter
  for scenario in repository direct; do
    publication_fixture
    if [[ "${scenario}" == repository ]]; then
      HEAD_REPO_FULL="${REPO}"
      filter='.head.repo.full_name = "test/upstream" | .maintainer_can_modify = false'
    else
      filter='.maintainer_can_modify = false | .permissions.push = true'
    fi
    jq "${filter}" "${GH_RESPONSE}" >"${GH_RESPONSE}.new"
    mv "${GH_RESPONSE}.new" "${GH_RESPONSE}"
    bash "${SCRIPT_DIR}/publish-bundle.sh"
    [[ "$(git --git-dir="${REMOTE}" rev-parse contribution)" == "$(git rev-parse HEAD)" ]] || fail "publication rejected ${scenario} access"
    printf 'PASS: publish with %s access\n' "${scenario}"
  done
}

test_publication_rejections() {
  local scenario filter
  for scenario in closed head base repository edits symlink advanced; do
    publication_fixture
    filter='.'
    case "${scenario}" in
      closed) filter='.state = "closed"' ;;
      head) filter='.head.sha = "0000000000000000000000000000000000000000"' ;;
      base) filter='.base.sha = "0000000000000000000000000000000000000000"' ;;
      repository) filter='.head.repo.full_name = "other/fork"' ;;
      edits) filter='.maintainer_can_modify = false' ;;
      symlink)
        mv "${GENERATED_BUNDLE}" "${GENERATED_BUNDLE}.target"
        ln -s "${GENERATED_BUNDLE}.target" "${GENERATED_BUNDLE}"
        ;;
      advanced)
        git --git-dir="${REMOTE}" update-ref refs/heads/contribution "${BASE_SHA}"
        ;;
    esac
    jq "${filter}" "${GH_RESPONSE}" >"${GH_RESPONSE}.new"
    mv "${GH_RESPONSE}.new" "${GH_RESPONSE}"
    local before
    before="$(git --git-dir="${REMOTE}" rev-parse contribution)"
    if bash "${SCRIPT_DIR}/publish-bundle.sh" >"${WORK_DIR}/reject.log" 2>&1; then
      fail "publication accepted ${scenario}"
    fi
    [[ "$(git --git-dir="${REMOTE}" rev-parse contribution)" == "${before}" ]] || fail "publication overwrote ${scenario}"
    printf 'PASS: reject publication for %s\n' "${scenario}"
  done
}

scanner_fixture() {
  publication_fixture
  git merge --abort
  git --git-dir="${REMOTE}" fetch --no-prune --quiet "${PWD}" "${EXPECTED_HEAD_SHA}:refs/pull/1/head" "${EXPECTED_HEAD_SHA}:refs/pull/2/head"
  export RUN_ID=999 GITHUB_OUTPUT="${WORK_DIR}/outputs"
  export RUNS_JSON="${WORK_DIR}/runs.json" JOBS_JSON="${WORK_DIR}/jobs.json" RUN_JSON="${WORK_DIR}/run.json"
  printf '[{"total_count":2,"workflow_runs":[{"id":123},{"id":999}]}]\n' >"${RUNS_JSON}"
  printf '[{"total_count":0,"jobs":[]}]\n' >"${JOBS_JSON}"
  jq -n '{created_at:(now | strftime("%Y-%m-%dT%H:%M:%SZ")), run_attempt:1}' >"${RUN_JSON}"
  : >"${GITHUB_OUTPUT}"
  cat >"${WORK_DIR}/bin/gh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  'api repos/test/upstream/actions/runs/999') cat "${RUN_JSON}" ;;
  'api --method GET repos/test/upstream/actions/workflows/autofix-bundle-conflicts.yaml/runs '*)
    if [[ "$*" == *--slurp* && "$*" == *--jq* ]]; then
      printf 'gh api does not support --slurp with --jq\n' >&2
      exit 1
    fi
    cat "${RUNS_JSON}"
    ;;
  'api repos/test/upstream/actions/runs/123/attempts/1/jobs?per_page=100 --paginate --slurp') cat "${JOBS_JSON}" ;;
  'pr list '*) printf '1\n2\n' ;;
  'api repos/test/upstream/pulls/'*) cat "${GH_RESPONSE}" ;;
  *) printf 'Unexpected gh call: %s\n' "$*" >&2; exit 1 ;;
esac
MOCK
  chmod +x "${WORK_DIR}/bin/gh"
}

scanner_history() {
  jq -n --argjson first "$1" --argjson last "$2" \
    '[{jobs:([{name:"Select bundle repairs", completed_at:(now | strftime("%Y-%m-%dT%H:%M:%SZ"))}] +
    [range($first;$last) as $pr | ["Prepare bundle regeneration", "Generate bundle", "Push regenerated bundle"][] |
    {name:("Bundle repair #" + ($pr | tostring) + " / " + .), conclusion:"failure"}])}] |
    map(. + {total_count:(.jobs | length)})' >"${JOBS_JSON}"
}

check_plan() {
  local actual
  actual="$(grep '^prs=' "${GITHUB_OUTPUT}" | tail -n 1 | cut -d= -f2-)"
  [[ "${actual}" == "$1" ]] || fail "expected plan $1, got ${actual}"
}

test_scanner_limit() {
  scanner_fixture
  scanner_history 1 11
  DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"
  check_plan '[]'
  printf 'PASS: failed requests count toward the automatic hourly limit\n'
}

test_scanner_selection() {
  scanner_fixture
  scanner_history 10 19
  DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"
  check_plan '[1]'
  printf 'PASS: select an editable fork within the remaining hourly budget\n'

  scanner_history 1 2
  DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"
  check_plan '[2]'
  printf 'PASS: skip a pull request during its one-hour cooldown\n'

  printf '[{"total_count":0,"jobs":[]}]\n' >"${JOBS_JSON}"
  DRY_RUN=true bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"
  check_plan '[]'
  printf 'PASS: dry run probes forks without requesting repairs\n'

  scanner_history 1 11
  jq '.[0].jobs[0].completed_at = (now - 7200 | strftime("%Y-%m-%dT%H:%M:%SZ"))' "${JOBS_JSON}" >"${JOBS_JSON}.new"
  mv "${JOBS_JSON}.new" "${JOBS_JSON}"
  DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"
  check_plan '[1,2]'
  printf 'PASS: expired requests do not consume the current hourly budget\n'

  printf 'invalid\n' >"${RUNS_JSON}"
  if DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh" >"${WORK_DIR}/invalid-runs.log" 2>&1; then
    fail 'the scanner accepted an invalid rate-limit response'
  fi
  check_plan '[]'
  printf 'PASS: fail closed when the rate-limit query fails\n'

  printf '[{"total_count":1,"workflow_runs":[{"id":123}]}]\n' >"${RUNS_JSON}"
  scanner_history 1 2
  jq '.[0] |= (.jobs |= .[1:] | .total_count -= 1)' "${JOBS_JSON}" >"${JOBS_JSON}.new"
  mv "${JOBS_JSON}.new" "${JOBS_JSON}"
  if DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh" >"${WORK_DIR}/incomplete-jobs.log" 2>&1; then
    fail 'the scanner accepted incomplete request history'
  fi
  check_plan '[]'
  printf 'PASS: incomplete selection history cannot reset the budget\n'

  local response
  for response in '[]' '[{"total_count":1001,"workflow_runs":[]}]' '[{"total_count":2,"workflow_runs":[{"id":123}]}]'; do
    printf '%s\n' "${response}" >"${RUNS_JSON}"
    if DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh" >"${WORK_DIR}/incomplete-pages.log" 2>&1; then
      fail 'the scanner accepted missing or truncated run pages'
    fi
    check_plan '[]'
  done
  printf 'PASS: missing or truncated run pages cannot reset the budget\n'

  printf '[{"total_count":1,"workflow_runs":[{"id":123}]}]\n' >"${RUNS_JSON}"
  for response in '[]' '[{"total_count":1,"jobs":[]}]'; do
    printf '%s\n' "${response}" >"${JOBS_JSON}"
    if DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh" >"${WORK_DIR}/missing-jobs.log" 2>&1; then
      fail 'the scanner accepted missing or truncated job pages'
    fi
    check_plan '[]'
  done
  printf 'PASS: missing or truncated job pages cannot reset the budget\n'

  jq '.created_at = (now - 7200 | strftime("%Y-%m-%dT%H:%M:%SZ"))' "${RUN_JSON}" >"${RUN_JSON}.new"
  mv "${RUN_JSON}.new" "${RUN_JSON}"
  if DRY_RUN=false bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh" >"${WORK_DIR}/stale-run.log" 2>&1; then
    fail 'a stale scanner run requested new repairs'
  fi
  check_plan '[]'
  printf 'PASS: stale scanner runs cannot escape the history window\n'
}

main() {
  fixture
  bash "${SCRIPT_DIR}/merge-bundle-branch.sh" . "${BASE_SHA}"
  [[ "$(git rev-parse MERGE_HEAD)" == "${BASE_SHA}" ]] || fail 'the base is not a merge parent'
  [[ "$(git show :base.txt)" == 'base change' ]] || fail 'the merge lost a base change'
  cmp tool/data/otelc-bundle.tgz <(printf 'contribution\0bundle') || fail 'the merge changed the bundle'
  printf 'PASS: merge a bundle-only conflict without losing source changes\n'
  test_source_conflict
  test_authorization
  test_fork_publication
  test_empty_merge
  test_repository_and_direct_access
  test_publication_rejections
  test_scanner_limit
  test_scanner_selection
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
