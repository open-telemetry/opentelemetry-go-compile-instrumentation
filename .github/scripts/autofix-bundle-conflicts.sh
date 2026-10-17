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
# Fork pull requests cannot be pushed to at all (the App is not installed on
# forks); for those we post a one-time (deduplicated) comment with manual
# instructions instead. Fork code is never checked out or executed here.
#
# The push is made with a GitHub App installation token rather than
# GITHUB_TOKEN, so the normal pull_request workflows are triggered on the new
# head and re-report the required status checks automatically.
#
# Required environment variables:
#   GH_TOKEN  - GitHub App installation token with contents:write and
#               pull-requests:write permissions
#   REPO      - owner/name of the repository to operate on
#
# Optional environment variables:
#   DRY_RUN   - set to "true" to log the planned actions without pushing or
#               commenting; useful for testing

: "${GH_TOKEN:?GH_TOKEN must be set}"
: "${REPO:?REPO must be set}"
: "${DRY_RUN:=false}"

BUNDLE="tool/data/otelc-bundle.tgz"
MARKER="<!-- otelc-bundle-conflict-help -->"

# Attribute the merge commits to the App's bot user (which EasyCLA knows about)
# instead of a hardcoded account.
bot_info="$(gh api user --jq '[.login, (.id | tostring)] | join(" ")')"
read -r bot_login bot_id <<<"${bot_info}"
[[ -n "${bot_login}" && -n "${bot_id}" ]] || {
  echo "cannot resolve the bot identity from the App token" >&2
  exit 1
}
git config user.name "${bot_login}"
git config user.email "${bot_id}+${bot_login}@users.noreply.github.com"

git config merge.otelc-bundle.name "Keep current otelc-bundle.tgz (regenerate with make package)"
git config merge.otelc-bundle.driver ".github/scripts/merge-bundle.sh %A"

# Pin the merge-driver policy in $GIT_DIR/info/attributes, which takes
# precedence over any .gitattributes checked out from the PR: only the bundle
# may use the custom driver, everything else keeps git's default merge
# behavior. Without this a same-repository PR could map its own source files to
# the driver and have real conflicts silently resolved in its favor.
git_dir="$(git rev-parse --git-dir)"
mkdir -p "${git_dir}/info"
printf '%s\n%s\n' '* !merge' 'tool/data/otelc-bundle.tgz merge=otelc-bundle' \
  >"${git_dir}/info/attributes"

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
    # Fork PR: never check out or execute fork code, and the App's token cannot
    # push to the fork. Post a one-time comment with manual instructions.
    # Capture the bodies into a variable instead of piping into grep: with
    # pipefail, grep exiting early on a match would kill the pipeline.
    comments="$(gh pr view "${num}" --repo "${REPO}" --json comments --jq '.comments[].body')"
    if [[ "${comments}" == *"${MARKER}"* ]]; then
      echo "help comment already present; not reposting"
    elif [[ "${DRY_RUN}" == "true" ]]; then
      echo "dry run: would post a one-time help comment"
    else
      echo "posting help comment"
      body="$(cat <<'EOF'
<!-- otelc-bundle-conflict-help -->
This pull request has merge conflicts with `main`, and the bot cannot fix fork
pull requests (its token is only valid on this repository), so please merge
`main` locally and resolve any source conflicts yourself. If the merge stops on
`tool/data/otelc-bundle.tgz` — a binary archive that is regenerated from the
sources — you can keep the local copy to let the merge proceed and regenerate
it afterwards:

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
  # Only skip when there is no merge in progress AND nothing is staged: with a
  # merge in progress the index may equal HEAD (e.g. the only changes were
  # already contained in the branch) yet the merge commit still needs to be
  # recorded so GitHub re-evaluates the conflict.
  if ! git rev-parse -q --verify MERGE_HEAD >/dev/null \
    && git diff --quiet --cached; then
    echo "nothing to merge and nothing staged; nothing to push"
    git merge --abort >/dev/null 2>&1 || true
    echo "::endgroup::"
    continue
  fi

  if [[ "${DRY_RUN}" == "true" ]]; then
    echo "dry run: would commit and push 'chore: merge main and regenerate ${BUNDLE}' to '${branch}'"
    git merge --abort >/dev/null 2>&1 || true
    git reset --hard --quiet
    git clean -ffdxq
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
    # The App token triggers the normal pull_request workflows on the new
    # head, so the required status checks re-report themselves.
    echo "pushed merge commit to '${branch}'"
  else
    echo "push rejected (branch moved meanwhile); skipping"
  fi
  echo "::endgroup::"
done <<<"${prs}"
