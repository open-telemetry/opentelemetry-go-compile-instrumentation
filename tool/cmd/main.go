// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	ActionSetup      = "setup"
	ActionGo         = "go"
	ActionIntoolexec = "toolexec"
)

func buildWithToolexec(logger *slog.Logger, args []string) error {
	// Add -toolexec=otel to the original build command and run it
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	insert := "-toolexec=" + execPath
	newArgs := make([]string, 0, len(args)+1) // Avoid in-place modification
	newArgs = append(newArgs, args[:2]...)    // Add "go build"
	newArgs = append(newArgs, insert)         // Add "-toolexec=..."
	newArgs = append(newArgs, args[2:]...)    // Add the rest
	logger.Info("Running go build with toolexec", "args", newArgs)
	err = util.RunCmd(newArgs...)
	if err != nil {
		return fmt.Errorf("failed to run command: %w", err)
	}
	return nil
}

func initLogger(phase string) (*slog.Logger, error) {
	var writer io.Writer
	switch phase {
	case ActionSetup, ActionGo:
		// Create .otel-build dir
		if _, err := os.Stat(util.BuildTempDir); os.IsNotExist(err) {
			err = os.MkdirAll(util.BuildTempDir, 0o755)
			if err != nil {
				return nil, fmt.Errorf("failed to create .otel-build dir: %w", err)
			}
		}
		// Configure slog to write to the debug.log file
		pwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
		logFile, err := os.OpenFile(filepath.Join(pwd, util.GetBuildTemp("debug.log")),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o777)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		writer = logFile
	case ActionIntoolexec:
		writer = os.Stdout
	default:
		return nil, fmt.Errorf("invalid action: %s", phase)
	}

	// Create a custom handler with shorter time format
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.Format("06/1/2 15:04:05"))
				}
			}
			return a
		},
	})
	logger := slog.New(handler)
	return logger, nil
}

func main() {
	if len(os.Args) < 2 { //nolint:mnd // number of args
		fmt.Println("Usage: otel <action> <args...>")
		os.Exit(1)
	}
	action := os.Args[1]
	switch action {
	case ActionSetup:
		// otel setup - This command is used to set up the environment for
		// 			    instrumentation. It should be run before other commands.
		logger, err := initLogger(ActionSetup)
		if err != nil {
			panic("failed to initialize logger: " + err.Error())
		}

		err = setup.Setup(logger)
		if err != nil {
			panic("failed to setup: " + err.Error())
		}
	case ActionGo:
		// otel go build - Invoke the go command with toolexec mode. If the setup
		// 				   is not done, it will run the setup command first.
		logger, err := initLogger(ActionGo)
		if err != nil {
			panic("failed to initialize logger: " + err.Error())
		}

		err = setup.Setup(logger)
		if err != nil {
			panic("failed to setup: " + err.Error())
		}
		err = buildWithToolexec(logger, os.Args[1:])
		if err != nil {
			panic("failed to build with toolexec: " + err.Error())
		}
	default:
		// in -toolexec - This should not be used directly, but rather
		// 				   invoked by the go command with toolexec mode.
		logger, err := initLogger(ActionIntoolexec)
		if err != nil {
			panic("failed to initialize logger: " + err.Error())
		}

		err = instrument.Toolexec(logger, os.Args[1:])
		if err != nil {
			panic("failed to instrument: " + err.Error())
		}
	}
}
