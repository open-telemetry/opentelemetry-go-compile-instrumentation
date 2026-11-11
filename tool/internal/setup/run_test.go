// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestRunBuildArgs(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "keeps build flags and the package",
			args: []string{"-tags", "foo", "-trimpath", "./cmd", "-tags", "bar", "arg"},
			want: []string{"-tags", "foo", "-trimpath", "./cmd"},
		},
		{
			name: "drops -exec and its value",
			args: []string{"-exec", "xprog", "-race", "--exec=yprog", ".", "-exec", "zprog"},
			want: []string{"-race", "."},
		},
		{
			name: "keeps every .go file target",
			args: []string{"main.go", "extra.go", "arg", "later.go"},
			want: []string{"main.go", "extra.go"},
		},
		{
			name: "keeps -C first",
			args: []string{"-C", "app", "-mod=vendor", ".", "-C", "other"},
			want: []string{"-C", "app", "-mod=vendor", "."},
		},
		{
			name: "keeps the terminator",
			args: []string{"--", "main.go", "--", "-v"},
			want: []string{"--", "main.go"},
		},
		{
			name: "no arguments",
			args: nil,
			want: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, runBuildArgs(tt.args))
		})
	}
}

// A program argument that looks like a build flag must not override GOFLAGS
// in the build that setup prepares.
func TestRunBuildArgs_GOFLAGS(t *testing.T) {
	t.Setenv("GOFLAGS", "-tags=envtag -pgo=off")
	args := runBuildArgs([]string{".", "-tags=progtag", "-pgo=auto"})

	sp := &setupPhase{buildFlags: extractBuildFlags(subcmdBuild, args)}
	assert.Contains(t, sp.getBuildContext().BuildTags, "envtag")
	assert.NotContains(t, sp.getBuildContext().BuildTags, "progtag")
	assert.Equal(t, "off", pgoValue(subcmdBuild, args))
}

// stubRunBuildCmd replaces runBuildCmd with fn for the duration of the test.
func stubRunBuildCmd(t *testing.T, fn func(context.Context, []string, ...string) error) {
	t.Helper()
	original := runBuildCmd
	t.Cleanup(func() { runBuildCmd = original })
	runBuildCmd = fn
}

// exitStatus2 returns the error of a command that exits with status 2: this
// test binary, given a flag it does not define.
func exitStatus2(t *testing.T) error {
	t.Helper()
	err := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$", "-otelc.undefined").Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, 2, exitErr.ExitCode())
	return err
}

func TestRunProgram(t *testing.T) {
	t.Run("an interrupt does not stop the program", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		var runErr error
		stubRunBuildCmd(t, func(ctx context.Context, _ []string, _ ...string) error {
			runErr = ctx.Err()
			return nil
		})

		require.NoError(t, runProgram(ctx, nil, []string{"go", "run", "."}))
		require.NoError(t, runErr)
	})

	t.Run("reports the exit status of go run", func(t *testing.T) {
		exitErr := exitStatus2(t)
		stubRunBuildCmd(t, func(context.Context, []string, ...string) error {
			return ex.Wrapf(exitErr, "failed to run command")
		})

		err := runProgram(t.Context(), nil, []string{"go", "run", "."})
		var codeErr *ExitCodeError
		require.ErrorAs(t, err, &codeErr)
		assert.Equal(t, 2, codeErr.Code)
		assert.EqualError(t, err, "exit status 2")
	})

	t.Run("returns other failures unchanged", func(t *testing.T) {
		want := errors.New("go not found")
		stubRunBuildCmd(t, func(context.Context, []string, ...string) error {
			return want
		})

		assert.ErrorIs(t, runProgram(t.Context(), nil, []string{"go", "run", "."}), want)
	})
}

func TestBuildWithToolexec_Run(t *testing.T) {
	t.Run("forwards only the build flags", func(t *testing.T) {
		got := runBuildWithToolexec(t, []string{"go", "run", "-tags=a", ".", "-tags=b", "-C", "missing"}, false)

		assert.Equal(t, "run", got.argv[1])
		assert.Equal(t, []string{".", "-tags=b", "-C", "missing"}, got.argv[len(got.argv)-4:])
		want := util.EnvOtelcBuildFlags + "=" + util.EncodeBuildFlags([]string{"-tags=a"})
		assert.Contains(t, got.env, want)
	})

	t.Run("reports the exit status of go run", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv(util.EnvOtelcWorkDir, dir)
		require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
		exitErr := exitStatus2(t)
		stubRunBuildCmd(t, func(context.Context, []string, ...string) error {
			return exitErr
		})

		cmd := &cli.Command{
			Name:            "go",
			SkipFlagParsing: true,
			Action: func(ctx context.Context, c *cli.Command) error {
				return buildWithToolexec(ctx, c, false)
			},
		}
		err := cmd.Run(t.Context(), []string{"go", "run", "."})

		var codeErr *ExitCodeError
		require.ErrorAs(t, err, &codeErr)
		assert.Equal(t, 2, codeErr.Code)
	})
}

// TestSetup_RunPlansEquivalentBuild checks the build plan dry run of
// `otelc go run`: it is the go build of the same package with the same build
// flags, without -exec and without the program's arguments.
func TestSetup_RunPlansEquivalentBuild(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "package",
			args: []string{"run", "-tags", "foo", ".", "-tags", "bar", "arg"},
			want: []string{"build", "-a", "-x", "-n", "-tags", "foo", "."},
		},
		{
			name: "file targets and -exec",
			args: []string{"run", "-exec", "xprog", "main.go", "extra.go", "arg.txt", "-v"},
			want: []string{"build", "-a", "-x", "-n", "main.go", "extra.go"},
		},
		{
			name: "change directory first",
			args: []string{"run", "-C", ".", "-trimpath", ".", "-C", "elsewhere"},
			want: []string{"build", "-C", ".", "-a", "-x", "-n", "-trimpath", "."},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setupTestModule(t, nil)
			dir, err := os.Getwd()
			require.NoError(t, err)
			t.Setenv(util.EnvOtelcWorkDir, dir)
			require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
			mustWriteFile(t, "extra.go", "package main\n")
			// With rules given, setup lists the plan itself instead of auto-pinning.
			t.Setenv(util.EnvOtelcRules, "rules.yaml")

			exe, err := os.Executable()
			require.NoError(t, err)

			var got []string
			oldExec := execCommandContext
			t.Cleanup(func() { execCommandContext = oldExec })
			execCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				got = args
				// Fail the dry run, so setup stops right after it.
				cmd := exec.CommandContext(ctx, exe, "-test.run=^TestHelperProcess$")
				cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "GO_HELPER_BUILD_FAILS=1")
				return cmd
			}

			cmd := &cli.Command{Name: "go", SkipFlagParsing: true, Action: Setup}
			require.ErrorContains(
				t,
				cmd.Run(t.Context(), append([]string{"go"}, tt.args...)),
				"failed to run build plan",
			)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToolexecBuildArgs_Run(t *testing.T) {
	const toolexec = "-toolexec=/usr/bin/otelc toolexec"
	for _, tt := range []struct {
		name     string
		args     []string
		vendored bool
		want     []string
	}{
		{
			name: "flags before the package",
			args: []string{"run", "-tags", "foo", ".", "-tags", "bar"},
			want: []string{"go", "run", toolexec, "-tags", "foo", ".", "-tags", "bar"},
		},
		{
			name: "after a leading -C",
			args: []string{"run", "-C", "app", ".", "-C", "other"},
			want: []string{"go", "run", "-C", "app", toolexec, ".", "-C", "other"},
		},
		{
			name: "keeps -exec",
			args: []string{"run", "-exec", "xprog", ".", "arg"},
			want: []string{"go", "run", toolexec, "-exec", "xprog", ".", "arg"},
		},
		{
			name:     "rewrites -mod=vendor only in the build flags",
			args:     []string{"run", "-mod=vendor", ".", "-mod=vendor"},
			vendored: true,
			want:     []string{"go", "run", toolexec, "-mod=mod", ".", "-mod=vendor"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toolexecBuildArgs(tt.args, "/usr/bin/otelc", tt.vendored)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestToolexecBuildArgs_RunFileTargets(t *testing.T) {
	const toolexec = "-toolexec=/usr/bin/otelc toolexec"
	dir := t.TempDir()
	t.Chdir(dir)
	for _, name := range []string{"main.go", "extra.go", otelcRuntimeFile} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0o644))
	}

	for _, tt := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "single file",
			args: []string{"run", "main.go", "-flag", "value"},
			want: []string{"go", "run", toolexec, "main.go", otelcRuntimeFile, "-flag", "value"},
		},
		{
			name: "multiple files",
			args: []string{"run", "main.go", "extra.go", "arg", "later.go"},
			want: []string{"go", "run", toolexec, "main.go", "extra.go", otelcRuntimeFile, "arg", "later.go"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toolexecBuildArgs(tt.args, "/usr/bin/otelc", false)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
