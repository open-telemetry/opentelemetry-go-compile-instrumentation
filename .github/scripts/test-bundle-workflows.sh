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
case "$*" in
  *'/git/ref/heads/'*) cat "${GH_BASE_RESPONSE}" ;;
  *) cat "${GH_RESPONSE}" ;;
esac
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
  AUTO_DEADLINE="$(jq -nr 'now + 300 | floor')"
  export AUTO_DEADLINE
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
  unset CALLER_WORKFLOW_REF RUN_ATTEMPT AUTO_DEADLINE
  printf 'PASS: automatic requests require the trusted caller and a new scan\n'
}

publication_fixture() {
  fixture
  EXPECTED_HEAD_SHA="$(git rev-parse HEAD)"
  export EXPECTED_HEAD_SHA BASE_SHA
  export REPO=test/upstream HEAD_REPO_FULL=test/fork HEAD_REF=contribution BASE_REF=main PR_NUMBER=1
  export BOT_LOGIN=test-bot BOT_ID=123 GH_TOKEN=test-token
  export GH_RESPONSE="${WORK_DIR}/pull.json" GH_BASE_RESPONSE="${WORK_DIR}/base.json"
  jq -n --arg base "${BASE_SHA}" '{ref:"refs/heads/main",object:{type:"commit",sha:$base}}' >"${GH_BASE_RESPONSE}"
  REMOTE="$(mktemp -d "${WORK_DIR}/remote.XXXXXX")/fork.git"
  git clone --quiet --bare . "${REMOTE}"
  git remote add origin "${REMOTE}"
  GENERATED_BUNDLE="$(mktemp "${WORK_DIR}/bundle.XXXXXX")"
  export GENERATED_BUNDLE
  printf 'regenerated\0bundle' >"${GENERATED_BUNDLE}"
  jq -n --arg head "${EXPECTED_HEAD_SHA}" --arg base "${BASE_SHA}" \
    '{state:"open", draft:false, maintainer_can_modify:true, head:{sha:$head, ref:"contribution", repo:{full_name:"test/fork"}}, base:{sha:$base, ref:"main"}}' >"${GH_RESPONSE}"
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
      base)
        jq '.object.sha = "0000000000000000000000000000000000000000"' "${GH_BASE_RESPONSE}" >"${GH_BASE_RESPONSE}.new"
        mv "${GH_BASE_RESPONSE}.new" "${GH_BASE_RESPONSE}"
        ;;
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
  scanner_history 1 1
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
    '[{jobs:([{name:"Select bundle repairs", completed_at:(now | strftime("%Y-%m-%dT%H:%M:%SZ")),
    steps:[{name:("Bundle repair selection: " + ({at:(now | floor),prs:[range($first;$last)]} | tojson)),conclusion:"success"}]}] +
    [range($first;$last) as $pr | ["Prepare bundle regeneration", "Generate bundle", "Push regenerated bundle"][] |
    {name:("Bundle repair #" + ($pr | tostring) + " / " + .), conclusion:"failure"}])}] |
    map(. + {total_count:(.jobs | length)})' >"${JOBS_JSON}"
}

check_plan() {
  local actual
  actual="$(grep '^prs=' "${GITHUB_OUTPUT}" | tail -n 1 | cut -d= -f2-)"
  jq -e --arg head "${EXPECTED_HEAD_SHA}" --arg base "${BASE_SHA}" '
    type == "array" and all(.[]; type == "object" and
      (.pr_number | type) == "number" and .pr_number > 0 and
      .head_sha == $head and .base_sha == $base and
      (.expires_at | type) == "number" and .expires_at > 0)' \
    <<<"${actual}" >/dev/null || fail 'the plan lost its captured snapshot'
  actual="$(jq -c 'map(.pr_number)' <<<"${actual}")"
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

  scanner_history 1 1
  DRY_RUN=true bash "${SCRIPT_DIR}/autofix-bundle-conflicts.sh"
  check_plan '[]'
  printf 'PASS: dry run probes forks without requesting repairs\n'

  scanner_history 1 11
  jq '.[0].jobs[0] |= (.completed_at = (now - 7200 | strftime("%Y-%m-%dT%H:%M:%SZ")) |
    .steps[0].name |= (ltrimstr("Bundle repair selection: ") | fromjson | .at -= 7200 |
      "Bundle repair selection: " + tojson))' "${JOBS_JSON}" >"${JOBS_JSON}.new"
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

stage_fixture() {
  BRANCH_DIR="$(mktemp -d "${WORK_DIR}/branch.XXXXXX")"
  TRUSTED_DIR="$(mktemp -d "${WORK_DIR}/trusted.XXXXXX")"
  local root
  for root in "${BRANCH_DIR}" "${TRUSTED_DIR}"; do
    mkdir -p "${root}/pkg/sub" "${root}/instrumentation/lib"
    printf 'package pkg\n' >"${root}/pkg/sub/kept.go"
    printf 'package lib\n' >"${root}/instrumentation/lib/kept.go"
    printf 'trusted\n' >"${root}/Makefile"
  done
  # The trusted checkout starts with files that the branch deleted or edited.
  printf 'stale\n' >"${TRUSTED_DIR}/pkg/stale.go"
  printf 'stale\n' >"${TRUSTED_DIR}/instrumentation/lib/stale.go"
  printf 'old\n' >"${TRUSTED_DIR}/pkg/sub/kept.go"
}

stage() {
  local status=0
  bash "${SCRIPT_DIR}/stage-bundle-sources.sh" "${BRANCH_DIR}" "${TRUSTED_DIR}" >"${WORK_DIR}/stage.log" 2>&1 || status=$?
  return "${status}"
}

test_stage_sync() {
  stage_fixture
  # Add, edit, and delete files under both trees.
  printf 'package pkg\nvar added int\n' >"${BRANCH_DIR}/pkg/sub/added.go"
  printf 'package pkg\nvar edited int\n' >"${BRANCH_DIR}/pkg/sub/kept.go"
  mkdir -p "${BRANCH_DIR}/instrumentation/new/deep"
  printf 'package deep\n' >"${BRANCH_DIR}/instrumentation/new/deep/added.go"
  printf 'hidden\n' >"${BRANCH_DIR}/instrumentation/lib/.hidden"
  printf 'data\n' >"${TRUSTED_DIR}/pkg/gone.txt"
  # The branch controls these files, but staging must not copy them.
  printf 'hostile\n' >"${BRANCH_DIR}/Makefile"
  mkdir -p "${BRANCH_DIR}/.tools/bundle"
  printf 'hostile\n' >"${BRANCH_DIR}/.tools/bundle/main.go"
  printf 'hostile\n' >"${BRANCH_DIR}/go.mod"
  stage || fail "staging failed: $(cat "${WORK_DIR}/stage.log")"
  local tree
  for tree in pkg instrumentation; do
    diff -r "${BRANCH_DIR}/${tree}" "${TRUSTED_DIR}/${tree}" || fail "the trusted ${tree} tree differs from the branch"
  done
  [[ "$(cat "${TRUSTED_DIR}/Makefile")" == trusted ]] || fail 'staging copied a hostile Makefile'
  [[ ! -e "${TRUSTED_DIR}/.tools" && ! -e "${TRUSTED_DIR}/go.mod" ]] || fail 'staging copied files outside the bundled trees'
  [[ ! -e "${TRUSTED_DIR}/pkg/stale.go" ]] || fail 'staging kept a stale file'
  printf 'PASS: stage the bundled trees and nothing else\n'
}

test_stage_rejections() {
  local tree scenario
  for tree in pkg instrumentation; do
    for scenario in file-link dir-link fifo root-link missing root-file other-tree-link; do
      stage_fixture
      case "${scenario}" in
        file-link) ln -s /etc/passwd "${BRANCH_DIR}/${tree}/link.go" ;;
        dir-link) ln -s /tmp "${BRANCH_DIR}/${tree}/dir-link" ;;
        fifo) mkfifo "${BRANCH_DIR}/${tree}/pipe" ;;
        root-link)
          mkdir "${BRANCH_DIR}/elsewhere"
          rm -rf "${BRANCH_DIR:?}/${tree}"
          ln -s "${BRANCH_DIR}/elsewhere" "${BRANCH_DIR}/${tree}"
          ;;
        missing) rm -rf "${BRANCH_DIR:?}/${tree}" ;;
        root-file)
          rm -rf "${BRANCH_DIR:?}/${tree}"
          printf 'file\n' >"${BRANCH_DIR}/${tree}"
          ;;
        other-tree-link)
          # A bad entry in the other tree must also stop the copy of this one.
          if [[ "${tree}" == pkg ]]; then
            ln -s /etc/passwd "${BRANCH_DIR}/instrumentation/lib/link.go"
          else
            ln -s /etc/passwd "${BRANCH_DIR}/pkg/sub/link.go"
          fi
          printf 'changed\n' >"${BRANCH_DIR}/${tree}/changed.go"
          ;;
      esac
      cp -R "${TRUSTED_DIR}" "${WORK_DIR}/trusted.before"
      local status=0
      stage || status=$?
      [[ "${status}" == 1 ]] || fail "staging exited ${status} for ${scenario} in ${tree}: $(cat "${WORK_DIR}/stage.log")"
      diff -r "${WORK_DIR}/trusted.before" "${TRUSTED_DIR}" || fail "staging changed the trusted tree for ${scenario} in ${tree}"
      rm -rf "${WORK_DIR}/trusted.before"
    done
  done
  printf 'PASS: reject unsafe bundled trees before changing the trusted checkout\n'
}

test_stage_arguments() {
  local status=0
  bash "${SCRIPT_DIR}/stage-bundle-sources.sh" --help >/dev/null || fail 'staging rejected --help'
  bash "${SCRIPT_DIR}/stage-bundle-sources.sh" >/dev/null 2>&1 || status=$?
  [[ "${status}" == 1 ]] || fail "staging exited ${status} without arguments"
  status=0
  bash "${SCRIPT_DIR}/stage-bundle-sources.sh" "${WORK_DIR}/missing-a" "${WORK_DIR}/missing-b" >/dev/null 2>&1 || status=$?
  [[ "${status}" == 1 ]] || fail "staging exited ${status} for missing directories"
  printf 'PASS: staging validates its arguments\n'
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
  test_stage_sync
  test_stage_rejections
  test_stage_arguments
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  main "$@"
fi
