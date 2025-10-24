# OpenTelemetry Go Compile Instrumentation

[![logo](https://img.shields.io/badge/slack-@cncf/otel--gocomp-blue.svg?logo=opentelemetry)](https://cloud-native.slack.com/archives/C088D8GSSSF)  &nbsp;

<img src="./docs/assets/otel-logo.png" alt="OpenTelemetry Logo" width="500">

> [!IMPORTANT]
> This is a work in progress and not ready for production use. 🚨

This project provides a tool to automatically instrument Go applications with
[OpenTelemetry](https://opentelemetry.io/) at compile time.

It modifies the Go build process to inject OpenTelemetry code into the application without
requiring manual changes to the source code.

## Getting Started

1. Build the otel tool

    ```bash
    git clone https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation.git
    cd opentelemetry-go-compile-instrumentation
    make build
    ```

2. Run the test

    ```bash
    make test
    ```

## Contributing

See the [contributing documentation](CONTRIBUTING.md) for general contribution guidelines.

See the [developing documentation](./docs/developing.md) for tool development.

For the code of conduct, please refer to our [OpenTelemetry Community Code of Conduct](https://github.com/open-telemetry/community/blob/main/code-of-conduct.md)

## License

This project is licensed under the terms of the [Apache Software License version 2.0](./LICENSE).
