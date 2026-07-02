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
	runWithFlags := func(t *testing.T, workDir string, debug bool) (context.Context, string) {
		t.Helper()

		ctxCh := make(chan context.Context, 1)
		app := &cli.Command{
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "work-dir", Value: util.GetOtelcWorkDir()},
				&cli.BoolFlag{Name: "debug", Sources: cli.EnvVars(util.EnvOtelcDebug)},
			},
			Before: initLogger,
			Action: func(ctx context.Context, cmd *cli.Command) error {
				ctxCh <- ctx
				return nil
			},
			After: func(ctx context.Context, cmd *cli.Command) error {
				return closeLogger(ctx)
			},
		}

		args := []string{"otelc"}
		if workDir != "" {
			args = append(args, "--work-dir", workDir)
		}
		if debug {
			args = append(args, "--debug")
		}
		if err := app.Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}

		gotCtx := <-ctxCh
		gotWorkDir := os.Getenv(util.EnvOtelcWorkDir)
		logPath := filepath.Join(gotWorkDir, util.BuildTempDir, debugLogFilename)
		if _, err := os.Stat(logPath); err != nil {
			t.Fatalf("expected log file at %s: %v", logPath, err)
		}

		return gotCtx, gotWorkDir
	}

	t.Run("default level is info", func(t *testing.T) {
		t.Setenv(util.EnvOtelcDebug, "")
		t.Setenv(util.EnvOtelcWorkDir, "")
		workDir := t.TempDir()

		ctx, gotWorkDir := runWithFlags(t, workDir, false)
		if gotWorkDir != workDir {
			t.Fatalf("expected %s=%q, got %q", util.EnvOtelcWorkDir, workDir, gotWorkDir)
		}

		logger := util.LoggerFromContext(ctx)
		if logger.Enabled(context.Background(), slog.LevelDebug) {
			t.Error("expected debug logging to be disabled")
		}
	})

	t.Run("debug flag enables debug level", func(t *testing.T) {
		t.Setenv(util.EnvOtelcDebug, "")
		t.Setenv(util.EnvOtelcWorkDir, "")
		workDir := t.TempDir()

		ctx, gotWorkDir := runWithFlags(t, workDir, true)
		if gotWorkDir != workDir {
			t.Fatalf("expected %s=%q, got %q", util.EnvOtelcWorkDir, workDir, gotWorkDir)
		}

		logger := util.LoggerFromContext(ctx)
		if !logger.Enabled(context.Background(), slog.LevelDebug) {
			t.Error("expected debug logging to be enabled")
		}
	})

	t.Run("debug flag sets env for subprocess propagation", func(t *testing.T) {
		t.Setenv(util.EnvOtelcDebug, "")
		t.Setenv(util.EnvOtelcWorkDir, "")
		workDir := t.TempDir()

		_, gotWorkDir := runWithFlags(t, workDir, true)
		if gotWorkDir != workDir {
			t.Fatalf("expected %s=%q, got %q", util.EnvOtelcWorkDir, workDir, gotWorkDir)
		}
		if got := os.Getenv(util.EnvOtelcDebug); got != "1" {
			t.Errorf("expected %s=1, got %q", util.EnvOtelcDebug, got)
		}
	})

	t.Run("env var enables debug without flag", func(t *testing.T) {
		t.Setenv(util.EnvOtelcDebug, "1")
		t.Setenv(util.EnvOtelcWorkDir, "")
		workDir := t.TempDir()

		ctx, gotWorkDir := runWithFlags(t, workDir, false)
		if gotWorkDir != workDir {
			t.Fatalf("expected %s=%q, got %q", util.EnvOtelcWorkDir, workDir, gotWorkDir)
		}

		logger := util.LoggerFromContext(ctx)
		if !logger.Enabled(context.Background(), slog.LevelDebug) {
			t.Error("expected debug logging to be enabled via env var")
		}
	})

	t.Run("default work dir uses cwd as workspace root", func(t *testing.T) {
		t.Setenv(util.EnvOtelcDebug, "")
		t.Setenv(util.EnvOtelcWorkDir, "")
		workDir := t.TempDir()
		t.Chdir(workDir)

		_, gotWorkDir := runWithFlags(t, "", false)
		if gotWorkDir != workDir {
			t.Fatalf("expected %s=%q, got %q", util.EnvOtelcWorkDir, workDir, gotWorkDir)
		}
	})
}

func TestCloseLoggerNoWriter(t *testing.T) {
	// When initLogger never ran (e.g. it failed early), the context holds no log
	// writer and closeLogger must be a no-op rather than panic.
	if err := closeLogger(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
