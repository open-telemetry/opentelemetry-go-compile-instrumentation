// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestInitLogger(t *testing.T) {
	runWithFlags := func(t *testing.T, workDir string, debug bool) (context.Context, string) {
		t.Helper()

		ctxCh := make(chan context.Context, 1)
		app := &cli.Command{
			Flags: []cli.Flag{
				newWorkDirFlag(),
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
			args = append(args, "--"+flagWorkDir, workDir)
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

func TestInitLoggerToolexecWorkDir(t *testing.T) {
	runToolexec := func(t *testing.T, extraArgs ...string) error {
		t.Helper()
		app := &cli.Command{
			Flags: []cli.Flag{
				newWorkDirFlag(),
			},
			Before: initLogger,
			Commands: []*cli.Command{{
				Name:            "toolexec",
				SkipFlagParsing: true,
				Action: func(ctx context.Context, _ *cli.Command) error {
					return closeLogger(ctx)
				},
			}},
		}
		args := append([]string{"otelc"}, extraArgs...)
		args = append(args, "toolexec")
		return app.Run(context.Background(), args)
	}

	t.Run("discovers setup work dir when flag is unset", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, "")
		module := t.TempDir()
		if err := os.MkdirAll(filepath.Join(module, util.BuildTempDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Chdir(module)

		if err := runToolexec(t); err != nil {
			t.Fatal(err)
		}
		if got := os.Getenv(util.EnvOtelcWorkDir); got != module {
			t.Fatalf("expected %s=%q, got %q", util.EnvOtelcWorkDir, module, got)
		}
		logPath := filepath.Join(module, util.BuildTempDir, debugLogFilename)
		if _, err := os.Stat(logPath); err != nil {
			t.Fatalf("expected log file at %s: %v", logPath, err)
		}
	})

	t.Run("skips filesystem setup when none is discovered", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, "")
		module := t.TempDir()
		if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Chdir(module)

		if err := runToolexec(t); err != nil {
			t.Fatal(err)
		}
		if got := os.Getenv(util.EnvOtelcWorkDir); got != "" {
			t.Fatalf("expected %s unset, got %q", util.EnvOtelcWorkDir, got)
		}
		if _, err := os.Stat(filepath.Join(module, util.BuildTempDir)); !os.IsNotExist(err) {
			t.Fatalf("expected no %s directory, stat err: %v", util.BuildTempDir, err)
		}
	})

	t.Run("explicit flag is used without discovery", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, "")
		workDir := t.TempDir()
		cwd := t.TempDir()
		t.Chdir(cwd)

		if err := runToolexec(t, "--"+flagWorkDir, workDir); err != nil {
			t.Fatal(err)
		}
		if got := os.Getenv(util.EnvOtelcWorkDir); got != workDir {
			t.Fatalf("expected %s=%q, got %q", util.EnvOtelcWorkDir, workDir, got)
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

func TestCleanupSubcommand(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, "")
	workDir := t.TempDir()

	// Pre-create the workspace structure that setup would leave behind.
	buildTemp := filepath.Join(workDir, util.BuildTempDir)
	if err := os.MkdirAll(buildTemp, 0o755); err != nil {
		t.Fatal(err)
	}

	app := &cli.Command{
		Flags: []cli.Flag{
			newWorkDirFlag(),
		},
		Before:   initLogger,
		Commands: []*cli.Command{&commandCleanup},
		After: func(ctx context.Context, _ *cli.Command) error {
			return closeLogger(ctx)
		},
	}
	args := []string{"otelc", "--" + flagWorkDir, workDir, "cleanup"}
	if err := app.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(buildTemp); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, stat err: %v", buildTemp, err)
	}
}
