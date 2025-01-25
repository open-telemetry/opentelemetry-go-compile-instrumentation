# Contributing to opentelemetry-go-compile-instrumentation

The go compile instrumentation SIG meets regularly. See the
OpenTelemetry
[community](https://github.com/open-telemetry/community?tab=readme-ov-file#implementation-sigs)
repo for information on this and other SIGs.

See the [public meeting
notes](https://docs.google.com/document/d/1XkVahJfhf482d3WVHsvUUDaGzHc8TO3sqQlSS80mpGY/edit)
for a summary description of past meetings. You can also get in touch on slack channel 
[#otel-go-compt-instr-sig](https://cloud-native.slack.com/archives/C088D8GSSSF)

## Development

TBD

## Pull Requests

### How to Send Pull Requests

Everyone is welcome to contribute code to `opentelemetry-go-compile-instrumentation` via
GitHub pull requests (PRs).

To create a new PR, fork the project in GitHub and clone the upstream
repo:

```sh
git clone https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation
```

This would put the project in the `opentelemetry-go-compile-instrumentation` directory in
current working directory.

Enter the newly created directory and add your fork as a new remote:

```sh
git remote add <YOUR_FORK> git@github.com:<YOUR_GITHUB_USERNAME>/opentelemetry-go-compile-instrumentation
```

Check out a new branch, make modifications, run linters and tests, update
`CHANGELOG.md`, and push the branch to your fork:

```sh
git checkout -b <YOUR_BRANCH_NAME>
# edit files
# update changelog
git add -p
git commit
git push <YOUR_FORK> <YOUR_BRANCH_NAME>
```

Open a pull request against the main `opentelemetry-go-compile-instrumentation` repo. Be sure to add the pull
request ID to the entry you added to `CHANGELOG.md`.

Avoid rebasing and force-pushing to your branch to facilitate reviewing the pull request.
Rewriting Git history makes it difficult to keep track of iterations during code review.
All pull requests are squashed to a single commit upon merge to `main`.

### How to Receive Comments

* If the PR is not ready for review, please put `[WIP]` in the title,
  tag it as `work-in-progress`, or mark it as
  [`draft`](https://github.blog/2019-02-14-introducing-draft-pull-requests/).
* Make sure CLA is signed and CI is clear.

### How to Get PRs Merged

A PR is considered **ready to merge** when:

* It has received two qualified approvals[^1].

  This is not enforced through automation, but needs to be validated by the
  maintainer merging.
  * The qualified approvals need to be from [Approver]s/[Maintainer]s
    affiliated with different companies. Two qualified approvals from
    [Approver]s or [Maintainer]s affiliated with the same company counts as a
    single qualified approval.
  * PRs introducing changes that have already been discussed and consensus
    reached only need one qualified approval. The discussion and resolution
    needs to be linked to the PR.
  * Trivial changes[^2] only need one qualified approval.

* All feedback has been addressed.
  * All PR comments and suggestions are resolved.
  * All GitHub Pull Request reviews with a status of "Request changes" have
    been addressed. Another review by the objecting reviewer with a different
    status can be submitted to clear the original review, or the review can be
    dismissed by a [Maintainer] when the issues from the original review have
    been addressed.
  * Any comments or reviews that cannot be resolved between the PR author and
    reviewers can be submitted to the community [Approver]s and [Maintainer]s
    during the weekly SIG meeting. If consensus is reached among the
    [Approver]s and [Maintainer]s during the SIG meeting the objections to the
    PR may be dismissed or resolved or the PR closed by a [Maintainer].
  * Any substantive changes to the PR require existing Approval reviews be
    cleared unless the approver explicitly states that their approval persists
    across changes. This includes changes resulting from other feedback.
    [Approver]s and [Maintainer]s can help in clearing reviews and they should
    be consulted if there are any questions.

* The PR branch is up to date with the base branch it is merging into.
  * To ensure this does not block the PR, it should be configured to allow
    maintainers to update it.

* It has been open for review for at least one working day. This gives people
  reasonable time to review.
  * Trivial changes[^2] do not have to wait for one day and may be merged with
    a single [Maintainer]'s approval.

* All required GitHub workflows have succeeded.
* Urgent fix can take exception as long as it has been actively communicated
  among [Maintainer]s.

Any [Maintainer] can merge the PR once the above criteria have been met.

[^1]: A qualified approval is a GitHub Pull Request review with "Approve"
  status from an OpenTelemetry Go Compile Instrumentation [Approver] or [Maintainer].
[^2]: Trivial changes include: typo corrections, cosmetic non-substantive
  changes, documentation corrections or updates, dependency updates, etc.


## Approvers and Maintainers

### Triagers

### Approvers

### Maintainers

- [Dinesh Gurumurthy](https://github.com/dineshg13), Datadog
- [Huxing Zhang](https://github.com/ralf0131), Alibaba
- [Przemyslaw Delewski](https://github.com/pdelewski), Quesma

### Emeritus


### Become an Approver or a Maintainer

See the [community membership document in OpenTelemetry community
repo](https://github.com/open-telemetry/community/blob/main/guides/contributor/membership.md).
