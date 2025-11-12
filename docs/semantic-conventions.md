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

Compare the current semantic convention registry against a baseline version to see what has changed:

```bash
make registry-diff
```

By default, this compares against `v1.29.0`. To use a different baseline:

```bash
BASELINE_VERSION=v1.28.0 make registry-diff
```

This command:

- Generates a markdown report showing differences between versions
- Highlights added, modified, and removed attributes
- Saves the report to `tmp/registry-diff.md`
- Displays the report in your terminal

**When to use**: Use this to understand changes when updating semantic convention dependencies or when adding new attributes.

### Resolve Registry Schema

Generate a resolved, flattened view of the entire semantic convention registry:

```bash
make semantic-conventions/resolve
```

This command:

- Fetches the complete semantic convention registry
- Resolves all references and inheritance
- Outputs a single YAML file with all definitions
- Saves the output to `tmp/resolved-schema.yaml`

**When to use**: Use this when you need to inspect the complete schema or debug attribute definitions.

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

1. **Registry Validation**: Validates that definitions follow the correct schema
2. **Diff Report**: Generates a comparison against the upstream registry
3. **PR Comment**: Posts a summary of changes as a PR comment
4. **Blocking Check**: CI will **fail** if validation errors are found, preventing merge until issues are resolved

### On Main Branch

When changes are merged to `main`:

1. **Registry Validation**: Re-validates the current state
2. **Baseline Update**: Establishes a new baseline for future comparisons

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
2. Ask in the [#otel-go-compt-instr-sig](https://cloud-native.slack.com/archives/C088D8GSSSF) Slack channel
3. Open a new issue with details about your problem
