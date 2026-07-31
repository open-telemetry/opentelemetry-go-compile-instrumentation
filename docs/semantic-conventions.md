# Semantic Conventions Management

This document describes the tooling and workflow for managing [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/concepts/semantic-conventions/) in the compile-instrumentation project.

## Overview

Semantic conventions define a common set of attribute names and values used across OpenTelemetry projects to ensure consistency and interoperability. This project uses [OTel Weaver](https://github.com/open-telemetry/weaver) to:

1. **Validate the project's own telemetry contract** — a local Weaver registry under [`schemas/otelc/`](../schemas/otelc/) declares exactly which metrics, spans, and attributes each instrumentation emits, and CI validates it against the pinned upstream semantic conventions. This is the primary integration and is described in [Local Registry](#local-registry-schemasotelc) below.
2. **Track upstream changes** — the `.semconv-version` file pins the upstream version the project abides by, and helper targets report what's new upstream.

Weaver runs from an OCI image (`otel/weaver`) via Docker/Podman for the registry validation, so no host install is required for `make lint-schema`.

## Version Management

The project's semantic conventions version is tracked in the `.semconv-version` file at the root of the repository. This file:

- Specifies which semantic conventions version the project intends to abide by
- Must match the `semconv` imports used in `instrumentation/**/semconv/` Go code
- Is validated by CI to ensure consistency

**Example `.semconv-version` file**:

```
v1.30.0
```

When updating to a new semantic conventions version:

1. Update the version in `.semconv-version`
2. Update the upstream dependency in `schemas/otelc/registry_manifest.yaml` (the `.deps/upstream-vX.Y.Z[model]` path) to match
3. Update Go imports in `instrumentation/**/semconv/` to match
4. Run `make lint-schema` to validate
5. Update code and registry groups to handle any breaking changes

> The `.semconv-version` file, the `registry_manifest.yaml` dependency, and the Go `semconv/vX.Y.Z` imports must all agree. CI enforces this consistency.

## Local Registry (`schemas/otelc/`)

The project maintains its own Weaver schema registry under [`schemas/otelc/`](../schemas/otelc/) that is the machine-readable contract of the telemetry otelc's instrumentations emit:

```
schemas/otelc/
├── registry_manifest.yaml   # registry metadata + pinned upstream semconv dependency
├── groups/                  # one file per instrumentation (metrics, spans, attributes)
│   ├── http.yaml            # net/http client & server metrics
│   ├── grpc.yaml            # google.golang.org/grpc client & server metrics + spans
│   ├── database-sql.yaml    # database/sql client spans
│   ├── redis.yaml           # redis/go-redis (v9) client spans
│   ├── kafka.yaml           # segmentio/kafka-go producer & consumer spans
│   ├── k8s.yaml             # k8s.io/client-go informer spans
│   ├── openai.yaml          # openai/openai-go GenAI client spans
│   ├── anthropic.yaml       # anthropics/anthropic-sdk-go GenAI client spans
│   ├── mongo.yaml           # go.mongodb.org/mongo-driver client spans
│   ├── gin.yaml             # gin-gonic/gin server-span enrichment
│   ├── otel-sdk.yaml        # go.opentelemetry.io/otel* — Go runtime metrics
│   ├── logs.yaml            # log, log/slog, logrus — no telemetry (correlation only)
│   └── runtime.yaml         # runtime — no telemetry (GLS context propagation)
└── .deps/                   # pre-fetched upstream semconv (git-ignored, generated)
```

Every instrumentation module in `instrumentation/` maps to exactly one file here — see the [coverage table](../schemas/otelc/README.md#instrumentation-coverage). Instrumentations that emit no telemetry of their own still get a file, with `groups: []` and a comment explaining why.

- `registry_manifest.yaml` declares the registry name and a **dependency** on the upstream OpenTelemetry semantic conventions, pre-fetched locally under `.deps/` so weaver doesn't clone it over the network on every run.
- Each `groups/*.yaml` file declares the metrics/spans/attributes one instrumentation produces. Telemetry that exists **upstream** is referenced with `ref:`; telemetry that is **specific to a library** (not covered upstream) is declared locally with `id:`.

### Adding telemetry for a new instrumentation

Use `groups/http.yaml` as the template:

1. Create `schemas/otelc/groups/<library>.yaml`.
2. For each metric your instrumentation records (see its `instrumentation/**/semconv/*.go`), add a `type: metric` group with `metric_name`, `instrument`, `unit`, `stability`, and its attribute set.
3. For each span your instrumentation creates, add a `type: span` group with `span_kind`, `stability`, `brief`, and its attribute set (see `groups/grpc.yaml`, `groups/database-sql.yaml`). List the union of all attributes the span may carry, including those set only conditionally.
4. Reference upstream attributes with `- ref: <attribute.id>`. For attributes/metrics not defined upstream, declare them locally with `id:` (include `type`, `stability`, `brief`, and `examples` for string attributes — `--future` treats a missing example as an error). Group local attribute definitions in a `type: attribute_group` so several groups can `ref:` them (see `groups/k8s.yaml`, `groups/openai.yaml`).
5. Run `make lint-schema` and fix any diagnostics.

> **Note on re-declaring upstream metrics.** Most instrumentations emit standard upstream metrics (e.g. `http.client.request.duration`). Weaver has no way to *reference* a metric defined in a dependency, so we re-declare it locally to pin the emission contract. Weaver then reports a `DuplicateMetricName` between our registry and the upstream dependency; this specific, expected duplicate is allowlisted in [`scripts/semconv/lint-schema-filter.jq`](../scripts/semconv/lint-schema-filter.jq). Any *other* diagnostic — including a metric declared twice within our own registry, or an unresolved `ref:` — still fails the lint.

## Prerequisites

Validating the local registry (`make lint-schema`) requires:

- **Docker** (or Podman — set `OCI_BIN=podman`). Weaver runs from the `otel/weaver` OCI image, so no host install is needed. Override the image/version with `WEAVER_IMAGE=otel/weaver:vX.Y.Z`.
- **`jq`** — used to filter weaver's diagnostics. Install via `brew install jq` / `apt-get install jq`.

The upstream-tracking targets (`make semantic-conventions/diff`, `make semantic-conventions/resolve`) instead use a locally installed weaver binary:

```bash
make weaver-install
```

This installs the weaver CLI tool to `$GOPATH/bin`. Ensure your `$GOPATH/bin` is in your `PATH`.

## Available Targets

### Validate the Local Registry

Validate the project's own telemetry contract in `schemas/otelc/`:

```bash
make lint-schema           # or the umbrella alias: make lint/semantic-conventions
```

This command:

- Pre-fetches the pinned upstream semconv into `schemas/otelc/.deps/` (via `make fetch-upstream-semconv`)
- Asserts the manifest's upstream dependency matches `.semconv-version`
- Runs `weaver registry check --future` (from the `otel/weaver` OCI image) against `schemas/otelc/`
- Filters weaver's diagnostics through `scripts/semconv/lint-schema-filter.jq`, failing on any diagnostic that is not the expected upstream-metric duplicate
- **This check is blocking** — violations will fail CI

**When to use**: Run this before committing changes to `schemas/otelc/**`, `instrumentation/**/semconv/`, or `.semconv-version`.

### Generate Registry Diff

Compare the current version against the latest to see available updates:

```bash
make registry-diff
```

This command automatically:

1. **Reads** the version from `.semconv-version` (e.g., `v1.30.0`)
2. **Generates a comparison report**: Latest (main branch) vs Current version
3. Shows what new features and changes are available

**Output file**: `tmp/registry-diff-latest.md`

**Example output**:

```
Current project version: v1.30.0
Comparing against latest (main branch)...

Available updates (latest vs v1.30.0):
- Added: db.client.connection.state
- Deprecated: net.peer.name (use server.address)
- Modified: http.response.status_code description
...
```

**When to use**:

- Understanding what's in your current semconv version
- Deciding whether to upgrade to a newer version
- Reviewing changes before modifying `instrumentation/**/semconv/`

**Requirements**:

- Network access to GitHub
- OTel Weaver installed (run `make weaver-install` first)

### Resolve Registry Schema

Generate a resolved, flattened view of the semantic convention registry for your current version:

```bash
make semantic-conventions/resolve
```

This command:

- Fetches the semantic convention registry at the **latest** version (main branch)
- Resolves all references and inheritance
- Outputs a single YAML file with all definitions
- Saves the output to `tmp/resolved-schema.yaml`

**To resolve a specific version** (e.g., the version you're using):

```bash
# Manually resolve for v1.30.0
weaver registry resolve \
  --registry https://github.com/open-telemetry/semantic-conventions.git[model]@v1.30.0 \
  --format yaml \
  --output tmp/resolved-v1.30.0.yaml \
  --future
```

**When to use**:

- Inspecting the complete schema structure
- Searching for specific attribute definitions
- Debugging attribute inheritance or references
- Understanding available attributes before implementing new features

## Workflow: Adding a New Attribute

When adding new semantic convention attributes to this project, follow this workflow:

### 1. Check Upstream Semantic Conventions

Before defining a new attribute, check if it already exists in the [OpenTelemetry Semantic Conventions](https://github.com/open-telemetry/semantic-conventions):

```bash
make semantic-conventions/resolve
# Search the resolved schema for your attribute
grep "your.attribute.name" tmp/resolved-schema.yaml
```

### 2. Define the Attribute

If the attribute doesn't exist upstream (or you need a project-specific attribute):

1. Add your attribute definition to the appropriate file in `instrumentation/**/semconv/`
2. Follow the [OpenTelemetry attribute naming conventions](https://opentelemetry.io/docs/specs/semconv/general/attribute-naming/)
3. Include proper documentation and examples

Example structure:

```go
// instrumentation/net/http/semconv/client.go
package semconv

const (
    // HTTPRequestMethod represents the HTTP request method.
    // Type: string
    // Examples: "GET", "POST", "DELETE"
    HTTPRequestMethod = "http.request.method"

    // HTTPResponseStatusCode represents the HTTP response status code.
    // Type: int
    // Examples: 200, 404, 500
    HTTPResponseStatusCode = "http.response.status_code"
)
```

### 3. Validate Your Changes

Run the validation tool to ensure your definitions are correct:

```bash
make lint/semantic-conventions
```

Fix any errors or warnings reported by the validator.

### 4. Generate a Diff Report

Generate a diff report to document your changes:

```bash
make registry-diff
```

Review the diff to ensure only expected changes are present.

### 5. Run Tests

Ensure your changes don't break existing functionality:

```bash
make test
```

### 6. Submit for Review

When submitting a PR with semantic convention changes:

1. The CI will automatically run `lint/semantic-conventions`
2. A registry diff report will be generated and posted as a PR comment
3. Review the diff report carefully to ensure all changes are intentional
4. Address any CI failures before merging

## Schema Definition Location

Semantic convention definitions in this project are located in:

```
instrumentation/
├── net/
│   └── http/
│       └── semconv/        # HTTP semantic conventions
│           ├── client.go
│           ├── server.go
│           ├── util.go
│           └── ...
├── google.golang.org/
│   └── grpc/
│       └── semconv/        # gRPC semantic conventions
│           ├── grpc.go
│           ├── util.go
│           └── ...
└── .../
```

These definitions extend or implement the official [OpenTelemetry Semantic Conventions](https://github.com/open-telemetry/semantic-conventions) for use in compile-time instrumentation.

## Continuous Integration

The project includes automated checks for semantic conventions:

### On Pull Requests

When you modify files in `schemas/**`, `scripts/semconv/**`, `instrumentation/**/semconv/`, or `.semconv-version`:

#### Job 1: Validate Semantic Conventions (Blocking)

This job ensures the registry and code stay consistent with the pinned version:

1. **Read Version**: Reads the version from `.semconv-version` file
2. **Validate Manifest Consistency**: Checks that the upstream dependency in `schemas/otelc/registry_manifest.yaml` matches `.semconv-version`
3. **Validate Code Consistency**: Checks that Go imports in `instrumentation/**/semconv/` match the version in `.semconv-version`
4. **Registry Validation**: Runs `make lint-schema` to validate `schemas/otelc/` with weaver
   - **This check is blocking** - violations will fail the PR

**What This Checks**:

- `.semconv-version`, the registry manifest dependency, and the `semconv` imports in Go code all agree
- The `schemas/otelc/` registry is valid against the pinned upstream semconv (no unexpected weaver diagnostics)

#### Job 2: Check Available Updates (Non-blocking)

This job shows what's new in the latest semantic conventions:

1. **Generate Diff**: Runs `make registry-diff` to compare current version vs latest
2. **Upload Report**: Uploads the diff report as an artifact
3. **PR Comment**: Posts an informational comment showing:
   - What new semantic conventions are available
   - Whether you're using the latest version
   - Suggestions for updating (if desired)

**What This Checks**:

- Shows available updates (informational only)
- **This check is non-blocking** - it will never fail your PR
- Helps you stay informed about new conventions without requiring immediate action

### On Main Branch

When changes are merged to `main`:

1. **Read Version**: Reads the version from `.semconv-version`
2. **Registry Validation**: Runs `make lint-schema` to ensure the `schemas/otelc/` registry stays valid

### How It Works

The CI workflow uses the Make targets defined in the Makefile:

- `make lint-schema`: Validates the `schemas/otelc/` registry with weaver via Docker (blocking check)
- `make semantic-conventions/diff`: Generates upstream diff report (non-blocking check; uses `make weaver-install`)

This approach:

- Reduces code duplication between CI and local development
- Ensures CI uses the same validation logic as developers
- Makes it easy to run the same checks locally before pushing

### When to Update Semantic Conventions

Consider updating your `semconv` version when:

- The "Available Updates" section shows relevant new conventions
- You need new attributes or metrics added in newer versions
- You want to adopt breaking changes or improvements

**Steps to update**:

1. Review the "Available Updates" diff
2. Update Go imports in `instrumentation/**/semconv/`: `semconv/v1.30.0` → `semconv/v1.31.0`
3. Update the version in `.semconv-version` file
4. Update the upstream dependency in `schemas/otelc/registry_manifest.yaml` to match
5. Update code to handle any breaking changes
6. Run `make lint-schema` to validate the new version
7. Run tests: `make test`

## Best Practices

### 1. Use Standard Attributes First

Always prefer existing semantic conventions from the official registry. Only create custom attributes when necessary.

### 2. Follow Naming Conventions

- Use dot notation: `namespace.concept.attribute`
- Use snake_case for multi-word attributes: `http.response.status_code`
- Be specific and avoid abbreviations: `client.address` not `cli.addr`

### 3. Document Thoroughly

Include:

- Clear description of the attribute's purpose
- Expected type (string, int, boolean, etc.)
- Example values
- Any constraints or valid ranges

### 4. Version Compatibility

When updating semantic conventions:

- Check for breaking changes in the diff report
- Update dependent code accordingly
- Update documentation to reflect changes

### 5. Test Impact

After modifying semantic conventions:

- Run all tests: `make test`
- Test with demo applications: `make build-demo`
- Verify instrumentation still works correctly

## Troubleshooting

### Weaver Installation Fails

If automatic installation fails:

1. **Check your platform**: Weaver supports macOS (Intel/ARM) and Linux (x86_64)
2. **Manual installation**: Download from [weaver releases](https://github.com/open-telemetry/weaver/releases)
3. **Verify installation**: Run `weaver --version`

### Registry Validation Errors

Common validation errors and solutions:

- **Invalid attribute name**: Ensure you follow the dot notation and naming conventions
- **Missing required field**: Add all required fields (name, type, description)
- **Type mismatch**: Ensure attribute type matches the expected schema type
- **Deprecated pattern**: Update to use current semantic convention patterns

### Diff Report Shows Unexpected Changes

If the diff report shows changes you didn't make:

1. **Check baseline version**: Ensure you're comparing against the correct baseline
2. **Update local registry**: Pull latest changes from the semantic conventions repository
3. **Review upstream changes**: Check the [semantic conventions changelog](https://github.com/open-telemetry/semantic-conventions/releases)

## Additional Resources

- [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/concepts/semantic-conventions/)
- [Semantic Conventions Repository](https://github.com/open-telemetry/semantic-conventions)
- [OTel Weaver Documentation](https://github.com/open-telemetry/weaver)
- [Attribute Naming Guidelines](https://opentelemetry.io/docs/specs/semconv/general/attribute-naming/)

## Questions or Issues?

If you encounter issues with semantic conventions tooling:

1. Check the [GitHub Issues](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues)
2. Ask in the [#otel-go-compile-instrumentation](https://cloud-native.slack.com/archives/C088D8GSSSF) Slack channel
3. Open a new issue with details about your problem
