// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/urfave/cli/v3"
)

const (
	exitCodeFailure = -1

	debugLogFilename = "debug.log"
)

func main() {
	app := cli.Command{
		Name:        "otel",
		Usage:       "OpenTelemetry Go Compile-Time Instrumentation Tool",
		HideVersion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:      "work-dir",
				Aliases:   []string{"w"},
				Usage:     "The path to a directory where working files will be written",
				TakesFile: true,
				Value:     filepath.Join(".", util.BuildTempDir),
				Sources:   cli.NewValueSourceChain(cli.EnvVar(util.EnvOtelWorkDir)),
			},
		},
		Commands: []*cli.Command{
			&commandSetup,
			&commandGo,
			&commandToolexec,
			&commandVersion,
		},
		Before: initLogger,
	}

	err := app.Run(context.Background(), os.Args)
	if err != nil {
		ex.Fatal(err)
	}
}

func initLogger(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	buildTempDir := cmd.String("work-dir")
	err := os.MkdirAll(buildTempDir, 0o755)
	if err != nil {
		return ctx, ex.Errorf(err, "failed to create work directory %q", buildTempDir)
	}

	logFilename := filepath.Join(buildTempDir, debugLogFilename)
	writer, err := os.OpenFile(logFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return ctx, ex.Errorf(err, "failed to open log file %q", buildTempDir)
	}

	// Create a custom handler with shorter time format
	// Remove time and level keys as they make no sense for debugging
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)
	ctx = util.ContextWithLogger(ctx, logger)

	return ctx, nil
}

func addLoggerPhaseAttribute(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	logger := util.LoggerFromContext(ctx)
	logger = logger.With("phase", cmd.Name)
	return util.ContextWithLogger(ctx, logger), nil
}
