// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	ActionSetup      = "setup"
	ActionGo         = "go"
	ActionIntoolexec = "toolexec"
	ActionVersion    = "version"
	DebugLog         = "debug.log"
)

func cleanBuildTemp() {
	_ = os.RemoveAll(setup.OtelRuntimeFile)
}

func initLogger(phase string) (*slog.Logger, error) {
	var writer io.Writer
	switch phase {
	case ActionSetup, ActionGo:
		// Create .otel-build dir
		_, err := os.Stat(util.BuildTempDir)
		if os.IsNotExist(err) {
			err = os.MkdirAll(util.BuildTempDir, 0o755)
			if err != nil {
				return nil, ex.Errorf(err, "failed to create .otel-build dir")
			}
		}
		// Configure slog to write to the debug.log file
		path := util.GetBuildTemp(DebugLog)
		logFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o777)
		if err != nil {
			return nil, ex.Errorf(err, "failed to open log file")
		}
		writer = logFile
	case ActionIntoolexec:
		path := util.GetBuildTemp(DebugLog)
		logFile, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o777)
		if err != nil {
			return nil, ex.Errorf(err, "failed to open log file")
		}
		writer = logFile
	default:
		return nil, ex.Errorf(nil, "invalid action: %s", phase)
	}

	// Create a custom handler with shorter time format
	// Remove time and level keys as they make no sense for debugging
	var handler slog.Handler
	handler = slog.NewTextHandler(writer, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
		Level: slog.LevelInfo,
	})
	handler = handler.WithAttrs([]slog.Attr{
		{
			Key:   "phase",
			Value: slog.StringValue(phase),
		},
	})
	logger := slog.New(handler)
	return logger, nil
}

func main() {
	if len(os.Args) < 2 { //nolint:mnd // number of args
		println("Usage: otel <action> <args...>")
		println("Actions:")
		println("  setup    Set up the environment for instrumentation.")
		println("  go       Invoke the go command with toolexec mode.")
		println("  version  Print the version of the tool.")
		os.Exit(1)
	}

	action := os.Args[1]
	switch action {
	case ActionVersion:
		_, _ = fmt.Printf("otel version %s_%s_%s\n", Version, CommitHash, BuildTime)
	case ActionSetup:
		// otel setup - This command is used to set up the environment for
		// 			    instrumentation. It should be run before other commands.
		logger, err := initLogger(ActionSetup)
		if err != nil {
			ex.Fatal(err)
		}

		err = setup.Setup(logger)
		if err != nil {
			ex.Fatal(err)
		}
	case ActionGo:
		// otel go build - Invoke the go command with toolexec mode. If the setup
		// 				   is not done, it will run the setup command first.
		defer cleanBuildTemp()
		backup := []string{"go.mod", "go.sum", "go.work", "go.work.sum"}
		util.BackupFile(backup)
		defer util.RestoreFile(backup)

		logger, err := initLogger(ActionGo)
		if err != nil {
			ex.Fatal(err)
		}
		err = setup.Setup(logger)
		if err != nil {
			ex.Fatal(err)
		}
		err = setup.BuildWithToolexec(logger, os.Args[1:])
		if err != nil {
			ex.Fatal(err)
		}
	case ActionIntoolexec:
		ex.Fatalf("It should not be used directly")
	default:
		// in -toolexec - This should not be used directly, but rather
		// 				   invoked by the go command with toolexec mode.
		logger, err := initLogger(ActionIntoolexec)
		if err != nil {
			ex.Fatal(err)
		}

		err = instrument.Toolexec(logger, os.Args[1:])
		if err != nil {
			ex.Fatal(err)
		}
	}
}
