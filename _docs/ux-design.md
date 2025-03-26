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
- Very low effort required for holistic instrumentation
- Ability to instrument within third-party dependencies
- Keeping the codebase completely de-coupled from instrumentation

## Target Audience

Compile-time instrumentation, like other techniques of automatic
instrumentation, does not affort users the same amount of control over their
instrumentation as manual instrumentation; it trades very granual control for
significantly reduced implementation effort. As a result, compile-time
instrumentation may not appeal to developers who have very specific requirements
on what their instrumentation produces.

The primary audience for the OpenTelementry Go compile-time instrumentation tool
is composed of the following personas:

- Application developers looking for a no-frills, turney instrumentation
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

- `github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/gotel`,
  a command-line tool that can be installed using `go install` or using
  `go get -tool`;
- `github.com/open-telemetry/opentelemetry-go-compile-instrumentation/runtime`,
  a small, self-contained package that contains essential runtime functionality
  used by instrumented applications (not intended for manual usage).

### Getting Started

The tool offers a wizard for getting started with the tool automatically by
following a series of prompts:

```console
$ go run github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/gotel@latest setup
╭──────────────────────────────────────────────────────────────────────╮
│                                                                      │
│           OpenTelemetry compile-time instrumentation tool            │
│                    v1.2.3 (go1.24.1 darwin/arm64)                    │
│                                                                      │
╰──────────────────────────────────────────────────────────────────────╯
✨ This setup assistant will guide you through the steps needed to properly
   configure gotel for the github.com/example/demo application. After you've
   answered all the prompts, it will present you with the list of actions it
   will execute; and you will have an choice to apply those or not.
🤖 Press enter to continue...

ℹ️ Registering gotel as a tool dependency is recommended: it allows you to
   manage the dependency on gotel like any other dependency of your application,
   via the go.mod file. When using go 1.24 or newer, you can use `go tool gotel`
   to use the correct version of the tool.
   Not registering a go tool dependency allows instrumenting applications
   without modifying their codebase at all (not even the `go.mod` file); which
   may be preferred for building third-party applications or integrating in the
   CI/CD pipeline. The reproductibility of builds is no longer guaranteed by the
   go toolchain, and the application may be built with newer versions of
   dependencies than those in the `go.mod` file if the enabled instrumentation
   package(s) requires it.
🤖 Should I add gotel as a tool dependency?
   [Yes]  No
🆗 I will add a tool dependency

ℹ️ You may enable one or more instrumentation packages for your application.
   Most users need to choose only one. This can be changed at any time.
🤖 What instrumentation do you want to enable for this project? (Select one or
   more using space, then press enter to confirm your selection)
   [X] OpenTelemetry (github.com/open-telemetry/opentelemetry-go)
   [ ] Datadog (github.com/DataDog/dd-trace-go/v2)
   [ ] Other
🆗 I will configure the following instrumentation: OpenTelemetry

ℹ️ Using go tool dependencies or a `gotel.instrumentation.go` file to configure
   integrations is recommended as it ensures the instrumentation packages are
   represented in your `go.mod` file, making builds reproductible.
   Using a `.gotel.yml` file is useful when instrumenting applications without
   modifying their codebase at all; which may be preferrable when building
   third-party applications or integrating in the CI/CD pipeline. The
   reproductibility of builds is no longer guaranteed by the go toolchain, and
   the application may be built with newer versions of dependencies than those
   in the `go.mod` file if the enabled instrumentation package(s) require it.
🤖 How do you want to configure instrumentation for this project?
   (*) Using go tool dependencies (Recommended)
   ( ) Using the `gotel.instrumentation.go` file
   ( ) Using a `.gotel.yml` file
🆗 I will use go tool dependencies to enable integration packages.

🤖 We're all set! Based on your answers, I will execute the following commands:
   $ go get -tool github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/gotel@v1.2.3
   $ go get -tool github.com/open-telemetry/opentelemetry-go
🤖 Should I proceed?
   [Yes]  No

🆗 Let's go!
✅ go get -tool github.com/open-telemetry/opentelemetry-go-compile-instrumentation/cmd/gotel@v1.2.3
✅ go get -tool github.com/open-telemetry/opentelemetry-go

🤖 Your project is now configured to use gotel with the following integrations:
   OpenTelemetry.

ℹ️ You should commit these changes. This can be done using the following commands:
   $ git add go.mod go.sum
   $ git commit -m "chore: enable gotel for compile-time instrumentation"
```

