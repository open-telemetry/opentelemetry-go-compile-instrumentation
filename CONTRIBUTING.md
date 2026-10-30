# Contributing to opentelemetry-go-compile-instrumentation

The go compile instrumentation SIG meets regularly. See the
OpenTelemetry
[community](https://github.com/open-telemetry/community?tab=readme-ov-file#implementation-sigs)
repository for information on this and other special interest groups.

See the [public meeting
notes](https://docs.google.com/document/d/1XkVahJfhf482d3WVHsvUUDaGzHc8TO3sqQlSS80mpGY/edit)
for a summary description of past meetings. You can also get in touch on slack channel
[#otel-go-compile-instrumentation](https://cloud-native.slack.com/archives/C088D8GSSSF)

## Development

### Prerequisites

This project uses several tools for development. Most tools will be automatically installed when you first run the corresponding `make` target. However, you need to have:

- [Go](https://golang.org/dl/) 1.26 or later
- [Git](https://git-scm.com/)
- [GNU Make](https://www.gnu.org/software/make/) 3.82 or later. macOS has GNU Make
  3.81, which is too old: install a newer version with `brew install make`, then use
  `gmake` (or add `$(brew --prefix make)/libexec/gnubin` to your `PATH` to use it as
  `make`)

The tests of the bundle repair tool need only Git and Go.

### Getting Started

> **Windows/WSL contributors:** make sure `core.autocrlf` is `false` before
> cloning (`git config --global core.autocrlf false`). CI runs on Windows as
> well as Linux and macOS, and a clone with `core.autocrlf` enabled checks out
> source files with CRLF line endings. That changes how blank lines are
> preserved when the AST-based code generator writes output, which then fails
> to match the (LF) golden files under `testdata/golden`, even though the
> generated code is correct. If you already cloned with `autocrlf` on, re-clone
> after changing the setting rather than trying to fix the line endings of an
> existing checkout.

1. Clone the repository:

   ```sh
   git clone https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation
   cd opentelemetry-go-compile-instrumentation
   ```

2. Configure the git merge driver for the instrumentation bundle (run once per clone):

   ```sh
   make setup-git
   ```

   `tool/data/otelc-bundle.tgz` is a binary archive generated from `pkg/` and
   `instrumentation/`, so git cannot merge it and it conflicts on almost every
   rebase. This registers a merge driver that keeps the current bundle instead
   of stopping the rebase/merge; you then refresh it with `make package`. See
   [Keeping the bundle in sync](#keeping-the-bundle-in-sync).

3. Build the project:

   ```sh
   make build
   ```

4. Run tests:

   ```sh
   make test
   ```

### Available Make Targets

Run `make help` to see all available targets:

```sh
make help
```

#### Build Targets

- `make build` - Build the instrumentation tool (includes packaging)
- `make install` - Install the `otelc` binary to `$GOPATH/bin`
- `make package` - Package the instrumentation code into a binary archive
- `make build-demo-grpc` - Build gRPC demo server and client

#### Code Quality

- `make format` - Format all code (Go + YAML + License Headers)
  - `make format/go` - Format Go code only using golangci-lint
  - `make format/yaml` - Format YAML files only using `yamlfmt`
- `make lint` - Run all linters (Go, YAML, GitHub Actions, `Makefile`)
  - `make lint/go` - Run golangci-lint on Go code
  - `make lint/yaml` - Lint YAML formatting
  - `make lint/action` - Lint GitHub Actions workflows
  - `make lint/makefile` - Lint `Makefile`
  - `make lint/license-header` - Check license headers (has dedicated CI workflow)
  - `make lint/license-header/fix` - Apply license headers to Go and shell files

#### License Headers

All Go and shell files must include the proper license header. The required headers and path exclusions are managed in `.github/scripts/license-check.sh`.

To check and fix license headers:

- **Check license headers**: `make lint/license-header`
- **Apply license headers**: `make lint/license-header/fix`

The license header checker has a dedicated CI workflow (`check-license-headers.yaml`) that runs automatically on pull requests and pushes when Go files or the license configuration change.

#### Testing

- `make test` - Run all tests (unit + integration)
- `make test-unit` - Run unit tests only with formatted output
- `make test-integration` - Run integration tests only with formatted output
- `make test-e2e` - Run end-to-end tests
- `make test-bundle-workflows` - Test the bundle repair tool in `.tools/bundlerepair` and check the repair workflows
- `make test-latestlibbuild` - Build instrumented test apps against the `@latest` version of each instrumented library
- `make test-latestlibrun` - Bump instrumented test apps to `@latest` and run the full integration suite

Test results are saved to `gotest-unit.log` and `gotest-integration.log` for review.

#### Documentation

- `make docs` - Update embedded documentation in markdown files

#### Semantic Conventions

- `make weaver-install` - Install OTel Weaver if not present
- `make lint/semantic-conventions` - Validate semantic convention registry
- `make semantic-conventions/diff` - Generate diff between two versions of semantic convention registry
- `make semantic-conventions/resolve` - Resolve semantic convention registry schema

For detailed information on managing semantic conventions, see [docs/semantic-conventions.md](docs/semantic-conventions.md).

#### GitHub Actions Security

- `make ratchet/pin` - Pin GitHub Actions to specific commit hashes for security
- `make ratchet/update` - Update pinned GitHub Actions to latest versions
- `make ratchet/check` - Verify all GitHub Actions are properly pinned

#### Cleanup

- `make clean` - Remove all build artifacts and temporary files

### Development Workflow

A typical development workflow looks like:

1. Make your changes
2. Format your code: `make format`
3. Run linters: `make lint`
4. Run tests: `make test`
5. Commit your changes

For a complete check before submitting a pull request, run:

```sh
make all
```

This will run: `build`, `format`, `lint`, and `test` in sequence.

### Keeping the bundle in sync

`tool/data/otelc-bundle.tgz` is a reproducible archive of `pkg/` and
`instrumentation/` that is embedded into `otelc` via `//go:embed`. It must stay
committed so that `go install go.opentelemetry.io/otelc/tool/cmd/otelc@latest`
works, but because it is binary, git cannot 3-way merge it — so any branch that
touches the sources conflicts with `main` on this file.

To make this painless, run `make setup-git` once per clone (see
[Getting Started](#getting-started)). It registers a custom merge driver
(defined in `.gitattributes` + `.github/scripts/merge-bundle.sh`) that keeps the
current ("ours") bundle on conflict so the rebase/merge runs to completion
without stopping. You then regenerate the bundle from the fully-merged sources:

```sh
git fetch origin main
git merge origin/main    # no longer halts on the bundle
make package             # regenerate tool/data/otelc-bundle.tgz from merged sources
git add tool/data/otelc-bundle.tgz && git commit
git push
```

Why the driver doesn't regenerate the bundle for you: when git invokes a merge
driver it has not yet written the *other* merged source files to the working
tree, so running `make package` at that moment would embed stale sources and
miss the incoming changes. Regenerating after the rebase/merge completes is the
only reliable point, hence the explicit `make package` step above.

#### Automatic conflict resolution

The `Auto-fix Bundle Conflicts` workflow runs on pushes to `main`, hourly, and
on demand. It probes open, non-draft pull requests against `main` without
executing contributor code. It calls the shared `Regenerate Bundle` workflow
only when the bundle is the sole conflict. Forks must permit maintainer edits.
The repair workflow checks the caller workflow and its `main` ref.

The scanner permits ten automatic requests per repository per hour and one
request per pull request per hour. Failed requests count toward both limits.
The scanner counts its recorded selections, including requests whose repair
jobs never start. Missing or incomplete history stops selection. A missing
selection record can pause automation for the three-hour history window.

The scanner and its repair jobs run serially with other scans. The scanner
workflow starts at most two repairs in parallel, and the scanner enforces the
hourly budget and the cooldown. GitHub API quotas do not set these limits.

Automatic requests use the head and base commits that the scanner captured.
The workflow rechecks eligibility before generation and publication. Requests
expire one hour after GitHub creates the scanner run. Queue delays do not
extend this deadline. Start a new scanner run to retry automatic repairs.
Automatic reruns cannot start new repairs.

Users with the GitHub `maintain` or `admin` role can comment
`/regenerate-bundle` to request a repair, including during the automatic
cooldown. The `write` role alone does not authorize the command. The workflow
checks authorization before requests enter the shared queue. Requests for the
same pull request run serially.

Repair builds the archive with the Makefile and the bundle tool from `main`.
It copies only `pkg/` and `instrumentation/` from the pull request. If `main`
moves after a request is prepared, the generate job stops and the request must
be made again.

Verify Bundle runs only when `pkg/**`, `instrumentation/**`, or
`tool/data/otelc-bundle.tgz` change. A pull request that changes only the
bundle tool does not run it. In that case, CI does not catch a stale archive.
The author must run `make package` locally and commit the archive.

The repair workflows call one Go program, `bundlerepair`, in
`.tools/bundlerepair`. It uses only the Go standard library. Each job builds it
from the trusted checkout of `main`, with module downloads turned off. No job
runs code or build tooling from the pull request. The trusted build packages
the `pkg/` and `instrumentation/` sources. It resolves their module
dependencies through the public Go proxy. The program has these subcommands:

- `authorize` checks who may request a repair.
- `prepare` checks the pull request and captures its head and base commits.
- `select` chooses the conflicting pull requests for automatic repair.
  `select --budget` reports the requests of the last hour.
- `merge` merges the captured base commit into the pull request checkout.
- `stage` copies `pkg/` and `instrumentation/` from the pull request into the
  trusted checkout.
- `publish` commits and pushes the regenerated archive.
- `identity` checks the publisher token and reports its account.
- `credentials` chooses the App token or the maintainer token.

Run the tests of the program with `make test-bundle-workflows`. The target also
checks the structure of the repair workflows.

Generation runs without write credentials or a shared cache. A separate
publisher downloads the archive by its immutable artifact ID, merges the
captured base commit, and copies the archive. It does not execute contributor
scripts or Git hooks. It checks the current
base, head, branch, and fork permission before pushing a descendant commit.
An expected-head lease rejects concurrent branch updates.

Before enabling fork publication, an administrator must:

1. Create the `bundle-publisher` environment.
2. Restrict its deployment branch rules to the `main` branch only. For
   unattended repair, leave required reviewers unset.
3. Add `OTELC_BUNDLE_PUSH_TOKEN` as an environment secret. Do not create a
   repository or organization copy. Only the publisher uses this environment.

The callers forward the secret by name. The `bundle-publisher` environment
supplies the value. Only the publisher job reads the secret. A caller can pass
an environment secret by name even when the value lives only in that environment
([Reuse workflows](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows)).

Use an expiring classic token from a dedicated identity with upstream write
access. For this public repository, the token needs `public_repo` and
`workflow` scopes. Organization policy and SSO authorization must permit its
use. Do not put the token in a comment or a workflow file. The fork author
must allow maintainer edits unless the token identity has direct write access
to the fork.

Repository branches continue to use the GitHub App when
`OTELBOT_COMPILE_INSTRUMENTATION_CLIENT_ID` and
`OTELBOT_COMPILE_INSTRUMENTATION_PRIVATE_KEY` are configured. Otherwise, they
use the maintainer token. App and user-token pushes trigger normal pull request
checks. The workflow reports missing credentials and denied updates in its
logs and job summary. If publication fails, the generated artifact remains
available for one day.

To inspect automatic selection without starting repair jobs, run
`Auto-fix Bundle Conflicts` from the Actions tab with `dry_run` enabled.
The `verify-bundle` check still requires the committed archive to match the
sources.

### Tools

The following development tools are used and will be automatically installed on first use:

- **[golangci-lint](https://golangci-lint.run/)** - Go linter aggregator
- **[`gotestfmt`](https://github.com/gotesttools/gotestfmt)** - Prettier test output
- **[`yamlfmt`](https://github.com/google/yamlfmt)** - YAML formatter
- **[`actionlint`](https://github.com/rhysd/actionlint)** - GitHub Actions linter
- **[ratchet](https://github.com/sethvargo/ratchet)** - GitHub Actions security pinning
- **[`embedmd`](https://github.com/campoy/embedmd)** - Embed code in markdown files

### Architecture Decision Records

Significant architectural decisions are documented as Architecture Decision Records (ADRs) in [`docs/adr/`](docs/adr/). Read them to understand *why* the project is structured the way it is.

Create a new ADR when proposing:

- Changes to the instrumentation API or hook model
- New external dependencies or replacements
- Changes to the two-phase build process
- Any decision the SIG discusses and reaches consensus on

To create a new ADR:

```sh
make adr-new TITLE="Title of Your Decision"
```

This requires `adr-tools` (`npryce/adr-tools`). Run `make adr-tools` to install it. To list existing ADRs:

```sh
make adr-list
```

## AI Usage

This project welcomes the use of AI tools. Please read the [AI Usage Policy](docs/AI_POLICY.md) before
contributing. The critical rule is: **you must understand every line of code you submit.**
Contributors using AI tools are held to the same quality standards as any other contribution.

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
  │       ╰─ Optional: change scope (e.g, 'cmd/otelc', `pkg/weaver`, ...)
  ╰─ Required: commit type (see below for accepted values)
```

This repository requires using one of the following commit types:

- `chore` for routine repository maintenance that has no impact on the user interfaces (CI/CD
  operations, linter configuration, etc...)
- `doc` or `docs` for documentation changes
- `feat` for introduction of new features
- `fix` for bug fixes
- `release` when cutting a new release
- `refactor` for code changes that do not add new features or fix bugs
- `test` for changes to tests or test infrastructure

Please try to keep the commit title concise, yet specific: they are used to derive the release notes
for this repository. A good litmus test for whether a pull request title is suitable or not is to
determine whether a user would be able to determine whether this change affects them or not by just
looking at the title.

Here are some examples for the various supported commit types:

- `chore`:
  - :information_source: What's changing in this pull request? This should provide enough information from a
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
  - :white_check_mark: `feat(cmd/otelc): -log-level flag to configure log verbosity`
  - :x: `feat: logging`
- `fix`:
  - :information_source: What bug is being fixed? Refer to the symptoms of the fixed issue, not to
    the solution. A user might decide whether their problem is solved by a release or not based on
    this.
  - :white_check_mark: `fix: SEGFAULT on when cross-compiling on linux/arm64 platforms`
  - :x: `fix: check pointer for nil before dereferencing it`
- `release`:
  - :information_source: What version is this commit preparing for?
  - :white_check_mark: `release: v1.2.3`
  - :x: `release: new release`
- `refactor`:
  - :information_source: What code is being refactored?
  - :white_check_mark: `refactor: remove unused code`
  - :x: `refactor: improve code readability`
- `test`:
  - :information_source: What behavior or test coverage is being added?
  - :white_check_mark: `test: validate exported span output in basic integration test`
  - :x: `test: improve tests`

[conv-commit]: https://www.conventionalcommits.org/en/v1.0.0/

### How to Send Pull Requests

Everyone is welcome to contribute code to `opentelemetry-go-compile-instrumentation` via
GitHub pull requests.

To create a new pull request, fork the project in GitHub and clone the upstream
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

Check out a new branch, make modifications, run linters and tests, and push
the branch to your fork:

```sh
git checkout -b <YOUR_BRANCH_NAME>
# edit files
git add -p
git commit
git push <YOUR_FORK> <YOUR_BRANCH_NAME>
```

Open a pull request against the main `opentelemetry-go-compile-instrumentation` repo.

Avoid rebasing and force-pushing to your branch to facilitate reviewing the pull request.
Rewriting Git history makes it difficult to keep track of iterations during code review.
All pull requests are squashed to a single commit upon merge to `main`.

### How to Receive Comments

- If the pull request is not ready for review, please put `[WIP]` in the title,
  tag it as `work-in-progress`, or mark it as
  [`draft`](https://github.blog/2019-02-14-introducing-draft-pull-requests/).
- Make sure CLA is signed and CI is clear.

<a name="how-to-get-prs-merged"></a>

### How to Get Pull Requests Merged

A pull request is considered **ready to merge** when:

- It has received two qualified approvals[^1].

  This is not enforced through automation, but needs to be validated by the
  maintainer merging.
  - The qualified approvals need to be from [Approver]s/[Maintainer]s
    affiliated with different companies. Two qualified approvals from
    [Approver]s or [Maintainer]s affiliated with the same company counts as a
    single qualified approval.
  - Pull requests introducing changes that have already been discussed and consensus
    reached only need one qualified approval. The discussion and resolution
    needs to be linked to the pull request.
  - Trivial changes[^2] only need one qualified approval.

- All feedback has been addressed.
  - All pull request comments and suggestions are resolved.
  - All GitHub Pull Request reviews with a status of "Request changes" have
    been addressed. Another review by the objecting reviewer with a different
    status can be submitted to clear the original review, or the review can be
    dismissed by a [Maintainer] when the issues from the original review have
    been addressed.
  - Any comments or reviews that cannot be resolved between the pull request author and
    reviewers can be submitted to the community [Approver]s and [Maintainer]s
    during the weekly SIG meeting. If consensus is reached among the
    [Approver]s and [Maintainer]s during the SIG meeting the objections to the
    pull request may be dismissed or resolved or the pull request closed by a [Maintainer].
  - Any substantive changes to the pull request require existing Approval reviews be
    cleared unless the approver explicitly states that their approval persists
    across changes. This includes changes resulting from other feedback.
    [Approver]s and [Maintainer]s can help in clearing reviews and they should
    be consulted if there are any questions.

- The pull request branch is up to date with the base branch it is merging into.
  - To ensure this does not block the pull request, it should be configured to allow
    maintainers to update it.

- It has been open for review for at least one working day. This gives people
  reasonable time to review.
  - Trivial changes[^2] do not have to wait for one day and may be merged with
    a single [Maintainer]'s approval.

- All required GitHub workflows have succeeded.
- Urgent fix can take exception as long as it has been actively communicated
  among [Maintainer]s.

Any [Maintainer] can merge the pull request once the above criteria have been met.

[^1]: A qualified approval is a GitHub Pull Request review with "Approve"
  status from an OpenTelemetry Go Compile Instrumentation [Approver] or [Maintainer].
[^2]: Trivial changes include: typo corrections, cosmetic non-substantive
  changes, documentation corrections or updates, dependency updates, etc.

## Release Process

See [docs/RELEASE.md](docs/RELEASE.md) for the full release process, including release
cadence, tagging conventions, cross-compilation targets, and hotfix guidance.
