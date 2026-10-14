#!/bin/bash

# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0

set -EeufCo pipefail

# Auto-resolve pull request merge conflicts that are limited to
# tool/data/otelc-bundle.tgz.
#
# The bundle is a binary archive regenerated from pkg/ and instrumentation/ on
# almost every change, and git cannot 3-way merge binary files, so open pull
# requests frequently hit "This branch has conflicts that must be resolved" on
# it. For each conflicted pull request we merge origin/main locally: the custom
# 'otelc-bundle' merge driver (.gitattributes + .github/scripts/merge-bundle.sh)
# auto-resolves the bundle, and any other conflict still fails the merge. On
# success we regenerate the bundle from the merged sources (make package) and
# push a single commit back to the branch, which clears GitHub's server-side
# conflict indicator.
#
# Fork pull requests cannot be pushed to with GITHUB_TOKEN; for those we post a
# one-time (deduplicated) comment with manual instructions instead. Fork code is
# never checked out or executed here.
#
# Pushes made with GITHUB_TOKEN do not trigger other workflows, so after a
# successful push we re-dispatch the repository's required workflows ourselves
# (see REQUIRED_WORKFLOWS) to report the required status checks on the new head.
#
# Required environment variables:
#   GH_TOKEN  - GitHub token with contents:write, pull-requests:write and
#               actions:write permissions
#   REPO      - owner/name of the repository to operate on

: "${GH_TOKEN:?GH_TOKEN must be set}"
: "${REPO:?REPO must be set}"

BUNDLE="tool/data/otelc-bundle.tgz"
MARKER="<!-- otelc-bundle-conflict-help -->"

# Required-workflow files to re-dispatch after the bot pushes, because
# GITHUB_TOKEN pushes do not trigger them. Keep this list in sync with the
# repository's required workflows.
REQUIRED_WORKFLOWS=(
  verify-bundle.yaml
  check-consistency.yaml
  check-license-headers.yaml
  lint-golang.yaml
  test-unit.yaml
  test-integration.yaml
  test-e2e.yaml
  test-latestlibbuild.yaml
)

git config merge.otelc-bundle.name "Keep current otelc-bundle.tgz (regenerate with make package)"
git config merge.otelc-bundle.driver ".github/scripts/merge-bundle.sh %A"
git config user.name "github-actions[bot]"
git config user.email "github-actions[bot]@users.noreply.github.com"

git fetch --force origin main

prs="$(gh pr list --repo "${REPO}" --state open --limit 200 \
  --json number,isDraft,mergeable \
  --jq '.[] | select(.isDraft == false and .mergeable == "CONFLICTING") | .number')"

if [[ -z "${prs}" ]]; then
  echo "No pull requests are conflicted on ${BUNDLE}; nothing to do."
  exit 0
fi

while IFS= read -r num; do
  echo "::group::PR #${num}"

  meta="$(gh pr view "${num}" --repo "${REPO}" \
    --json state,mergeable,isCrossRepository,headRefName)"
  state="$(jq -r .state <<<"${meta}")"
  mergeable="$(jq -r .mergeable <<<"${meta}")"
  cross="$(jq -r .isCrossRepository <<<"${meta}")"
  branch="$(jq -r .headRefName <<<"${meta}")"

  if [[ "${state}" != "OPEN" || "${mergeable}" != "CONFLICTING" ]]; then
    echo "state=${state} mergeable=${mergeable}; skipping"
    echo "::endgroup::"
    continue
  fi

  if [[ "${cross}" == "true" ]]; then
    # Fork PR: never check out or execute fork code, and GITHUB_TOKEN cannot
    # push to the fork. Post a one-time comment with manual instructions.
    if gh pr view "${num}" --repo "${REPO}" --json comments --jq '.comments[].body' \
      | grep -Fq "${MARKER}"; then
      echo "help comment already present; not reposting"
    else
      echo "posting help comment"
      body="$(cat <<'EOF'
<!-- otelc-bundle-conflict-help -->
This pull request conflicts with `main` on `tool/data/otelc-bundle.tgz`, a binary
archive that is regenerated from the sources. The bot cannot fix fork pull
requests (GITHUB_TOKEN cannot push to your fork), so please merge main locally:

```sh
git fetch origin main
git merge origin/main   # run `make setup-git` once so the bundle does not block this
make package            # regenerate tool/data/otelc-bundle.tgz from the merged sources
git add tool/data/otelc-bundle.tgz && git commit
git push
```

See [Keeping the bundle in sync](https://github.com/__REPO__/blob/main/CONTRIBUTING.md#keeping-the-bundle-in-sync).
EOF
)"
      gh pr comment "${num}" --repo "${REPO}" --body "${body//__REPO__/${REPO}}"
    fi
    echo "::endgroup::"
    continue
  fi

  # Same-repository branch. Reset the shared working tree between iterations;
  # this checkout is a throwaway CI workspace, not a developer machine.
  git merge --abort >/dev/null 2>&1 || true
  git reset --hard --quiet
  git clean -ffdxq

  if ! git fetch --force origin "refs/heads/${branch}:refs/remotes/origin/${branch}"; then
    echo "cannot fetch branch '${branch}' (deleted?); skipping"
    echo "::endgroup::"
    continue
  fi
  git checkout --detach --force "refs/remotes/origin/${branch}"

  # Re-check just before acting: the listing can be stale.
  state="$(gh pr view "${num}" --repo "${REPO}" --json state --jq .state)"
  if [[ "${state}" != "OPEN" ]]; then
    echo "PR is now ${state}; skipping"
    echo "::endgroup::"
    continue
  fi

  # The custom driver auto-resolves only the bundle; any other conflict makes
  # the merge fail, meaning this PR has a real conflict that must stay with the
  # author.
  if ! git merge --no-ff --no-commit origin/main >/dev/null; then
    echo "conflicts beyond ${BUNDLE}; skipping"
    git merge --abort >/dev/null 2>&1 || true
    echo "::endgroup::"
    continue
  fi

  if ! make package; then
    echo "'make package' failed on the merged sources (semantic conflict); skipping"
    git merge --abort >/dev/null 2>&1 || true
    git reset --hard --quiet
    git clean -ffdxq
    echo "::endgroup::"
    continue
  fi

  git add -- "${BUNDLE}"
  if git diff --quiet --cached; then
    echo "merge produced no changes; nothing to push"
    git merge --abort >/dev/null 2>&1 || true
    echo "::endgroup::"
    continue
  fi
  git commit -m "chore: merge main and regenerate ${BUNDLE}"

  # Re-check right before pushing so we never resurrect a branch whose PR was
  # merged (and possibly auto-deleted) while we were working.
  state="$(gh pr view "${num}" --repo "${REPO}" --json state --jq .state)"
  if [[ "${state}" != "OPEN" ]]; then
    echo "PR is now ${state}; not pushing"
    echo "::endgroup::"
    continue
  fi

  if git push origin "HEAD:refs/heads/${branch}"; then
    echo "pushed merge commit to '${branch}'"
    for wf in "${REQUIRED_WORKFLOWS[@]}"; do
      if ! gh workflow run "${wf}" --repo "${REPO}" --ref "${branch}"; then
        echo "::warning::failed to dispatch ${wf} on '${branch}'"
      fi
    done
  else
    echo "push rejected (branch moved meanwhile); skipping"
  fi
  echo "::endgroup::"
done <<<"${prs}"
