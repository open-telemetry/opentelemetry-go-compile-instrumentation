# Semantic Conventions Management

This document describes the tooling and workflow for managing [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/concepts/semantic-conventions/) in the compile-instrumentation project.

## Overview

Semantic conventions define a common set of attribute names and values used across OpenTelemetry projects to ensure consistency and interoperability. This project uses [OTel Weaver](https://github.com/open-telemetry/weaver) to validate and track changes to semantic conventions.

## Version Management

The project's semantic conventions version is tracked in the `.semconv-version` file at the root of the repository. This file:

- Specifies which semantic conventions version the project intends to abide by
- Must match the `semconv` imports used in `pkg/inst-api-semconv/` Go code
- Is validated by CI to ensure consistency

**Example `.semconv-version` file**:

```
v1.30.0
```

When updating to a new semantic conventions version:

1. Update the version in `.semconv-version`
2. Update Go imports in `pkg/inst-api-semconv/` to match
3. Run `make registry-check` to validate
4. Update code to handle any breaking changes

## Prerequisites

The semantic conventions tooling requires OTel Weaver. It will be automatically installed when you run the related make targets:

```bash
make weaver-install
```

This installs the weaver CLI tool to `$GOPATH/bin`. Ensure your `$GOPATH/bin` is in your `PATH`.

## Available Targets

### Validate Semantic Conventions

Validate that the project's semantic conventions adhere to the registry at the specified version:

```bash
make registry-check
```

This command:

- Reads the version from `.semconv-version`
- Validates the semantic convention registry at that version
- Reports any violations or deprecated patterns
- Uses the `--future` flag to enable stricter validation rules
- **This check is blocking** - violations will fail CI

**When to use**: Run this before committing changes to semantic convention definitions in `pkg/inst-api-semconv/`.

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

When you modify files in `pkg/inst-api-semconv/` or `.semconv-version`:

#### Job 1: Validate Semantic Conventions (Blocking)

This job ensures your code follows the correct semantic conventions version:

1. **Read Version**: Reads the version from `.semconv-version` file
2. **Validate Consistency**: Checks that Go imports in `pkg/inst-api-semconv/` match the version in `.semconv-version`
3. **Registry Validation**: Runs `make registry-check` to validate against the registry
   - **This check is blocking** - violations will fail the PR

**What This Checks**:

- The version in `.semconv-version` matches the `semconv` imports in Go code
- The semantic conventions registry at that version is valid (no violations)
- Your code adheres to the conventions for your specified version

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
2. **Registry Validation**: Validates that version's registry to ensure continued compliance

### How It Works

The CI workflow uses the Make targets defined in the Makefile:

- `make weaver-install`: Installs OTel Weaver
- `make registry-check`: Validates the registry (blocking check)
- `make registry-diff`: Generates diff report (non-blocking check)

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
2. Update Go imports: `semconv/v1.30.0` → `semconv/v1.31.0`
3. Update the version in `.semconv-version` file
4. Update code to handle any breaking changes
5. Run `make registry-check` to validate the new version
6. Run tests: `make test`

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
