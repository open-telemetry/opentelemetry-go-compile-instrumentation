# User Experience Design

## Introduction

This document outlines the essential aspects of the user experience of the new
OpenTelemetry standard tool for compile-time instrumentation of Go applications.
This is a living document bound to change as the product is being built,
matures, and receives user feedback.

## Intended Purpose

The OpenTelemetry Go compile-time instrumentation tool allows users to
instrument their full application automatically, without having to perform any
significant code change beyond adding simple, well-defined configuration that
may be as simple as adding the relevant tool dependencies.

The main value proposition is:
- Very little effort is required for holistic instrumentation
- Ability to instrument within third-party dependencies
- Keeping the codebase completely de-coupled from instrumentation

## Target Audience

Compile-time instrumentation, like other techniques of automatic
instrumentation, does not afford users the same amount of control over their
instrumentation as manual instrumentation; it trades very granular control for
significantly reduced implementation effort. As a result, compile-time
instrumentation may not appeal to developers who have very specific requirements
on what their instrumentation produces.

The primary audience for the OpenTelemetry Go compile-time instrumentation tool
is composed of the following personas:

- Application developers looking for a no-frills, turnkey instrumentation
  solution
- System operators look to instrument applications without involving developers

The tool may however also be relevant to the following personas:
- Security personnel looking for maximal instrumentation coverage
- Library developers looking to improve the instrumentation experience for their
  library

Large applications and deployments may involve multiple of these personas,
possibly including some that are not part of the primary audience. This is
particularly true for Enterprise scale companies, where different parts of the
organization are often involved at different stages of the software delivery
lifecycle. It is hence important the OpenTelemetry Go compile-time
instrumentation tool allows co-operation between each of these entities without
coupling them too tightly.

## User Experience Overview

The OpenTelemetry Go compile-time instrumentation tool is composed of the
following software artifacts (in the form of Go packages):

- `github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/otel`,
  a command-line tool that can be installed using `go install` or using
  `go get -tool`;
- `github.com/open-telemetry/opentelemetry-go-compile-instrumentation/runtime`,
  a small, self-contained package that contains essential runtime functionality
  used by instrumented applications (not intended for manual usage).

### Getting Started

The tool offers a wizard for getting started with the tool automatically by
following a series of prompts:

```console
$ go run github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/otel@latest setup
╭──────────────────────────────────────────────────────────────────────╮
│                                                                      │
│           OpenTelemetry compile-time instrumentation tool            │
│                    v1.2.3 (go1.24.1 darwin/arm64)                    │
│                                                                      │
╰──────────────────────────────────────────────────────────────────────╯
✨ This setup assistant will guide you through the steps needed to properly
   configure otel for your application. After you've answered all the prompts,
   it will present you with the list of actions it will execute; and you will
   have an choice to apply those or not.
🤖 Press enter to continue...

ℹ️ Registering otel as a tool dependency is recommended: it allows you to
   manage the dependency on otel like any other dependency of your application,
   via the go.mod file. When using go 1.24 or newer, you can use `go tool otel`
   to use the correct version of the tool (for more information, see:
   https://go.dev/doc/modules/managing-dependencies#tools).
   Not registering a go tool dependency allows instrumenting applications
   without modifying their codebase at all (not even the `go.mod` file); which
   may be preferred for building third-party applications or integrating in the
   CI/CD pipeline. The reproductibility of builds is no longer guaranteed by the
   go toolchain, and the application may be built with newer versions of
   dependencies than those in the `go.mod` file if the enabled instrumentation
   package(s) requires it.
🤖 Should I add otel as a tool dependency?
   [Yes]  No
🆗 I will add a tool dependency

ℹ️ You may enable one or more instrumentation packages for your application.
   Most users need to choose only one. This can be changed at any time.
🤖 What instrumentation do you want to enable for this project? (Select one or
   more using space, then press enter to confirm your selection)
   [X] Everything      (github.com/open-telemetry/opentelemetry-go)
   [ ] Databases       (github.com/open-telemetry/opentelemetry-go/db)
   [ ] GRPC Service    (github.com/open-telemetry/opentelemetry-go/grpc)
   [ ] HTTP Service    (github.com/open-telemetry/opentelemetry-go/http)
   [ ] Message Streams (github.com/open-telemetry/opentelemetry-go/msgstream)
   [ ] Other
🆗 I will configure the following instrumentation: OpenTelemetry

ℹ️ Using go tool dependencies or a `otel.instrumentation.go` file to configure
   integrations is recommended as it ensures the instrumentation packages are
   represented in your `go.mod` file, making builds reproducible.
   Using a `.otel.yml` file is useful when instrumenting applications without
   modifying their codebase at all; which may be preferable when building
   third-party applications or integrating in the CI/CD pipeline. The
   reproductibility of builds is no longer guaranteed by the go toolchain, and
   the application may be built with newer versions of dependencies than those
   in the `go.mod` file if the enabled instrumentation package(s) require it.
🤖 How do you want to configure instrumentation for this project?
   (*) Using go tool dependencies (Recommended)
   ( ) Using the `otel.instrumentation.go` file
   ( ) Using a `.otel.yml` file
🆗 I will use go tool dependencies to enable integration packages.

🤖 We're all set! Based on your answers, I will execute the following commands:
   $ go get -tool github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/otel@v1.2.3
   $ go get -tool github.com/open-telemetry/opentelemetry-go
🤖 Should I proceed?
   [Yes]  No

🆗 Let's go!
✅ go get -tool github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/otel@v1.2.3
✅ go get -tool github.com/open-telemetry/opentelemetry-go

🤖 Your project is now configured to use otel with the following integrations:
   OpenTelemetry.

ℹ️ You should commit these changes. This can be done using the following commands:
   $ git add go.mod go.sum
   $ git commit -m "chore: enable otel for compile-time instrumentation"
```

Downstream projects are able to customize these prompts as necessary, either by
documenting one or more flags users need to pass to the `otel setup` command, or
by directly wrapping it with their own command that manages these flags on
behalf of the user. The exact API for this flow will be defined separately, as
it is not part of the core experience.

#### Configuration Styles

The compile-time instrumentation tool is designed to allow users introduce the
tool at various steps in the software development lifecycle:

* Coding time &mdash; the configuration is checked into source control with the
  codebase, and allows developers (Dev and DevOps personas) direct control over
  what gets instrumented for a given application;
* Continuous Integration pipeline (CI/CD) &mdash; the configuration is tracked
  as part of the CI/CD pipeline definition, possibly externally to the built
  application's codebase; it is typically maintained by a different group of
  people than the application's maintainers, usually Ops and/or SecOps personas.

To allow for this, the `otel` tool allows managing configuration in several
ways:

1. Using `tool` dependencies (requires `go1.24` or newer) allows the tool to be
   used by the simpler invocation `go tool otel`, and has all instrumentation
   configuration made available by `tool` dependencies, without requiring the
   addition of any new `.go` or `.yml` file;

2. Using a `otel.instrumentation.go` file is an alternate strategy that is
   fully supported by `go1.23`, and allows more direct control over what
   instrumentation is included in the projects' configuration;

3. The `.otel.yml` file allows injecting configuration directly within the
   CI/CD pipeline without persisting any change to the project's source code
   &ndash; but has the disadvantage of making hermetic or reproducible builds
   more difficult (the `go.mod` and `go.sum` files ought to be considered as
   build artifacts, as they will be modified at the start of the build and are
   needed to correctly reproduce a build in the future).

### Building Applications

Once the configuration has been created, either by the command-line assistant,
or directly by the user (possibly through automated processes), the tool can be
used directly to build, run, and test go applications directly:

1. If the tool is installed as a Go `tool` dependency (`go1.24` and newer):

   ```console
   $ go tool otel go build -o bin/app .
   $ go tool otel go test -shuffle=on ./...
   ```

2. Installing `otel` in `$GOBIN`

   ```console
   $ go install github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/otel
   $ otel go build -o bin/app
   $ otel go test -shuffle=on ./...
   ```

3. Running `otel` with `go run`:

   ```console
   $ go run github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/otel go build -o bin/app
   $ go run github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/otel go test -shuffle=on ./...
   ```


### Ongoing Maintenance

Since the tool and configuration are registered in the `go.mod` file, users are
able to keep these dependencies up-to-date using the standard tools and
processes they use for any other dependency.

They are also able to modify what configuration gets applied by changing the
configuration according to their set up style, either:

- directly in the `go.mod` file by adding or removing `tool` dependencies,
- in the `otel.instrumentation.go` file by adding or removing `import`
  declarations.

### Custom Configuration

Users may wish to add their own, application-specific automatic instrumentation
configuration. This is achieved by adding a `.otel.yml` file in the same
directory as the application's `go.mod` file. Such instructions will be used
only when building packages in this module's context (and not when the module's
packages are dependencies of an automatically-instrumented application).

The contents of such configuration files uses the same schema as the
[instrumentation packages](#instrumentation-packages) definitions file.

### Clean-Room Usage

Some users want to be able to apply compile-time instrumentation to a codebase
without many _any_ modification to it.

To support these users, the `otel` tool can be used without making any
persistent modifications to the codebase: when using `otel go build` without
having created any configuration, the tool will automatically analyze the
project's dependency tree to self-configure and build the project, before
cleaning up any file system changes that may have needed to be made in the
process.

The self-configuration can be influenced by passing any and all relevant build
flags to the `otel` command as part of the build.

It is important to note that this mode of operation does not produce
reproducible builds. Users who want or need fully reproducible builds must use
the explicit set-up procedure.

### Uninstalling

Removing auto-instrumentation configuration is as simple as removing the related
tool dependencies from `go.mod` and removing the `otel.instrumentation.go`
file.

## Instrumentation Packages

A majority of users of the OpenTelemetry compile-time instrumentation tool will
rely on instrumentation packages to instrument their application. These are
standard Go packages that are part of a Go module and contain either (or both):

- a `otel.instrumentation.yml` file that declares all instrumentation
  configuration that is vended by this package (using the schema defined in the
  next section);
- a `otel.instrumentation.go` file that imports at least one valid
  instrumentation package:
  ```go
  //go:build tools
  package tools

  import (
  	_ "github.com/open-telemetry/opentelemetry-go/db"
  	_ "github.com/open-telemetry/opentelemetry-go/http"
  )
  ```

### Schema

The schema of the `otel.instrumentation.yml` file (as well as `.otel.yml`
files, which are used by project-specific instrumentation settings) is described
by the following document:

```yml
%YAML 1.2
---
# yaml-language-server: $schema=https://go-otel.opentelemetry.io/schemas/instrumentation

meta: # An optional block of metadata about the configuration file
   description: # Optional
      |-
         A description of what this configuration does, intended to inform
         end-users about this instrumentation package.
   caveats: # Optional
      - |-
            An array of strings detailing caveats from using this
            instrumentation package. These may be presented to the users when
            they install this package for the first time.

instrumentation: # Required with at least 1 item
   foo: # A unique identifier for this instrumentation item within this file
      description: #Optional
         |-
            A description of this instrumentation configuration, intended for
            end-users.
      pointcut: # Required
         # The definition of a pointcut, which selects which AST nodes are
         # targeted by this instrumentation configuration item.
         ...
      advice: # Required with at least 1 item
         # Transformations to be applied on all of the AST nodes that were
         # selected by the associated pointcut.
         - ...
   # etc...
```

> [!NOTE]
> The terms _Pointcut_ and _Advice_ are borrowed from [Aspect-oriented
> Programming (AoP)][aop], which is a programming paradigm that aims to increase
> modularity by allowing the separation of [cross-cutting
> concerns][x-cutting-concerns] &mdash; aspects of a program that addect several
> modules without the possibility of being encapsulated by any of them.
>
> This appears to be a relatively good description of what compile-time
> instrumentation is set to achieve.
>
> [aop]: https://en.wikipedia.org/wiki/Aspect-oriented_programming
> [x-cutting-concerns]: https://en.wikipedia.org/wiki/Cross-cutting_concern

#### Pointcuts & Advice

##### Required Tool Version

Supported _pointcuts_ and _advice_ types are dependent on the version of the
tool used to apply the configuration. Instrumentation packages can declare the
minimum required version of the `otel` tool by including it in their `go.mod`
files; for example by including a blank import of
`github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/sdk`
to their `otel.instrumentation.go` file. The Go toolchain's Minimum Version
Selection algorithm will then ensure the version requirement is satisfied for
any user.

##### Examples

For example, an instrumentation configuration can be the following:

```yml
pointcut:
   all-of:
      - not: # Prevent injecting into the package itself
            import-path: fully.qualified.package.name
      - function-call: fully.qualified.package.name.FunctionName
advice:
   - before: qualified.instrumentation.package.BeforeFunctionName
   - after: qualified.instrumentation.package.AfterFunctionName
```

### Configuration Re-use

Instrumentation packages can re-use configuration defined in other packages by
containing a `otel.instrumentation.go` file, which contains `import` directives
for each of the instrumentation packages it re-uses:

```go
// Within github.com/open-telemetry/open-telemetry-go/otel/all

package all

import (
   _ "github.com/open-telemetry/opentelemetry-go/otel/instrumentation/net-http"
   _ "github.com/open-telemetry/opentelemetry-go/otel/instrumentation/database-sql"
   // ...
)
```

Using a `.go` file with `import` declarations allows to make sure the surrounding
module's `go.mod` file accurately accounts for all included instrumentation
packages without involving any additional bookkeeping.

The `otel.instrumentation.go` file may contain additional code, as well as
imports to packages that are not instrumentation packages.
