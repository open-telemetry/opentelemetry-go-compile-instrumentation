// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/util"
)

func buildWithToolexec(args []string) error {
	// go build -toolexec=otel ...
	for i, arg := range args {
		if arg == "build" {
			before := args[:i]
			after := args[i+1:]
			insert := []string{"-toolexec=otel"}
			combined := append(before, insert...)
			combined = append(combined, after...)
			args = combined
			break
		}
	}
	return util.RunCmd(args...)
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	buildTemp := ".otel-build"
	err := os.MkdirAll(buildTemp, 0755)
	if err != nil {
		panic(fmt.Sprintf("failed to create build temp directory %s: %v",
			buildTemp, err))
	}

	action := os.Args[1]
	switch action {
	case "setup":
		// otel setup - This command is used to set up the environment for
		// 			    instrumentation. It should be run before other commands.
		err = setup.Setup(logger)
		if err != nil {
			panic("failed to setup: " + err.Error())
		}
	case "go":
		// otel go build - Invoke the go command with toolexec mode. If the setup
		// 				   is not done, it will run the setup command first.
		err = setup.Setup(logger)
		if err != nil {
			panic("failed to setup: " + err.Error())
		}
		err = buildWithToolexec(os.Args[2:])
		if err != nil {
			panic("failed to build with toolexec" + err.Error())
		}
	default:
		// in -toolexec - This should not be used directly, but rather
		// 				   invoked by the go command with toolexec mode.
		args := os.Args[1:]
		err = instrument.Toolexec(logger, args)
		if err != nil {
			panic("failed to instrument: " + err.Error())
		}
	}
}
