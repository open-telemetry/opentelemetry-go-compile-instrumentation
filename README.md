# OpenTelemetry-Go-Compile-Instrumentation



OpenTelemetry-Go-Compile-Instrumentation provides compile time  [OpenTelemetry](https://opentelemetry.io/) instrumentation for [Golang](https://golang.org/).

## Project Status

| Signal  | Status             |
|---------|--------------------|
| Traces  | WIP             |
| Metrics | WIP             |
| Logs    | Not started       |
| Profiling    | Not started  |


## Getting Started

Run `sh -x build.sh` to show instrumentation example. In this example, we will inject a piece of code into the `main` function of the `main` package under the `demo` module to output the "Entering hook" string. This injected code comes from the `sdk` module.
If you want to check the result of instrumentation, go to the directory location that appears as output when running build.sh, e.g., `WORK=/var/folders/x9/fddsvlt5363c0plvvw8_2mr80000gn/T/go-build2020695287`.

### Navigate to the WORK directory:
```bash
cd /var/folders/x9/fddsvlt5363c0plvvw8_2mr80000gn/T/go-build2020695287
```

### Locate the main package under the b001 subdirectory:
```bash
ls -l b001
```

### Inspect files:
```bash
cat modified.go
```

## Contributing

See the [contributing documentation](CONTRIBUTING.md).

## License

OpenTelemetry Go Compile Instrumentation project is licensed under the terms of the [Apache Software License version 2.0].
See the [license file](./LICENSE) for more details.
