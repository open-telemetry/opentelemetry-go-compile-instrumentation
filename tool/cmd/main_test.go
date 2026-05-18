// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/urfave/cli/v3"
)

func TestInitLogger(t *testing.T) {
	runWithFlags := func(t *testing.T, debug bool) context.Context {
		t.Helper()
		tmpDir, mkErr := os.MkdirTemp( //nolint:usetesting // open log file handle prevents cleanup
			"",
			"otelc-logger-test-*",
		)
		if mkErr != nil {
			t.Fatal(mkErr)
		}
		t.Cleanup(func() { os.RemoveAll(tmpDir) })

		ctxCh := make(chan context.Context, 1)
		app := &cli.Command{
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "work-dir", Value: tmpDir},
				&cli.BoolFlag{Name: "debug", Sources: cli.EnvVars(util.EnvOtelcDebug)},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				ctx, err := initLogger(ctx, cmd)
				if err != nil {
					return err
				}
				ctxCh <- ctx
				return nil
			},
		}

		args := []string{"otelc"}
		if debug {
			args = append(args, "--debug")
		}
		if err := app.Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}

		gotCtx := <-ctxCh

		logPath := filepath.Join(tmpDir, debugLogFilename)
		if _, err := os.Stat(logPath); err != nil {
			t.Fatalf("expected log file at %s: %v", logPath, err)
		}

		return gotCtx
	}

	t.Run("default level is info", func(t *testing.T) {
		t.Setenv(util.EnvOtelcDebug, "")
		ctx := runWithFlags(t, false)
		logger := util.LoggerFromContext(ctx)
		if logger.Enabled(context.Background(), slog.LevelDebug) {
			t.Error("expected debug logging to be disabled")
		}
	})

	t.Run("debug flag enables debug level", func(t *testing.T) {
		t.Setenv(util.EnvOtelcDebug, "")
		ctx := runWithFlags(t, true)
		logger := util.LoggerFromContext(ctx)
		if !logger.Enabled(context.Background(), slog.LevelDebug) {
			t.Error("expected debug logging to be enabled")
		}
	})

	t.Run("debug flag sets env for subprocess propagation", func(t *testing.T) {
		t.Setenv(util.EnvOtelcDebug, "")
		_ = runWithFlags(t, true)
		if got := os.Getenv(util.EnvOtelcDebug); got != "1" {
			t.Errorf("expected %s=1, got %q", util.EnvOtelcDebug, got)
		}
	})

	t.Run("env var enables debug without flag", func(t *testing.T) {
		t.Setenv(util.EnvOtelcDebug, "1")
		ctx := runWithFlags(t, false)
		logger := util.LoggerFromContext(ctx)
		if !logger.Enabled(context.Background(), slog.LevelDebug) {
			t.Error("expected debug logging to be enabled via env var")
		}
	})
}
