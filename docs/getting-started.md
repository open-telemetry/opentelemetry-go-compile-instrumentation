# Getting Started

OpenTelemetry Go Compile-Time Instrumentation is a tool that automatically instruments your Go applications with [OpenTelemetry](https://opentelemetry.io/) at compile-time.
No manual code changes required.

## Why Use This Tool?

- **Zero-code instrumentation** - Automatically instrument your entire application without modifying source code
- **Third-party library support** - Instrument dependencies and libraries you don't control (HTTP, gRPC, database/sql, Redis, OpenAI, Anthropic, and more)
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
   cd demo/app/basic
   ../../../otelc go build
   ./basic
   ```

3. **Use with your application**

   ```bash
   # Option 1: Direct build
   ./otelc go build -o myapp .

   # Option 2: Install as tool dependency (Go 1.24+)
   go get -tool go.opentelemetry.io/otelc/tool/cmd/otelc
   go tool otelc go build -o myapp .
   ```

## Using `go build` Directly (toolexec drop-in)

Instead of wrapping the build with `otelc go build`, you can keep using the
regular `go` toolchain and plug `otelc` in through `GOFLAGS`. This is useful
when the build command is owned by a Makefile, CI pipeline, or another tool
you don't want to change.

1. **Prepare the module** (once, and again after dependencies change):

   ```bash
   cd path/to/your/module
   otelc setup
   ```

   `otelc setup` analyzes the module, generates the instrumentation sources
   (`otel.instrumentation.go`, `otelc.runtime.go`) and writes the matched
   rules to `.otelc-build/`, which the build phase below reads.

2. **Build with the standard toolchain**:

   ```bash
   export GOFLAGS="${GOFLAGS} '-toolexec=otelc toolexec'"
   go build -o myapp .
   ```

Run `go build` from the module directory (or any subdirectory); `otelc`
locates the `.otelc-build` directory created by `otelc setup` from there. To
run the build from somewhere else, set `OTELC_WORK_DIR` to the directory
where `otelc setup` ran.

Instrumented and plain build artifacts are kept apart in Go's build cache
(otelc marks the tool identity go hashes into every cache key), so switching
between instrumented and regular builds does not require cleaning the cache.

## Managing Instrumentations

Instrumentations are declared through an `otel.instrumentation.go` file located next to the application's `go.mod` file. The alternate filename `otelc.tool.go` is also accepted and behaves identically.

The file follows the standard Go `tools.go` pattern and contains blank imports for the instrumentation packages that should be enabled:

```go
//go:build tools

package tools

import (
	_ "go.opentelemetry.io/otelc/instrumentation/net/http/server" // enable net/http server instrumentation
	_ "go.opentelemetry.io/otelc/instrumentation/github.com/gin-gonic/gin" // enable gin instrumentation
)
```

The file can be created and maintained automatically using:

```bash
otelc pin
```

The `pin` command discovers applicable instrumentations, creates the file if it does not already exist, updates imports, synchronizes dependencies, and runs validation checks.

If no instrumentation file exists, `otelc go build` automatically analyzes the application's dependency graph and generates a temporary instrumentation configuration for the duration of the build. This ensures a zero-configuration workflow while allowing projects to adopt a persistent, source-controlled configuration when desired.

> [!NOTE]
> Support for committing an `otelc pin`-generated `otel.instrumentation.go` file is still under development. Until the work in [#585](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/585) to decouple instrumentation packages from the `otelc` executable is complete, `otelc pin` should be considered a local workflow. `otelc go build` continues to work without a committed instrumentation file by automatically generating a temporary configuration during the build.

## Vendored Projects

When your module has a `vendor/` directory, `otelc` keeps vendor mode in the
same cases as the go command: `go.mod` has go 1.14 or higher, the build is
not in workspace mode, and no `-mod=mod` or `-mod=readonly` flag is set (see
[the go version rules](#go-version-for-vendor-mode) below). Your
dependencies come from `vendor/`, and `otelc` keeps your changes to vendored
files: it never replaces a file that is in `vendor/`.

The hook code of `otelc` needs more modules (for example the OpenTelemetry
SDK). For the time of the build, `otelc go build` adds them to `vendor/`,
`vendor/modules.txt` and `go.mod`. After the build, `go.mod`, `go.sum` and
`vendor/` are the same as before. These modules come from the module cache or
the network.

`otelc` downloads a module that you vendor in one case only: the hook code
needs a package of this module that `vendor/` does not have. Then `otelc`
gets the module at the vendored version (from the module cache or the
network), and adds only the missing package. If `otelc` cannot get it (for
example a private module, or no network), the build stops. The message names
the package and the module version. To fix it, import the package in your
module and run `go mod vendor`, or run `otelc vendor` with network access.
The download uses your Go settings: set `GOPRIVATE` (or `GOPROXY=off`) if the
path of a private module must not go to a public proxy.

To build with no network and no module cache (for example in an air-gapped
CI), add the modules of the hook code to `vendor/` for all time:

```bash
go mod vendor
otelc vendor
```

`otelc vendor` writes `otel.instrumentation.go`, and adds the modules to
`vendor/`, `go.mod` and `go.sum`. You can commit these files, but know what
works in a new checkout (a checkout without `.otelc-build/`):

- `otelc go build` and `go build` work, also without network.
- `go mod tidy`, `go mod vendor` and `go mod verify` fail. The `replace`
  directives that `otelc vendor` adds point to `./.otelc-build`, which you do
  not commit, and these commands read the directories. Run `otelc vendor`
  first: it makes the directory again. This also applies to tools that run
  these commands, for example Dependabot or a CI job.

This stays until the instrumentation modules are published (see
[#585](https://github.com/open-telemetry/opentelemetry-go-compile-instrumentation/issues/585)),
the same as for `otelc pin` above.

`go mod vendor` writes a new `vendor/modules.txt` without the `otelc-bundle`
annotations (see below), so run `otelc vendor` again after `go mod vendor`.
If `otelc vendor` fails, it reverts all its changes.

`otelc vendor` works like `go mod vendor`, but it includes the needs of the
hook code. When the hook code (or one of its dependencies) needs a higher
version of a vendored module, `otelc vendor` gets this version, like
`go get` does. This also applies to the vendored modules that minimal
version selection raises because of the new modules, so that `go.mod` stays
correct for a later `go mod vendor`. It changes the requirement in `go.mod` and replaces the
vendored files of the module, so your changes to the files of this module are
lost. It prints each change, for example
`Bumped dependency golang.org/x/net (v0.51.0 -> v0.58.0)`.

`otelc vendor` writes a fingerprint of the `otelc` version in
`vendor/modules.txt` (the `otelc-bundle` annotation; the go command ignores
it). When a different `otelc` version builds the project, it finds that the
otelc modules in `vendor/` are old, and adds its own modules for the time of
the build. Run `otelc vendor` again after you update `otelc`.

<a id="go-version-for-vendor-mode"></a>
Vendor mode needs go 1.14 or higher in `go.mod`, as for the go command:

- With a lower go version (or no `go` directive) and no `-mod` flag, the go
  command does not use `vendor/`, so `otelc` does not use it either.
- With a lower go version, `-mod=vendor` (in the arguments or in `GOFLAGS`)
  and `otelc vendor` stop with an error. To fix it, run
  `go mod edit -go=1.14 && go mod vendor`.

`otelc vendor` uses only the vendor directory. It rejects `-mod=mod`,
`-mod=readonly` and `-modfile`.

`otelc` finds the vendor mode in the directory of the build: the `-C`
directory when you give one, else the current directory. A `-mod` flag on the
command line applies to all the go commands that `otelc` runs for the build.

Custom rules (`--rules` or `OTELC_RULES`) also work in vendor mode: `otelc`
adds the hook packages of the matched rules to `vendor/` for the time of the
build, as for the built-in instrumentation.

If the hook code (or one of its dependencies) needs a higher version of a
module than the one in `vendor/`, `otelc go build` stops with a
`vendor conflict` error. See
[Troubleshooting](troubleshooting.md#vendor-conflict).

Limitations:

- The vendor directory of a workspace (`go work vendor`) is not supported.
  In workspace mode, `otelc` uses module mode.
- For each build that adds modules to `vendor/`, `otelc` copies the vendored
  packages of your modules into `.otelc-build/vendor-scratch`. On macOS
  (APFS) the copies are clones, and on Linux hard links, when the file system
  supports them; else they are full copies. With a large `vendor/` directory
  (about 20,000 files), this step takes about 3 s on macOS, and less than
  1 s on Linux with hard links. After `otelc vendor`, no copy is necessary.
- `otelc` does not follow symbolic links in `vendor/`. A symbolic link at a
  path that `otelc` reads or writes stops the build. `otelc` checks the paths
  before it uses them. It does not protect against a different process that
  changes `vendor/` at the same time.

## How It Works

The tool uses compile-time instrumentation through:

1. **Trampoline Code Injection** - Injects lightweight hook points into target functions
2. **Function Pointer Redirection** - Automatically generates and links hooks to monitoring code via `//go:linkname`
3. **Custom Toolchain Integration** - Intercepts compilation using `-toolexec` flag

This approach provides dynamic instrumentation without runtime overhead or invasive code modifications.

## Supported Libraries

The following libraries are automatically instrumented:

| Library | Semantic Conventions |
| --- | --- |
| `net/http` (client & server) | HTTP spans |
| `google.golang.org/grpc` (client & server) | gRPC/RPC spans |
| `database/sql` | DB client spans |
| `github.com/gin-gonic/gin` | HTTP server spans |
| `github.com/gofiber/fiber/v2` | HTTP server spans |
| `github.com/labstack/echo/v4` | HTTP server spans |
| `github.com/redis/go-redis/v9` | Redis DB spans |
| `go.mongodb.org/mongo-driver` | MongoDB DB spans |
| `k8s.io/client-go` | K8s resource spans |
| `github.com/openai/openai-go` (v1/v2/v3) | GenAI spans |
| `github.com/anthropics/anthropic-sdk-go` | GenAI spans |
| `github.com/segmentio/kafka-go` | Kafka messaging spans |
| `github.com/rabbitmq/amqp091-go` | RabbitMQ messaging spans |
| `github.com/aws/aws-sdk-go-v2` | AWS SDK client spans |
| `github.com/linode/linodego/v2` | HTTP client spans and metrics |
| `github.com/stripe/stripe-go` (v81/v82) | Stripe API client spans and metrics (net/http RoundTrip spans become children when both are enabled) |
| `log`, `log/slog`, `github.com/sirupsen/logrus`, `go.uber.org/zap` | Trace/span ID log correlation |
| `github.com/apache/cassandra-gocql-driver/v2` | Cassandra DB spans |
| `github.com/olivere/elastic/v7` | Elasticsearch DB spans |
| `github.com/opensearch-project/opensearch-go/v5` | OpenSearch DB spans |

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

This project is stable and ready for production use as of v1.0.0.

For the latest updates, follow the project on GitHub and join the community discussions.
