// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/profile"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	debugLogFilename = "debug.log"
)

func main() {
	app := cli.Command{
		Name:        "otelc",
		Usage:       "OpenTelemetry Go Compile-Time Instrumentation Tool",
		HideVersion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:      "work-dir",
				Aliases:   []string{"w"},
				Usage:     "The path to a directory where working files will be written",
				TakesFile: true,
				Value:     util.GetBuildTempDir(),
			},
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "Enable debug mode",
				Value:   false,
			},
			&cli.StringFlag{
				Name:      "rules",
				Aliases:   []string{"rules"},
				Usage:     "The path to the rules configuration file",
				TakesFile: true,
				Value:     "",
			},
			&cli.StringFlag{
				Name:    "profile-path",
				Sources: cli.EnvVars(profile.EnvProfilePath),
				Usage:   "Directory for profiling output",
				Hidden:  true,
			},
			&cli.StringSliceFlag{
				Name:    "profile",
				Sources: cli.EnvVars(profile.EnvEnabledProfiles),
				Usage:   "Enable profiling: cpu, heap, trace (repeatable)",
				Hidden:  true,
			},
			&cli.BoolFlag{
				Name:    "profile-summary",
				Sources: cli.EnvVars("OTELC_PROFILE_SUMMARY"),
				Usage:   "Merge profile files into one per type after build completes",
				Hidden:  true,
			},
			&cli.BoolFlag{
				Name:    "stats",
				Sources: cli.EnvVars(util.EnvOtelcStats),
				Usage:   "Log per-tool wall-clock duration for toolexec commands",
				Hidden:  true,
			},
		},
		Commands: []*cli.Command{
			&commandSetup,
			&commandGo,
			&commandCleanup,
			&commandToolexec,
			&commandVersion,
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			ctx, err := initLogger(ctx, cmd)
			if err != nil {
				return ctx, err
			}
			ctx, err = initProfiling(ctx, cmd)
			if err != nil {
				return ctx, err
			}
			return initStats(ctx, cmd)
		},
		After: stopProfiling,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := app.Run(ctx, os.Args)
	if err != nil {
		ex.Fatal(err)
	}
}

func initLogger(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	buildTempDir := cmd.String("work-dir")
	err := os.MkdirAll(buildTempDir, 0o755)
	if err != nil {
		return ctx, ex.Wrapf(err, "failed to create work directory %q", buildTempDir)
	}

	logFilename := filepath.Join(buildTempDir, debugLogFilename)
	writer, err := os.OpenFile(logFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return ctx, ex.Wrapf(err, "failed to open log file %q", logFilename)
	}

	// Log timestamps and levels are omitted: they add noise when correlating
	// with Go toolchain output and the log file is for human debugging only.
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
