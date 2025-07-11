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

### Conventional Commits

Pull requests made to this repository are expected to use the [Conventional Commits][conv-commit]
specification. Specifically, pull request titles are required to follow the specification's title
format:

```
<type>(<scope>)!: <description>
╰─┬──╯╰───┬───╯│  ╰─────┬─────╯
  │       │    │        ╰─ Short description of the change (see below)
  │       │    ╰─ If, and only if the PR contains breaking changes
  │       ╰─ Optional: change scope (e.g, 'cmd/gotel', `pkg/weaver`, ...)
  ╰─ Required: commit type (see below for accepted values)
```

This repository requires using one of the following commit types:

- `chore` for routine repository maintenance that has no impact on the user interfaces (CI/CD
  operations, linter configuration, etc...)
- `doc` or `docs` for documentation changes
- `feat` for introduction of new features
- `fix` for bug fixes
- `release` when cutting a new release

Please try to keep the commit title concise, yet specific: they are used to derive the release notes
for this repository. A good litmus test for whether a pull request title is suitable or not is to
determine whether a user would be able to determine whether this change affects them or not by just
looking at the title.

Here are some examples for the various supported commit types:

- `chore`:
  - :information_source: What's changing in this PR? This should provide enough information from a
    maintainer to make sense of what's going on.
  - :white_check_mark: `chore(ci): add OSSF Scorecard automation`
  - :x: `chore: new CI step`
- `doc`, `docs`:
  - :information_source: What documentation has change? A user might decide whether they go read it
    or not based on this.
  - :white_check_mark: `docs: explain proper use of the -log-level flag`
  - :x: `docs: improve documentation`
- `feat`:
  - :information_source:  What feature is being introduced specifically? A user might decide if this
    is useful to them or not based on this.
  - :white_check_mark: `feat(cmd/gotel): -log-level flag to configure log verbosity`
  - :x: `feat: logging`
- `fix`:
  - :information_source: What bug is being fixed? Refer to the symptoms of the fixed issue, not to
    the solution. A user might decide whether their problem is solved by a release or not based on
    this.
  - :white_check_mark: `fix: SEGFAULT on when cross-compiling on linux/arm64 platforms`
  - :x: `fix: check pointer for nil before dereferencing it`
- `release:
  - :information_source: What version is this commit preparing for?
  - :white_check_mark: `release: v1.2.3`
  - :x: `release: new release`

[conv-commit]: https://www.conventionalcommits.org/en/v1.0.0/

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

- If the PR is not ready for review, please put `[WIP]` in the title,
  tag it as `work-in-progress`, or mark it as
  [`draft`](https://github.blog/2019-02-14-introducing-draft-pull-requests/).
- Make sure CLA is signed and CI is clear.

### How to Get PRs Merged

A PR is considered **ready to merge** when:

- It has received two qualified approvals[^1].

  This is not enforced through automation, but needs to be validated by the
  maintainer merging.
  - The qualified approvals need to be from [Approver]s/[Maintainer]s
    affiliated with different companies. Two qualified approvals from
    [Approver]s or [Maintainer]s affiliated with the same company counts as a
    single qualified approval.
  - PRs introducing changes that have already been discussed and consensus
    reached only need one qualified approval. The discussion and resolution
    needs to be linked to the PR.
  - Trivial changes[^2] only need one qualified approval.

- All feedback has been addressed.
  - All PR comments and suggestions are resolved.
  - All GitHub Pull Request reviews with a status of "Request changes" have
    been addressed. Another review by the objecting reviewer with a different
    status can be submitted to clear the original review, or the review can be
    dismissed by a [Maintainer] when the issues from the original review have
    been addressed.
  - Any comments or reviews that cannot be resolved between the PR author and
    reviewers can be submitted to the community [Approver]s and [Maintainer]s
    during the weekly SIG meeting. If consensus is reached among the
    [Approver]s and [Maintainer]s during the SIG meeting the objections to the
    PR may be dismissed or resolved or the PR closed by a [Maintainer].
  - Any substantive changes to the PR require existing Approval reviews be
    cleared unless the approver explicitly states that their approval persists
    across changes. This includes changes resulting from other feedback.
    [Approver]s and [Maintainer]s can help in clearing reviews and they should
    be consulted if there are any questions.

- The PR branch is up to date with the base branch it is merging into.
  - To ensure this does not block the PR, it should be configured to allow
    maintainers to update it.

- It has been open for review for at least one working day. This gives people
  reasonable time to review.
  - Trivial changes[^2] do not have to wait for one day and may be merged with
    a single [Maintainer]'s approval.

- All required GitHub workflows have succeeded.
- Urgent fix can take exception as long as it has been actively communicated
  among [Maintainer]s.

Any [Maintainer] can merge the PR once the above criteria have been met.

[^1]: A qualified approval is a GitHub Pull Request review with "Approve"
  status from an OpenTelemetry Go Compile Instrumentation [Approver] or [Maintainer].
[^2]: Trivial changes include: typo corrections, cosmetic non-substantive
  changes, documentation corrections or updates, dependency updates, etc.

## Approvers and Maintainers

### Maintainers

- [Huxing Zhang](https://github.com/ralf0131), Alibaba
- [Kemal Akkoyun](https://github.com/kakkoyun), Datadog
- [Liu Ziming](https://github.com/123liuziming), Alibaba
- [Przemyslaw Delewski](https://github.com/pdelewski), Quesma
- [Romain Marcadier](https://github.com/RomainMuller), Datadog

For more information about the maintainer role, see the [community repository](https://github.com/open-telemetry/community/blob/main/guides/contributor/membership.md#maintainer).

### Approvers

- [Dario Castañe](https://github.com/darccio), Datadog
- [Eliott Bouhana](https://github.com/eliottness), Datadog
- [Haibin Zhang](https://github.com/NameHaibinZhang), Alibaba
- [Yin Yang](https://github.com/y1yang0), Alibaba

For more information about the approver role, see the [community repository](https://github.com/open-telemetry/community/blob/main/guides/contributor/membership.md#approver).

### Emeritus maintainers

- [Dinesh Gurumurthy](https://github.com/dineshg13)

For more information about the emeritus role, see the [community repository](https://github.com/open-telemetry/community/blob/main/guides/contributor/membership.md#emeritus-maintainerapprovertriager).
