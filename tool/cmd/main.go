// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/urfave/cli/v2"
)

const (
	exitCodeFailure    = -1
	exitCodeUsageError = 2
)

func main() {
	app := cli.App{
		Name:        "otel",
		Usage:       "OpenTelemetry Go Compile-Time instrumentation tool",
		HideVersion: true,
		Flags: []cli.Flag{
			&cli.PathFlag{
				Name:      "work-dir",
				Aliases:   []string{"w"},
				Usage:     "The path to a directory where working files will be written",
				TakesFile: false,
				Value:     filepath.Join(".", util.BuildTempDir),
			},
		},
		Commands: []*cli.Command{
			commandSetup,
			commandGo,
			commandToolexec,
			commandVersion,
		},
		Before: initLogger,
	}

	if err := app.Run(os.Args); err != nil {
		panic(err)
	}
}

func initLogger(cCtx *cli.Context) error {
	buildTempDir := cCtx.Path("work-dir")
	if err := os.MkdirAll(buildTempDir, 0o755); err != nil {
		return ex.Errorf(err, "failed to create work directory %q", buildTempDir)
	}

	writer, err := os.OpenFile(buildTempDir, os.O_APPEND|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return ex.Errorf(err, "failed to open log file %q", buildTempDir)
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
	cCtx.Context = util.ContextWithLogger(cCtx.Context, logger)

	return nil
}
