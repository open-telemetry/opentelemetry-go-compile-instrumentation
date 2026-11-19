// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestCanBuildUnlocked(t *testing.T) {
	for _, test := range []struct {
		name                                                     string
		args                                                     []string
		goflags                                                  string
		disabled, vendored, debug, inherited, workspace, noState bool
		want                                                     bool
	}{
		{name: "package", args: []string{"build", "./cmd/app"}, want: true},
		{name: "install", args: []string{"install", "./..."}, want: true},
		{name: "race", args: []string{"build", "-race", "."}, want: true},
		{name: "empty"},
		{name: "test", args: []string{"test", "."}},
		{name: "file", args: []string{"build", "main.go"}},
		{name: "overlay", args: []string{"build", "-overlay=custom.json", "."}},
		{name: "modfile", args: []string{"build", "-modfile", "custom.mod", "."}},
		{name: "goflags overlay", args: []string{"build", "."}, goflags: "-overlay=custom.json"},
		{name: "goflags modfile", args: []string{"build", "."}, goflags: "-modfile=custom.mod"},
		{name: "disabled", args: []string{"build", "."}, disabled: true},
		{name: "vendored", args: []string{"build", "."}, vendored: true},
		{name: "debug", args: []string{"build", "."}, debug: true},
		{name: "nested", args: []string{"build", "."}, inherited: true},
		{name: "workspace", args: []string{"build", "."}, workspace: true},
		{name: "no state", args: []string{"build", "."}, noState: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			mustWriteFile(t, "main.go", "package main\nfunc main() {}\n")
			t.Setenv(envUnlockedCompile, "1")
			if test.disabled {
				t.Setenv(envUnlockedCompile, "")
			}
			t.Setenv("GOFLAGS", test.goflags)
			t.Setenv(util.EnvOtelcSessionDir, "")
			if test.inherited {
				t.Setenv(util.EnvOtelcSessionDir, t.TempDir())
			}
			state := newStateManager()
			if test.workspace {
				state.files[filepath.Join(t.TempDir(), "go.work.sum")] = true
			}
			ctx := contextWithStateManager(t.Context(), state)
			if test.noState {
				ctx = t.Context()
			}
			cmd := &cli.Command{
				SkipFlagParsing: true,
				Flags:           []cli.Flag{&cli.BoolFlag{Name: "debug", Value: test.debug}},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					assert.Equal(t, test.want, canBuildUnlocked(ctx, cmd, test.vendored))
					return nil
				},
			}
			require.NoError(t, cmd.Run(ctx, append([]string{"otelc"}, test.args...)))
		})
	}
}

func TestGoBuildSnapshotLifecycle(t *testing.T) {
	for _, test := range []struct {
		name                                string
		unlocked, failCompile, failSnapshot bool
	}{
		{name: "unlocked", unlocked: true},
		{name: "compile failure", unlocked: true, failCompile: true},
		{name: "snapshot failure", unlocked: true, failSnapshot: true},
		{name: "locked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearNestedBuildEnv(t)
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv(util.EnvOtelcWorkDir, root)
			t.Setenv(util.EnvOtelcInvocationDir, "")
			t.Setenv(util.EnvOtelcSessionDir, "")
			t.Setenv(util.EnvOtelcStats, "1")
			t.Setenv(envUnlockedCompile, "")
			if test.unlocked {
				t.Setenv(envUnlockedCompile, "1")
			}
			const goMod = "module example.com/lifecycle\n\ngo 1.26.0\n"
			mustWriteFile(t, "go.mod", goMod)
			mustWriteFile(t, "main.go", "package main\nfunc main() {}\n")
			if test.failSnapshot {
				file := filepath.Join(root, "invalid-temp")
				mustWriteFile(t, file, "x")
				t.Setenv("TMPDIR", file)
				t.Setenv("TMP", file)
				t.Setenv("TEMP", file)
			}
			compileFailure := errors.New("compiler failed")
			original := runBuildCmd
			t.Cleanup(func() { runBuildCmd = original })
			called := false
			var invocation string
			runBuildCmd = func(_ context.Context, env []string, args ...string) error {
				called = true
				invocation = os.Getenv(util.EnvOtelcInvocationDir)
				if test.unlocked {
					require.DirExists(t, invocation)
					assert.Contains(t, args, "-mod=readonly")
					assert.Contains(t, args, "-overlay="+filepath.Join(invocation, "overlay.json"))
					assert.Contains(t, env, util.EnvOtelcInvocationDir+"="+invocation)
					module, readErr := os.ReadFile("go.mod")
					require.NoError(t, readErr)
					assert.Equal(t, goMod, string(module), "module must be restored before compilation")
				}
				if test.failCompile {
					return compileFailure
				}
				return nil
			}
			cmd := &cli.Command{Name: "go", SkipFlagParsing: true, Action: GoBuild}
			ctx := util.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))
			err := cmd.Run(ctx, []string{"otelc", "build", "."})
			if test.failSnapshot {
				require.Error(t, err)
				assert.False(t, called, "failed snapshot must not reach compilation")
				return
			}
			if test.failCompile {
				require.ErrorIs(t, err, compileFailure)
			} else {
				require.NoError(t, err)
			}
			assert.True(t, called)
			assert.Empty(t, os.Getenv(util.EnvOtelcInvocationDir))
			if test.unlocked {
				assert.NoDirExists(t, invocation)
			}
			module, readErr := os.ReadFile("go.mod")
			require.NoError(t, readErr)
			assert.Equal(t, goMod, string(module))
		})
	}
}

func TestSnapshotFilesystemErrors(t *testing.T) {
	t.Run("temporary directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-directory")
		mustWriteFile(t, file, "x")
		t.Setenv("TMPDIR", file)
		t.Setenv("TMP", file)
		t.Setenv("TEMP", file)
		snapshot, err := newSetupSnapshot()
		require.Error(t, err)
		assert.Nil(t, snapshot)
	})
	t.Run("stat", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-directory")
		mustWriteFile(t, file, "x")
		state := newStateManager()
		state.files[filepath.Join(file, "source.go")] = true
		snapshot := &setupSnapshot{invocationDir: t.TempDir()}
		require.Error(t, snapshot.writeOverlay(state))
	})
	t.Run("copy", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "source.go")
		mustWriteFile(t, source, "package main")
		mustWriteFile(t, filepath.Join(root, "overlay"), "not-a-directory")
		state := newStateManager()
		state.files[source] = true
		snapshot := &setupSnapshot{invocationDir: root}
		require.Error(t, snapshot.writeOverlay(state))
	})
	t.Run("write overlay", func(t *testing.T) {
		snapshot := &setupSnapshot{overlayFile: t.TempDir()}
		require.Error(t, snapshot.writeOverlay(newStateManager()))
	})
}

func TestUnlockedGoCache(t *testing.T) {
	t.Setenv("GOCACHE", "")
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	snapshot, err := newSetupSnapshot()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(snapshot.invocationDir)) })
	t.Setenv(util.EnvOtelcInvocationDir, snapshot.invocationDir)
	env, err := setupGoCache(t.Context(), nil)
	require.NoError(t, err)
	cache := filepath.Join(snapshot.invocationDir, "gocache")
	assert.Contains(t, env, "GOCACHE="+cache)
	require.NoError(t, os.RemoveAll(util.GetBuildTempDir()))
	assert.DirExists(t, cache)
}

func TestSetupSnapshotOverlay(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	root := t.TempDir()
	state := newStateManager()
	changed := filepath.Join(root, "go.mod")
	deleted := filepath.Join(root, "deleted.go")
	created := filepath.Join(root, "created.go")
	absent := filepath.Join(root, "absent.go")
	mustWriteFile(t, changed, "original")
	mustWriteFile(t, deleted, "original")
	require.NoError(t, state.TrackAll(changed, deleted, created, absent))
	mustWriteFile(t, changed, "modified")
	mustWriteFile(t, created, "new")
	require.NoError(t, os.Remove(deleted))

	snapshot, err := newSetupSnapshot()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(snapshot.invocationDir)) })
	require.NoError(t, snapshot.writeOverlay(state))
	require.NoError(t, restoreBeforeCompile(state))
	// A cleanup of the shared work directory must not remove overlay inputs.
	require.NoError(t, os.RemoveAll(util.GetBuildTempDir()))
	content, err := os.ReadFile(snapshot.overlayFile)
	require.NoError(t, err)
	var overlay goOverlay
	require.NoError(t, json.Unmarshal(content, &overlay))
	require.Len(t, overlay.Replace, 3)
	assert.Empty(t, overlay.Replace[deleted])
	assert.NotContains(t, overlay.Replace, absent)
	for path, want := range map[string]string{changed: "modified", created: "new"} {
		data, readErr := os.ReadFile(overlay.Replace[path])
		require.NoError(t, readErr)
		assert.Equal(t, want, string(data))
	}
	original, err := os.ReadFile(changed)
	require.NoError(t, err)
	assert.Equal(t, "original", string(original))
	assert.NoFileExists(t, created)
}

func TestRestoreBeforeCompileFailure(t *testing.T) {
	for _, failure := range []string{"restore", "discard"} {
		t.Run(failure, func(t *testing.T) {
			t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
			path := filepath.Join(t.TempDir(), "go.mod")
			mustWriteFile(t, path, "original")
			state := newStateManager()
			require.NoError(t, state.Track(path))
			mustWriteFile(t, path, "modified")
			if failure == "restore" {
				require.NoError(t, os.Remove(filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(path))))
			} else {
				require.NoError(t, os.Remove(util.GetBuildTemp(stateFileName)))
				mustWriteFile(t, filepath.Join(util.GetBuildTemp(stateFileName), "block-discard"), "x")
			}
			require.Error(t, restoreBeforeCompile(state))
			if failure == "restore" {
				assert.FileExists(t, util.GetBuildTemp(stateFileName))
			}
		})
	}
}
