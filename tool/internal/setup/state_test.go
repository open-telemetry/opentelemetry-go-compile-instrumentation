// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func TestLoadStateManager(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	modified := filepath.Join(tmp, "modified.go")
	generated := filepath.Join(tmp, "generated.go")

	tests := []struct {
		name      string
		setup     func(t *testing.T)
		wantNil   bool
		wantFiles map[string]bool
		wantErr   bool
	}{
		{
			name:    "missing state file",
			wantNil: true,
		},
		{
			name: "invalid json",
			setup: func(t *testing.T) {
				mustWriteFile(t, util.GetBuildTemp(stateFileName), "{bad json")
			},
			wantErr: true,
		},
		{
			name: "loads tracked files",
			setup: func(t *testing.T) {
				state, err := json.Marshal([]string{
					modified,
					"-" + generated,
				})
				require.NoError(t, err)

				mustWriteFile(t, util.GetBuildTemp(stateFileName), string(state))
			},
			wantFiles: map[string]bool{
				modified:  true,
				generated: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.RemoveAll(util.GetBuildTempDir())

			if tt.setup != nil {
				tt.setup(t)
			}

			stateManager, err := LoadStateManager()
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.wantNil {
				require.Nil(t, stateManager)
				return
			}

			require.Equal(t, tt.wantFiles, stateManager.files)
		})
	}
}

func TestStateManagerFromContext(t *testing.T) {
	t.Run("manager exists in context", func(t *testing.T) {
		expected := NewStateManager()

		ctx := ContextWithStateManager(t.Context(), expected)
		actual, found := StateManagerFromContext(ctx)

		require.True(t, found)
		require.Same(t, expected, actual)
	})

	t.Run("manager missing from context", func(t *testing.T) {
		actual, found := StateManagerFromContext(t.Context())

		require.False(t, found)
		require.NotNil(t, actual)
		require.Empty(t, actual.files)
	})
}

func TestGetBackupFiles(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, tmp string) string
		wantFiles func(tmp, moduleDir string) []string
	}{
		{
			name: "go.mod, go.sum and go.work.sum exist",
			setup: func(t *testing.T, tmp string) string {
				moduleDir := filepath.Join(tmp, "mod")

				mustWriteFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com")
				mustWriteFile(t, filepath.Join(moduleDir, "go.sum"), "sum")

				mustWriteFile(t, filepath.Join(tmp, "go.work"), "go 1.24")
				mustWriteFile(t, filepath.Join(tmp, "go.work.sum"), "worksum")

				return moduleDir
			},
			wantFiles: func(tmp, moduleDir string) []string {
				return []string{
					filepath.Join(moduleDir, "go.mod"),
					filepath.Join(moduleDir, "go.sum"),
					filepath.Join(tmp, "go.work.sum"),
				}
			},
		},
		{
			name: "missing go.sum and go.work.sum",
			setup: func(t *testing.T, tmp string) string {
				moduleDir := filepath.Join(tmp, "mod")

				mustWriteFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com")
				mustWriteFile(t, filepath.Join(tmp, "go.work"), "go 1.24")

				return moduleDir
			},
			wantFiles: func(tmp, moduleDir string) []string {
				return []string{
					filepath.Join(moduleDir, "go.mod"),
					filepath.Join(moduleDir, "go.sum"),
					filepath.Join(tmp, "go.work.sum"),
				}
			},
		},
		{
			name: "missing go.work",
			setup: func(t *testing.T, tmp string) string {
				moduleDir := filepath.Join(tmp, "mod")
				mustWriteFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com")
				return moduleDir
			},
			wantFiles: func(tmp, moduleDir string) []string {
				return []string{
					filepath.Join(moduleDir, "go.mod"),
					filepath.Join(moduleDir, "go.sum"),
				}
			},
		},
		{
			name: "missing go.mod",
			setup: func(t *testing.T, tmp string) string {
				return filepath.Join(tmp, "mod")
			},
			wantFiles: func(_, _ string) []string {
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Chdir(tmp)

			moduleDir := tt.setup(t, tmp)

			files, err := getBackupFiles(t.Context(), map[string]bool{
				moduleDir: true,
			})

			require.NoError(t, err)
			require.ElementsMatch(t, tt.wantFiles(tmp, moduleDir), files)
		})
	}
}

func TestStateSnapshotPath(t *testing.T) {
	path := filepath.Join("foo", "..", "bar", "go.mod")

	got1 := stateSnapshotPath(path)
	got2 := stateSnapshotPath(filepath.Clean(path))

	require.Equal(t, got1, got2)
	require.True(t, strings.HasPrefix(got1, "go.mod."))
}

func TestStateManagerTrack(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, tmp string) string
		existed bool
	}{
		{
			name: "existing file",
			setup: func(t *testing.T, tmp string) string {
				path := filepath.Join(tmp, "go.mod")
				mustWriteFile(t, path, "module example")
				return path
			},
			existed: true,
		},
		{
			name: "missing file",
			setup: func(t *testing.T, tmp string) string {
				return filepath.Join(tmp, "otelc.runtime.go")
			},
			existed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Chdir(tmp)

			path := tt.setup(t, tmp)

			stateManager := NewStateManager()

			require.NoError(t, stateManager.Track(path))
			require.Equal(t, tt.existed, stateManager.files[path])

			snapshot := filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(path))
			require.Equal(t, tt.existed, util.PathExists(snapshot))
		})
	}
}

func TestStateManagerTrack_Duplicate(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	path := filepath.Join(tmp, "go.mod")
	mustWriteFile(t, path, "original")

	stateManager := NewStateManager()
	require.NoError(t, stateManager.Track(path))

	// Modify the original file after it has been tracked.
	mustWriteFile(t, path, "modified")

	// Tracking the same file again should be a no-op.
	require.NoError(t, stateManager.Track(path))

	snapshot := filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(path))
	data, err := os.ReadFile(snapshot)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))

	require.Len(t, stateManager.files, 1)
	require.True(t, stateManager.files[path])
}

func TestStateManagerTrackAll(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	a := filepath.Join(tmp, "a")
	b := filepath.Join(tmp, "b")

	mustWriteFile(t, a, "a")

	stateManager := NewStateManager()
	require.NoError(t, stateManager.TrackAll(a, b))

	require.Equal(t, map[string]bool{
		a: true,
		b: false,
	}, stateManager.files)
}

func TestStateManagerCommit(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	existing := filepath.Join(tmp, "go.mod")
	generated := filepath.Join(tmp, OtelcRuntimeFile)

	mustWriteFile(t, existing, "module example")

	stateManager := NewStateManager()

	require.NoError(t, stateManager.Track(existing))
	require.NoError(t, stateManager.Track(generated))
	require.NoError(t, stateManager.Commit())

	data, err := os.ReadFile(util.GetBuildTemp(stateFileName))
	require.NoError(t, err)

	var entries []string
	require.NoError(t, json.Unmarshal(data, &entries))

	require.ElementsMatch(t, []string{
		existing,
		"-" + generated,
	}, entries)
}

func TestStateManagerRevert(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	existing := filepath.Join(tmp, "go.mod")
	generated := filepath.Join(tmp, OtelcRuntimeFile)

	mustWriteFile(t, existing, "original")

	stateManager := NewStateManager()

	require.NoError(t, stateManager.Track(existing))
	require.NoError(t, stateManager.Track(generated))

	mustWriteFile(t, existing, "modified")
	mustWriteFile(t, generated, "generated")

	require.NoError(t, stateManager.Revert())

	data, err := os.ReadFile(existing)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))

	require.False(t, util.PathExists(generated))
}

func TestStateManagerRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	original := filepath.Join(tmp, "go.mod")
	generated := filepath.Join(tmp, OtelcRuntimeFile)

	mustWriteFile(t, original, "module original")

	stateManager := NewStateManager()

	require.NoError(t, stateManager.Track(original))
	require.NoError(t, stateManager.Track(generated))
	require.NoError(t, stateManager.Commit())

	// simulate instrumentation
	mustWriteFile(t, original, "module modified")
	mustWriteFile(t, generated, "package main")

	loaded, err := LoadStateManager()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	require.NoError(t, loaded.Revert())

	data, err := os.ReadFile(original)
	require.NoError(t, err)
	require.Equal(t, "module original", string(data))

	require.False(t, util.PathExists(generated))
}
