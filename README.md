# OpenTelemetry Go Compile Instrumentation

> [!IMPORTANT]
> This is a work in progress and not ready for production use. 🚨

This project provides a tool to automatically instrument Go applications with [OpenTelemetry](https://opentelemetry.io/) at compile time. The tool modifies the Go build process to inject OpenTelemetry code into the application without requiring manual changes to the source code.

## Getting Started

1. Build the otel tool
```bash
$ git clone https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation.git
$ cd opentelemetry-go-compile-instrumentation
$ make build
```

2. Build the application with the tool and run it
```bash
$ make demo
```

## Project Structure
- `docs/`: Documentation for the project
- `demo/`: A sample Go application to demonstrate the tool
- `instrumentation/`: Contains the instrumentation logic for various Go packages
- `pkg/`: The core library used by `instrumentation/`, including utilities and common functionality
  - `pkg/inst/`: Definition of the instrumentation context
  - `pkg/inst-api/`: The API for the instrumentation
  - `pkg/inst-api-semconv/`: The semantic conventions for the instrumentation
- `tool/`: The main tool for compile-time instrumentation
  - `tool/internal/setup/`: Setup phase, it prepares the environment for future instrumentation phase
  - `tool/internal/instrument`: Instrument phase, where the actual instrumentation happens
  - `tool/internal/rule/`: The rule describes how to match the target function and which instrumentation to apply

## Contributing

See the [contributing documentation](./docs/CONTRIBUTING.md).

For the code of conduct, please refer to our [OpenTelemetry Community Code of Conduct](https://github.com/open-telemetry/community/blob/main/code-of-conduct.md)

## License

This project is licensed under the terms of the [Apache Software License version 2.0].
See the [license file](./LICENSE) for more details.
