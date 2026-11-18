// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"encoding/json"
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
