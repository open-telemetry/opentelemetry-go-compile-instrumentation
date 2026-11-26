// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/util"
)

func newTestLazyLogger(t *testing.T) (*slog.Logger, *lazyLogFile, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), util.DebugLogFilename)
	lazy := &lazyLogFile{path: path}
	t.Cleanup(func() { _ = lazy.Close() })
	handler := slog.NewTextHandler(lazy, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler), lazy, path
}

func readLogFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	return string(content)
}

func runTestLoggerCommand(t *testing.T, workDir, subcommand string, args ...string) {
	t.Helper()
	app := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:      flagWorkDir,
				Aliases:   []string{"w"},
				Usage:     "The path to a directory where working files will be written",
				TakesFile: true,
				Value:     util.GetOtelcWorkDir(),
			},
			&cli.BoolFlag{Name: "debug", Sources: cli.EnvVars(util.EnvOtelcDebug)},
		},
		Before: initLogger,
		Commands: []*cli.Command{{
			Name:            subcommand,
			SkipFlagParsing: true,
			Action: func(ctx context.Context, _ *cli.Command) error {
				return closeLogger(ctx)
			},
		}},
	}

	cmdArgs := append([]string{"otelc", "--" + flagWorkDir, workDir, subcommand}, args...)
	if err := app.Run(context.Background(), cmdArgs); err != nil {
		t.Fatal(err)
	}
}

func TestLazyLogFileDoesNotOpenForDisabledLevels(t *testing.T) {
	logger, lazy, path := newTestLazyLogger(t)

	logger.Debug("only a debug record")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no %s after a disabled record, stat err: %v", util.DebugLogFilename, err)
	}

	if err := lazy.Close(); err != nil {
		t.Fatalf("closing a never-opened log file: %v", err)
	}
}

func TestLazyLogFileOpensOnEnabledRecordAndCloses(t *testing.T) {
	logger, lazy, path := newTestLazyLogger(t)

	logger.Info("instrumenting package")

	content := readLogFile(t, path)
	if !strings.Contains(content, "msg=\"instrumenting package\"") {
		t.Fatalf("expected the info record in %s, got:\n%s", util.DebugLogFilename, content)
	}

	if err := lazy.Close(); err != nil {
		t.Fatalf("closing the log file: %v", err)
	}
	if err := lazy.Close(); err != nil {
		t.Fatalf("closing the log file twice: %v", err)
	}
}

func TestLazyLogFileConcurrentRecordsShareFile(t *testing.T) {
	logger, _, path := newTestLazyLogger(t)

	const goroutines = 8
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			logger.Info("concurrent record")
		})
	}
	wg.Wait()

	content := readLogFile(t, path)
	if got := strings.Count(content, "concurrent record"); got != goroutines {
		t.Fatalf("expected %d records in %s, got %d:\n%s", goroutines, util.DebugLogFilename, got, content)
	}
}

func TestLazyLogFileOpenErrorReachesCaller(t *testing.T) {
	path := filepath.Join(t.TempDir(), util.DebugLogFilename)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	lazy := &lazyLogFile{path: path}
	handler := slog.NewTextHandler(lazy, &slog.HandlerOptions{Level: slog.LevelInfo})

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "record", 0)
	if err := handler.Handle(context.Background(), record); err == nil {
		t.Fatal("expected the open error to surface from Handle")
	}
}

func TestAppendHandlesDoNotOverwriteEachOther(t *testing.T) {
	path := filepath.Join(t.TempDir(), util.DebugLogFilename)
	h1, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer h1.Close()

	h2, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()

	if _, err = h1.WriteString("A"); err != nil {
		t.Fatal(err)
	}
	if _, err = h2.WriteString("BBBB"); err != nil {
		t.Fatal(err)
	}
	if _, err = h1.WriteString("C"); err != nil {
		t.Fatal(err)
	}

	got := readLogFile(t, path)
	if got != "ABBBBC" {
		t.Fatalf("expected %q, got %q", "ABBBBC", got)
	}
}

func TestInitLoggerAppendsDebugLogForTopLevelCommands(t *testing.T) {
	t.Setenv(util.EnvOtelcDebug, "")
	t.Setenv(util.EnvOtelcWorkDir, "")
	workDir := t.TempDir()
	buildDir := filepath.Join(workDir, util.BuildTempDir)
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(buildDir, util.DebugLogFilename)
	stale := "stale content from a previous build\n"
	if err := os.WriteFile(logPath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	runTestLoggerCommand(t, workDir, "go", "build", ".")

	content := readLogFile(t, logPath)
	if !strings.Contains(content, "stale content from a previous build") {
		t.Fatalf("expected initLogger to append to %s before build lock, got:\n%s", util.DebugLogFilename, content)
	}
}

func TestInitLoggerToolexecDoesNotCreateDebugLogWithoutRecords(t *testing.T) {
	t.Setenv(util.EnvOtelcDebug, "")
	t.Setenv(util.EnvOtelcWorkDir, "")
	workDir := t.TempDir()

	runTestLoggerCommand(t, workDir, "toolexec", "true")

	logPath := filepath.Join(workDir, util.BuildTempDir, util.DebugLogFilename)
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("expected no %s from a record-less toolexec run, stat err: %v", util.DebugLogFilename, err)
	}
}
