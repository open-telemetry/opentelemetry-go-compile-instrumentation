# Getting Started

OpenTelemetry Go Compile-Time Instrumentation is a tool that automatically instruments your Go applications with [OpenTelemetry](https://opentelemetry.io/) at compile-time.
No manual code changes required.

## Why Use This Tool?

- **Zero-code instrumentation** - Automatically instrument your entire application without modifying source code
- **Third-party library support** - Instrument dependencies and libraries you don't control
- **Complete decoupling** - Keep your codebase free from instrumentation concerns
- **Flexible deployment** - Integrate at development time or in your CI/CD pipeline

## Quick Start

1. **Clone and build the tool**

   ```bash
   git clone https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation.git
   cd opentelemetry-go-compile-instrumentation
   make build
   ```

2. **Try the demo**

   ```bash
   make demo
   ```

3. **Use with your application**

   ```bash
   # Option 1: Direct build
   ./otel go build -o myapp .

   # Option 2: Install as tool dependency (Go 1.24+)
   go get -tool github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/otel
   go tool otel go build -o myapp .
   ```

## How It Works

The tool uses compile-time instrumentation through:

1. **Trampoline Code Injection** - Injects lightweight hook points into target functions
2. **Function Pointer Redirection** - Links hooks to monitoring code via `//go:linkname`
3. **Custom Toolchain Integration** - Intercepts compilation using `-toolexec` flag

This approach provides dynamic instrumentation without runtime overhead or invasive code modifications.

## Learn More

- [User Experience Design](./ux-design.md) - Detailed UX documentation and configuration options
- [Implementation Details](./implementation.md) - Technical architecture and internals
- [API Design](./api-design-and-project-structure.md) - API structure and project organization
- [Contributing Guide](../CONTRIBUTING.md) - How to contribute to the project

### Video Talks

Learn more about the project from these presentations:

- [OpenTelemetry Go Compile-Time Instrumentation Overview](https://www.youtube.com/watch?v=xEsVOhBdlZY)
- [Deep Dive: Conceptual details](https://www.youtube.com/watch?v=8Rw-fVEjihw&list=PLDWZ5uzn69ewrYyHTNrXlrWVDjLiOX0Yb&index=19)

## Community

- **Slack**: Join [#otel-go-compt-instr-sig](https://cloud-native.slack.com/archives/C088D8GSSSF)
- **Meetings**: Check the [meeting notes](https://docs.google.com/document/d/1XkVahJfhf482d3WVHsvUUDaGzHc8TO3sqQlSS80mpGY/edit) for SIG schedules
- **GitHub**: [open-telemetry/opentelemetry-go-compile-instrumentation](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation)

## Status

> **Note**: This project is currently in active development and not yet ready for production use.

For the latest updates and development progress, follow the project on GitHub and join the community discussions.
