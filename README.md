# OpenTelemetry-Go-Compile-Instrumentation

OpenTelemetry-Go-Compile-Instrumentation provides compile time [OpenTelemetry](https://opentelemetry.io/) instrumentation for [Golang](https://golang.org/).

## Getting Started

### Prerequisites

- Go 1.23.0 or later
- Git

### Quick Start

1. **Clone the repository:**
   ```bash
   git clone https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation.git
   cd opentelemetry-go-compile-instrumentation
   ```

2. **Build the instrumentation tool:**
   ```bash
   make build
   ```

3. **Run the demo with instrumentation:**
   ```bash
   make demo
   ```

   This will:
   - Build the instrumentation tool (`otel` binary)
   - Run the demo with compile-time instrumentation
   - Execute the instrumented binary

## How It Works

The instrumentation tool injects OpenTelemetry code at compile time using Go's `-toolexec` flag. The process works as follows:

1. **Setup Phase**: Creates `.otel-build` directory and initializes logging
2. **Go Build Phase**: Intercepts the `go build` command and adds `-toolexec=./otel` flag
3. **Toolexec Phase**: The Go toolchain invokes the tool for each compilation step, allowing code injection. This phase is not visible to the user.

### Logging

The tool provides detailed logging to help debug instrumentation issues:
- Logs are written to `.otel-build/debug.log` for setup and go actions
- Toolexec action logs to stdout for immediate feedback
- All logs include structured information about the build process

### Inspecting the Instrumentation

To see the result of instrumentation, navigate to the WORK directory that appears in the build output:

```bash
# Example WORK directory
cd /var/folders/x9/fddsvlt5363c0plvvw8_2mr80000gn/T/go-build2020695287

# Locate the main package under the b001 subdirectory
ls -l b001

# Inspect the modified files
cat modified.go
```

## Contributing

See the [contributing documentation](CONTRIBUTING.md).

## License

OpenTelemetry Go Compile Instrumentation project is licensed under the terms of the [Apache Software License version 2.0].
See the [license file](./LICENSE) for more details.
