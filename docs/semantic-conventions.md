# Semantic Conventions Management

This document describes the tooling and workflow for managing [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/concepts/semantic-conventions/) in the compile-instrumentation project.

## Overview

Semantic conventions define a common set of attribute names and values used across OpenTelemetry projects to ensure consistency and interoperability. This project uses [OTel Weaver](https://github.com/open-telemetry/weaver) to validate and track changes to semantic conventions.

## Prerequisites

The semantic conventions tooling requires OTel Weaver. It will be automatically installed when you run the related make targets:

```bash
make weaver-install
```

This installs the weaver CLI tool to `$GOPATH/bin`. Ensure your `$GOPATH/bin` is in your `PATH`.

## Available Targets

### Validate Semantic Conventions

Validate that your semantic convention definitions follow the correct schema and conventions:

```bash
make lint/semantic-conventions
```

This command:

- Checks the semantic convention registry against the official schema
- Validates attribute names, types, and definitions
- Reports any schema violations or deprecated patterns
- Uses the `--future` flag to enable stricter validation rules

**When to use**: Run this before committing changes to semantic convention definitions in `pkg/inst-api-semconv/`.

### Generate Registry Diff

Compare semantic convention versions to understand changes and available updates:

```bash
make registry-diff
```

This command automatically:

1. **Detects** the `semconv` version used in your code (e.g., `v1.30.0`)
2. **Generates two comparison reports**:
   - **Current vs Baseline**: What changed in your version vs `v1.29.0`
   - **Latest vs Current**: What new features are available if you upgrade

By default, this compares against `v1.29.0`. To use a different baseline:

```bash
BASELINE_VERSION=v1.28.0 make registry-diff
```

**Output files**:

- `tmp/registry-diff-baseline.md` - Changes since baseline
- `tmp/registry-diff-latest.md` - Available updates

**Example output**:

```
Detected project semconv version: v1.30.0
Baseline version: v1.29.0

Changes in your current version (v1.30.0 vs v1.29.0):
- Added: http.request.body.size
- Modified: http.response.status_code description
...

Available updates (latest vs v1.30.0):
- Added: db.client.connection.state
- Deprecated: net.peer.name (use server.address)
...
```

**When to use**:

- Understanding what's in your current semconv version
- Deciding whether to upgrade to a newer version
- Reviewing changes before modifying `pkg/inst-api-semconv/`

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

1. Add your attribute definition to the appropriate file in `pkg/inst-api-semconv/instrumenter/`
2. Follow the [OpenTelemetry attribute naming conventions](https://opentelemetry.io/docs/specs/semconv/general/attribute-naming/)
3. Include proper documentation and examples

Example structure:

```go
// pkg/inst-api-semconv/instrumenter/http/http.go
package http

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
pkg/inst-api-semconv/
├── instrumenter/
│   ├── http/           # HTTP semantic conventions
│   │   ├── http.go
│   │   └── ...
│   ├── net/            # Network semantic conventions
│   │   ├── net.go
│   │   └── ...
│   └── utils/          # Utility functions
```

These definitions extend or implement the official [OpenTelemetry Semantic Conventions](https://github.com/open-telemetry/semantic-conventions) for use in compile-time instrumentation.

## Continuous Integration

The project includes automated checks for semantic conventions:

### On Pull Requests

When you modify files in `pkg/inst-api-semconv/`:

1. **Version Detection**: Automatically detects the `semconv` version used in the Go code (e.g., `v1.30.0`)
2. **Registry Validation**: Validates the semantic conventions registry at the detected version to ensure it's valid
3. **Diff Reports**: Generates two comparison reports:
   - **Current vs Baseline**: Shows changes between your version and the baseline (v1.29.0)
   - **Latest vs Current**: Shows available updates if you upgrade to the latest semantic conventions
4. **PR Comment**: Posts a comprehensive diff report as a PR comment with:
   - What changed in your current version
   - What new features/changes are available in newer versions
   - Action items for ensuring code compliance

**What This Checks**:

- Validates the semantic conventions version you're using is valid
- Shows what changed in that version compared to baseline
- Shows what's available if you upgrade to newer versions
- Helps ensure your Go code aligns with the correct semconv version

**What This Doesn't Check**:

- Does not validate Go code syntax or logic (use `make lint` and `make test`)
- Does not enforce upgrading to latest version (informational only)

### On Main Branch

When changes are merged to `main`:

1. **Version Detection**: Detects the current `semconv` version in use
2. **Registry Validation**: Validates that version's registry to ensure continued compliance

### How It Works

The CI workflow:

1. Scans your Go files for `semconv/vX.Y.Z` imports
2. Validates that specific version's registry using OTel Weaver
3. Compares against baseline and latest to show evolution
4. Posts actionable information to help you maintain compliance

### When to Update Semantic Conventions

Consider updating your `semconv` version when:

- The "Available Updates" section shows relevant new conventions
- You need new attributes or metrics added in newer versions
- You want to adopt breaking changes or improvements

**Steps to update**:

1. Review the "Available Updates" diff
2. Update Go imports: `semconv/v1.30.0` → `semconv/v1.31.0`
3. Update `CURRENT_SEMCONV_VERSION` in `.github/workflows/check-registry-diff.yaml`
4. Update code to handle any breaking changes
5. Run tests: `make test`

## Best Practices

### 1. Use Standard Attributes First

Always prefer existing semantic conventions from the official registry. Only create custom attributes when necessary.

### 2. Follow Naming Conventions

- Use dot notation: `namespace.concept.attribute`
- Use snake_case for multi-word attributes: `http.request.method`
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
