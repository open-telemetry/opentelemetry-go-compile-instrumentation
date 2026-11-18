// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otelc/tool/internal/setup"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestInitLogger(t *testing.T) {
	runWithFlags := func(t *testing.T, workDir string, debug bool) (context.Context, string) {
		t.Helper()

		ctxCh := make(chan context.Context, 1)
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
				&cli.StringFlag{
					Name:      flagWorkDir,
					Aliases:   []string{"w"},
					Usage:     "The path to a directory where working files will be written",
					TakesFile: true,
					Value:     util.GetOtelcWorkDir(),
				},
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

func TestInitGoflags(t *testing.T) {
	tests := []struct {
		name       string
		goflags    string
		expected   string
		jsonOutput bool
	}{
		{name: "unset", goflags: "", expected: ""},
		{name: "nothing to strip", goflags: "-mod=mod", expected: "-mod=mod"},
		{name: "toolexec", goflags: "-mod=mod '-toolexec=otelc toolexec'", expected: "-mod=mod"},
		{name: "json", goflags: "-json -mod=mod", expected: "-mod=mod", jsonOutput: true},
		{name: "json turned off", goflags: "-json=false", expected: ""},
		{
			name:       "toolexec and json",
			goflags:    "-toolexec=otelc -json -race",
			expected:   "-race",
			jsonOutput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOFLAGS", tt.goflags)

			ctx, err := initGoflags(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := os.Getenv("GOFLAGS"); got != tt.expected {
				t.Errorf("GOFLAGS = %q, want %q", got, tt.expected)
			}
			if got := setup.JSONOutputFromContext(ctx); got != tt.jsonOutput {
				t.Errorf("JSONOutputFromContext = %v, want %v", got, tt.jsonOutput)
			}
		})
	}
}

func TestInitRoot(t *testing.T) {
	run := func(t *testing.T, args ...string) (context.Context, error) {
		t.Helper()
		// CI can set the profiling variables for the whole run; keep this
		// test independent of them so it never starts a profile.
		t.Setenv(envProfilePath, "")
		t.Setenv(envEnabledProfiles, "")

		ctxCh := make(chan context.Context, 1)
		app := &cli.Command{
			Name:  "otelc",
			Flags: rootFlags(),
			Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
				ctx, err := initRoot(ctx, cmd)
				if err != nil {
					// After does not run when Before fails. Close the log
					// file here so the temp dir can be removed on Windows.
					_ = closeLogger(ctx)
				}
				return ctx, err
			},
			Action: func(ctx context.Context, _ *cli.Command) error {
				ctxCh <- ctx
				return nil
			},
			After: func(ctx context.Context, _ *cli.Command) error {
				return closeLogger(ctx)
			},
		}
		err := app.Run(t.Context(), append([]string{"otelc", "--" + flagWorkDir, t.TempDir()}, args...))
		if err != nil {
			return nil, err
		}
		return <-ctxCh, nil
	}

	t.Run("strips GOFLAGS and records json output", func(t *testing.T) {
		t.Setenv("GOFLAGS", "-json -mod=mod")
		t.Setenv(util.EnvOtelcWorkDir, "")

		ctx, err := run(t)
		if err != nil {
			t.Fatal(err)
		}
		if got := os.Getenv("GOFLAGS"); got != "-mod=mod" {
			t.Errorf("GOFLAGS = %q, want %q", got, "-mod=mod")
		}
		if !setup.JSONOutputFromContext(ctx) {
			t.Error("JSONOutputFromContext = false, want true")
		}
	})

	t.Run("reports a profiling error", func(t *testing.T) {
		t.Setenv("GOFLAGS", "")
		t.Setenv(util.EnvOtelcWorkDir, "")

		_, err := run(t, "--profile", "cpu")
		if err == nil || !strings.Contains(err.Error(), "--profile-path is required") {
			t.Fatalf("err = %v, want the --profile-path error", err)
		}
	})
}

// TestAppSurfacesDeadlineCause wires initDeadline/deadlineCauseIfExceeded/
// cancelDeadline together the same way main's app does (Before derives the
// context, Action can run past it, After reports the cause), the one path
// deadlineCauseIfExceeded exists for: a build that otelc itself stopped on
// --timeout must report why, not just the generic error its canceled
// context left the build command with.
func TestAppSurfacesDeadlineCause(t *testing.T) {
	t.Setenv(util.EnvOtelcBuildDeadline, "")

	var deadlineCause error
	app := &cli.Command{
		Name:   "otelc",
		Flags:  rootFlags(),
		Before: initDeadline,
		Action: func(ctx context.Context, _ *cli.Command) error {
			<-ctx.Done()
			return errors.New("signal: killed")
		},
		After: func(ctx context.Context, _ *cli.Command) error {
			deadlineCause = deadlineCauseIfExceeded(ctx)
			cancelDeadline(ctx)
			return nil
		},
	}

	err := app.Run(t.Context(), []string{"otelc", "--timeout", "1ms", "go"})

	if err == nil {
		t.Fatal("app.Run() = nil error, want the build's own error")
	}
	if deadlineCause == nil {
		t.Fatal("deadlineCause = nil, want the --timeout cause")
	}
	if !strings.Contains(deadlineCause.Error(), "--timeout") {
		t.Errorf("deadlineCause = %q, want it to name --timeout", deadlineCause.Error())
	}

	// This is the exact call main() makes: a non-nil deadlineCause replaces
	// the build's own generic error, so the user sees why, not
	// "signal: killed".
	finalErr := reportedErr(err, deadlineCause)
	if !errors.Is(finalErr, deadlineCause) {
		t.Errorf("reportedErr() = %v, want the deadline cause", finalErr)
	}
}

// TestAppKeepsContextLiveForTeardown mirrors main()'s exact After ordering:
// capture deadlineCause, defer cancelDeadline, then run teardown (in main,
// stopProfiling and closeLogger). A build that finishes well within its
// --timeout must not have its context already canceled by the time
// teardown runs, since stopProfiling's --profile-summary merge step starts
// a real subprocess with that same ctx.
func TestAppKeepsContextLiveForTeardown(t *testing.T) {
	t.Setenv(util.EnvOtelcBuildDeadline, "")

	var sawCanceledDuringTeardown bool
	app := &cli.Command{
		Name:   "otelc",
		Flags:  rootFlags(),
		Before: initDeadline,
		Action: func(_ context.Context, _ *cli.Command) error {
			return nil // the build finishes immediately, well inside --timeout
		},
		After: func(ctx context.Context, _ *cli.Command) error {
			_ = deadlineCauseIfExceeded(ctx)
			defer cancelDeadline(ctx)
			// Stand-in for stopProfiling: any teardown step that still
			// needs a live ctx (e.g. to start a subprocess) must run here,
			// before the deferred cancelDeadline fires.
			sawCanceledDuringTeardown = ctx.Err() != nil
			return nil
		},
	}

	if err := app.Run(t.Context(), []string{"otelc", "--timeout", "1h", "go"}); err != nil {
		t.Fatalf("app.Run() error = %v", err)
	}
	if sawCanceledDuringTeardown {
		t.Error("ctx was already canceled during teardown; cancelDeadline must run after, not before, it")
	}
}

func TestInitDeadline(t *testing.T) {
	run := func(t *testing.T, args ...string) context.Context {
		t.Helper()
		ctxCh := make(chan context.Context, 1)
		app := &cli.Command{
			Name:   "otelc",
			Flags:  rootFlags(),
			Before: initDeadline,
			Action: func(ctx context.Context, _ *cli.Command) error {
				ctxCh <- ctx
				return nil
			},
		}
		if err := app.Run(t.Context(), append([]string{"otelc"}, args...)); err != nil {
			t.Fatal(err)
		}
		return <-ctxCh
	}

	t.Run("no timeout leaves the context without a deadline", func(t *testing.T) {
		t.Setenv(util.EnvOtelcBuildDeadline, "")

		ctx := run(t, "go")

		if _, ok := ctx.Deadline(); ok {
			t.Error("ctx has a deadline, want none")
		}
		if got := os.Getenv(util.EnvOtelcBuildDeadline); got != "" {
			t.Errorf("%s = %q, want unset", util.EnvOtelcBuildDeadline, got)
		}
	})

	t.Run("a non-go subcommand ignores --timeout", func(t *testing.T) {
		t.Setenv(util.EnvOtelcBuildDeadline, "")

		ctx := run(t, "--timeout", "1h", "version")

		if _, ok := ctx.Deadline(); ok {
			t.Error("ctx has a deadline, want none")
		}
		if got := os.Getenv(util.EnvOtelcBuildDeadline); got != "" {
			t.Errorf("%s = %q, want unset", util.EnvOtelcBuildDeadline, got)
		}
	})

	t.Run("--timeout sets a deadline and publishes it for child processes", func(t *testing.T) {
		t.Setenv(util.EnvOtelcBuildDeadline, "")
		before := time.Now()

		ctx := run(t, "--timeout", "1h", "go")

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("ctx has no deadline, want one")
		}
		if d := deadline.Sub(before); d < 59*time.Minute || d > 61*time.Minute {
			t.Errorf("deadline is %s from now, want ~1h", d)
		}
		published, ok := util.BuildDeadlineFromEnv()
		if !ok {
			t.Fatal(util.EnvOtelcBuildDeadline + " not set, want it published for child processes")
		}
		if !published.Equal(deadline) {
			t.Errorf("published deadline %s != ctx deadline %s", published, deadline)
		}
	})

	t.Run("an inherited deadline wins over --timeout", func(t *testing.T) {
		inherited := time.Now().Add(5 * time.Minute)
		t.Setenv(util.EnvOtelcBuildDeadline, util.FormatBuildDeadline(inherited))

		ctx := run(t, "--timeout", "1h", "go")

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("ctx has no deadline, want the inherited one")
		}
		if !deadline.Equal(inherited) {
			t.Errorf("deadline = %s, want the inherited %s", deadline, inherited)
		}
	})
}

func TestReportedErr(t *testing.T) {
	runErr := errors.New("signal: killed")
	cause := errors.New("the --timeout build timeout (1ms) elapsed")

	t.Run("deadline cause wins when both are set", func(t *testing.T) {
		if got := reportedErr(runErr, cause); !errors.Is(got, cause) {
			t.Errorf("reportedErr() = %v, want the deadline cause", got)
		}
	})

	t.Run("the run error passes through when there is no deadline cause", func(t *testing.T) {
		if got := reportedErr(runErr, nil); !errors.Is(got, runErr) {
			t.Errorf("reportedErr() = %v, want the run error", got)
		}
	})

	t.Run("nil stays nil when the run succeeded and had no deadline", func(t *testing.T) {
		if got := reportedErr(nil, nil); got != nil {
			t.Errorf("reportedErr() = %v, want nil", got)
		}
	})
}

func TestDeadlineCauseIfExceeded(t *testing.T) {
	t.Run("nil for a context with no deadline at all", func(t *testing.T) {
		if got := deadlineCauseIfExceeded(context.Background()); got != nil {
			t.Errorf("deadlineCauseIfExceeded() = %v, want nil", got)
		}
	})

	t.Run("nil for a context canceled for an unrelated reason", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if got := deadlineCauseIfExceeded(ctx); got != nil {
			t.Errorf("deadlineCauseIfExceeded() = %v, want nil for a plain cancel", got)
		}
	})

	t.Run("nil while the deadline has not actually passed yet", func(t *testing.T) {
		ctx, err := withDeadline(context.Background(), time.Now().Add(time.Hour), "should not surface")
		if err != nil {
			t.Fatalf("withDeadline() error = %v", err)
		}
		defer cancelDeadline(ctx)

		if got := deadlineCauseIfExceeded(ctx); got != nil {
			t.Errorf("deadlineCauseIfExceeded() = %v, want nil before the deadline passes", got)
		}
	})

	t.Run("surfaces the custom cause once the deadline passes", func(t *testing.T) {
		const message = "the --timeout build timeout (1ms) elapsed"
		ctx, err := withDeadline(context.Background(), time.Now().Add(time.Millisecond), message)
		if err != nil {
			t.Fatalf("withDeadline() error = %v", err)
		}
		defer cancelDeadline(ctx)
		<-ctx.Done()

		cause := deadlineCauseIfExceeded(ctx)
		if cause == nil {
			t.Fatal("deadlineCauseIfExceeded() = nil, want the cause withDeadline attached")
		}
		if cause.Error() != message {
			t.Errorf("cause = %q, want %q", cause.Error(), message)
		}
	})
}

func TestCancelDeadline(t *testing.T) {
	t.Run("no-op on a context initDeadline never touched", func(t *testing.T) {
		// Must not panic on the type assertion when no cancel func is stored.
		cancelDeadline(context.Background())
	})

	t.Run("releases the deadline initDeadline derived", func(t *testing.T) {
		ctx, err := withDeadline(context.Background(), time.Now().Add(time.Hour), "test")
		if err != nil {
			t.Fatalf("withDeadline() error = %v", err)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("ctx has no deadline, want one from withDeadline")
		}

		cancelDeadline(ctx)

		if ctxErr := ctx.Err(); ctxErr == nil {
			t.Error("ctx.Err() = nil, want context.Canceled after cancelDeadline")
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
			&cli.StringFlag{
				Name:      flagWorkDir,
				Aliases:   []string{"w"},
				Usage:     "The path to a directory where working files will be written",
				TakesFile: true,
				Value:     util.GetOtelcWorkDir(),
			},
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

// TestRootCommandFlagNamesUnique replicates the duplicate flag name check that
// urfave/cli v3.14.0+ performs at startup: no flag Name may collide with an
// Alias or another flag Name within the same command. Checking it directly
// catches such a regression even while the module still pins an older cli
// version that tolerates the collision.
func TestRootCommandFlagNamesUnique(t *testing.T) {
	app := &cli.Command{
		Name:  "otelc",
		Flags: rootFlags(),
		Commands: []*cli.Command{
			&commandPin,
			&commandSetup,
			&commandGo,
			&commandCleanup,
			&commandToolexec,
			&commandVersion,
		},
	}
	err := app.Walk(func(cmd *cli.Command) error {
		seen := make(map[string]struct{})
		for _, f := range cmd.Flags {
			for _, name := range f.Names() {
				if _, dup := seen[name]; dup {
					return fmt.Errorf("flag %q defined multiple times in command %q", name, cmd.Name)
				}
				seen[name] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
