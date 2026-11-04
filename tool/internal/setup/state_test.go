// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
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

			stateManager, err := loadStateManager()
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
		expected := newStateManager()

		ctx := contextWithStateManager(t.Context(), expected)
		actual, found := stateManagerFromContext(ctx)

		require.True(t, found)
		require.Same(t, expected, actual)
	})

	t.Run("manager missing from context", func(t *testing.T) {
		actual, found := stateManagerFromContext(t.Context())

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
			name: "go.mod, go.sum, tool file and go.work.sum",
			setup: func(t *testing.T, tmp string) string {
				moduleDir := filepath.Join(tmp, "mod")

				mustWriteFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com")
				mustWriteFile(t, filepath.Join(moduleDir, "go.sum"), "sum")
				mustWriteFile(t, filepath.Join(moduleDir, toolFileCanonical), "//go:build tools\npackage tools")

				mustWriteFile(t, filepath.Join(tmp, "go.work"), "go 1.24")
				mustWriteFile(t, filepath.Join(tmp, "go.work.sum"), "worksum")

				return moduleDir
			},
			wantFiles: func(tmp, moduleDir string) []string {
				return []string{
					filepath.Join(moduleDir, "go.mod"),
					filepath.Join(moduleDir, "go.sum"),
					filepath.Join(moduleDir, toolFileCanonical),
					filepath.Join(tmp, "go.work.sum"),
				}
			},
		},
		{
			name: "missing tool file, go.sum and go.work.sum",
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
					filepath.Join(moduleDir, toolFileCanonical),
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
			wantFiles: func(_, moduleDir string) []string {
				return []string{
					filepath.Join(moduleDir, "go.mod"),
					filepath.Join(moduleDir, "go.sum"),
					filepath.Join(moduleDir, toolFileCanonical),
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
		{
			name: "uses tool file alias when canonical is missing",
			setup: func(t *testing.T, tmp string) string {
				moduleDir := filepath.Join(tmp, "mod")
				mustWriteFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com")
				mustWriteFile(t, filepath.Join(moduleDir, toolFileAlias), "")
				return moduleDir
			},
			wantFiles: func(_, moduleDir string) []string {
				return []string{
					filepath.Join(moduleDir, "go.mod"),
					filepath.Join(moduleDir, "go.sum"),
					filepath.Join(moduleDir, toolFileAlias),
				}
			},
		},
		{
			name: "GOWORK=off does not track go.work.sum",
			setup: func(t *testing.T, tmp string) string {
				t.Setenv("GOWORK", "off")
				moduleDir := filepath.Join(tmp, "mod")
				mustWriteFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com")
				return moduleDir
			},
			wantFiles: func(_, moduleDir string) []string {
				return []string{
					filepath.Join(moduleDir, "go.mod"),
					filepath.Join(moduleDir, "go.sum"),
					filepath.Join(moduleDir, toolFileCanonical),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Chdir(tmp)

			moduleDir := tt.setup(t, tmp)

			files, err := getBackupFiles(t.Context(), []string{
				moduleDir,
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

			stateManager := newStateManager()

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

	stateManager := newStateManager()
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

func TestStateManagerTrackPersistsBeforeMutation(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	goMod := filepath.Join(tmp, "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n\ngo 1.25.0\n")
	runtimeFile := filepath.Join(tmp, otelcRuntimeFile)

	// The "build" process tracks, mutates, and dies without any explicit
	// Commit, exactly the `otelc go build` path.
	dying := newStateManager()
	require.NoError(t, dying.Track(goMod))
	require.NoError(t, dying.Track(runtimeFile))
	mustWriteFile(t, goMod, "module example.com/app\n\ngo 1.25.0\n\nreplace x => ./.otelc-build/x\n")
	mustWriteFile(t, runtimeFile, "package main\n")
	// (process dies here)

	// A fresh process recovers from the manifest alone.
	recovered, err := loadStateManager()
	require.NoError(t, err)
	require.NotNil(t, recovered, "manifest must exist without an explicit Commit")
	require.NoError(t, recovered.Revert())

	content, err := os.ReadFile(goMod)
	require.NoError(t, err)
	assert.Equal(t, "module example.com/app\n\ngo 1.25.0\n", string(content), "go.mod must be restored")
	assert.False(t, util.PathExists(runtimeFile), "generated runtime file must be removed")
}

func TestStateManagerTrackAll(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	a := filepath.Join(tmp, "a")
	b := filepath.Join(tmp, "b")

	mustWriteFile(t, a, "a")

	stateManager := newStateManager()
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
	generated := filepath.Join(tmp, otelcRuntimeFile)

	mustWriteFile(t, existing, "module example")

	stateManager := newStateManager()

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
	generated := filepath.Join(tmp, otelcRuntimeFile)

	mustWriteFile(t, existing, "original")

	stateManager := newStateManager()

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
	generated := filepath.Join(tmp, otelcRuntimeFile)

	mustWriteFile(t, original, "module original")

	stateManager := newStateManager()

	require.NoError(t, stateManager.Track(original))
	require.NoError(t, stateManager.Track(generated))
	require.NoError(t, stateManager.Commit())

	// simulate instrumentation
	mustWriteFile(t, original, "module modified")
	mustWriteFile(t, generated, "package main")

	loaded, err := loadStateManager()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	require.NoError(t, loaded.Revert())

	data, err := os.ReadFile(original)
	require.NoError(t, err)
	require.Equal(t, "module original", string(data))

	require.False(t, util.PathExists(generated))
}

func TestStateManagerCommitIsAtomicAndSorted(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	b := filepath.Join(tmp, "b.txt")
	a := filepath.Join(tmp, "a.txt")
	mustWriteFile(t, a, "a")

	s := newStateManager()
	require.NoError(t, s.Track(b)) // missing file, tracked first
	require.NoError(t, s.Track(a))

	first, err := os.ReadFile(util.GetBuildTemp(stateFileName))
	require.NoError(t, err)
	require.NoError(t, s.Commit())
	second, err := os.ReadFile(util.GetBuildTemp(stateFileName))
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second), "manifest serialization must be deterministic")
	assert.False(t, util.PathExists(util.GetBuildTemp(stateFileName)+".tmp"), "no temp file left behind")
}

func TestGetBackupFiles_DeterministicOrder(t *testing.T) {
	tmp := t.TempDir()
	modA := filepath.Join(tmp, "dirA")
	modB := filepath.Join(tmp, "dirB")
	modC := filepath.Join(tmp, "dirC")

	require.NoError(t, os.MkdirAll(modA, 0o755))
	require.NoError(t, os.MkdirAll(modB, 0o755))
	require.NoError(t, os.MkdirAll(modC, 0o755))

	mustWriteFile(t, filepath.Join(modA, "go.mod"), "module dirA")
	mustWriteFile(t, filepath.Join(modB, "go.mod"), "module dirB")
	mustWriteFile(t, filepath.Join(modC, "go.mod"), "module dirC")

	moduleDirs := []string{
		modC,
		modA,
		modB,
		modA, // duplicate to verify deduplication
	}
	callerOrig := append([]string(nil), moduleDirs...)

	var firstResult []string
	for range 20 {
		files, err := getBackupFiles(t.Context(), moduleDirs)
		require.NoError(t, err)

		if firstResult == nil {
			firstResult = files
			// Assert that files contains no duplicates
			seenFiles := make(map[string]bool)
			for _, f := range files {
				assert.False(t, seenFiles[f], "duplicate backup file: %s", f)
				seenFiles[f] = true
			}
		} else {
			assert.Equal(t, firstResult, files, "getBackupFiles output must be deterministic across multiple calls")
		}
	}
	assert.Equal(t, callerOrig, moduleDirs, "caller slice must not be mutated")
}

func TestStateManagerDiscard(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	s := newStateManager()

	// Track a non-existent path so Commit writes a manifest, then Discard must
	// remove the manifest and the snapshot directory.
	require.NoError(t, s.Track(util.GetBuildTemp("ghost.txt")))
	require.FileExists(t, util.GetBuildTemp(stateFileName))

	require.NoError(t, s.Discard())

	_, err := os.Stat(util.GetBuildTemp(stateFileName))
	assert.True(t, os.IsNotExist(err), "manifest must be removed")
}

func TestStateManagerDiscardEmpty(t *testing.T) {
	// Discarding an empty state manager is a no-op.
	require.NoError(t, newStateManager().Discard())
}

func TestStateManagerTrack_CopyFails(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	src := filepath.Join(tmp, "go.mod")
	mustWriteFile(t, src, "module example.com")

	// Make stateDir a regular file so CopyFile fails on all platforms (including Windows)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	snapshotDir := util.GetBuildTemp(stateDir)
	_ = os.RemoveAll(snapshotDir)
	require.NoError(t, os.WriteFile(snapshotDir, []byte("file"), 0o644))

	sm := newStateManager()
	err := sm.Track(src)
	require.Error(t, err)
}

func TestRevertRemovesCreatedFilesAndDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	existing := filepath.Join(tmp, "vendor", "modules.txt")
	mustWriteFile(t, existing, "before")
	newDir := filepath.Join(tmp, "vendor", "example.com")
	newSubDir := filepath.Join(newDir, "m")
	newFile := filepath.Join(newSubDir, "a.go")

	s := newStateManager()
	require.NoError(t, s.TrackAll(existing, newDir, newSubDir, newFile))
	// TrackAll persists the state one time, with all the paths.
	loaded, err := loadStateManager()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Len(t, loaded.files, 4)

	mustWriteFile(t, existing, "after")
	mustWriteFile(t, newFile, "package m")

	require.NoError(t, s.Revert())

	data, err := os.ReadFile(existing)
	require.NoError(t, err)
	assert.Equal(t, "before", string(data))
	assert.NoFileExists(t, newFile)
	assert.NoDirExists(t, newSubDir)
	assert.NoDirExists(t, newDir)
}

func TestRevertKeepsCreatedDirWithUnknownFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	newDir := filepath.Join(tmp, "created")
	s := newStateManager()
	require.NoError(t, s.Track(newDir))
	mustWriteFile(t, filepath.Join(newDir, "user.txt"), "not tracked")

	require.NoError(t, s.Revert())
	assert.FileExists(t, filepath.Join(newDir, "user.txt"))
}

// TestRevertDoesNotFollowLinks: a tracked path, or a tracked directory, that
// is a symbolic link at the time of Revert must not make Revert write or
// remove files at the target of the link.
func TestRevertDoesNotFollowLinks(t *testing.T) {
	skipWithoutSymlinks(t)
	tmp := t.TempDir()
	t.Chdir(tmp)
	outside := t.TempDir()

	existing := filepath.Join(tmp, "vendor", "modules.txt")
	mustWriteFile(t, existing, "before")
	newDir := filepath.Join(tmp, "vendor", "example.com")
	newFile := filepath.Join(newDir, "a.go")

	s := newStateManager()
	require.NoError(t, s.TrackAll(existing, newDir, newFile))

	// The tracked file and the tracked directory become links to files
	// outside the module.
	outsideFile := filepath.Join(outside, "target.txt")
	mustWriteFile(t, outsideFile, "outside")
	require.NoError(t, os.Remove(existing))
	require.NoError(t, os.Symlink(outsideFile, existing))
	mustWriteFile(t, filepath.Join(outside, "a.go"), "outside a.go")
	require.NoError(t, os.Symlink(outside, newDir))

	err := s.Revert()
	require.Error(t, err, "a tracked directory that is a link now is an error")

	data, readErr := os.ReadFile(outsideFile)
	require.NoError(t, readErr)
	assert.Equal(t, "outside", string(data), "Revert must not write through a link")
	assert.FileExists(t, filepath.Join(outside, "a.go"), "Revert must not remove files through a link")

	info, statErr := os.Lstat(existing)
	require.NoError(t, statErr)
	assert.True(t, info.Mode().IsRegular(), "the link is replaced by the restored file")
	data, readErr = os.ReadFile(existing)
	require.NoError(t, readErr)
	assert.Equal(t, "before", string(data))
}

// TestLoadStateManagerRejectsOutsidePaths: the manifest is a file in the
// project, so it is not trusted. A path outside the project must not be
// restored or removed.
func TestLoadStateManagerRejectsOutsidePaths(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWriteFile(t, outside, "keep me")

	for name, entry := range map[string]string{
		"outside path":  "-" + outside,
		"relative path": "-relative/x",
		"unclean path":  "-" + tmp + "/a/../../x",
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal([]string{entry})
			require.NoError(t, err)
			mustWriteFile(t, util.GetBuildTemp(stateFileName), string(data))

			_, err = loadStateManager()
			require.Error(t, err)
			require.Error(t, recoverLeftoverState(t.Context()))
			assert.FileExists(t, outside, "a file outside the project is never removed")
		})
	}
}

// TestRevertDoesNotFollowUntrackedLinkedParent: an existing vendor directory
// (not tracked) becomes a symbolic link to a directory outside the project.
// Revert must not write the snapshot through the link.
func TestRevertDoesNotFollowUntrackedLinkedParent(t *testing.T) {
	skipWithoutSymlinks(t)
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")
	outside := t.TempDir()

	file := filepath.Join(tmp, "vendor", "example.com", "dep", "file.go")
	mustWriteFile(t, file, "original")
	s := newStateManager()
	require.NoError(t, s.Track(file))

	mustWriteFile(t, filepath.Join(outside, "dep", "file.go"), "outside")
	require.NoError(t, os.RemoveAll(filepath.Join(tmp, "vendor", "example.com")))
	require.NoError(t, os.Symlink(outside, filepath.Join(tmp, "vendor", "example.com")))

	require.Error(t, s.Revert())
	data, err := os.ReadFile(filepath.Join(outside, "dep", "file.go"))
	require.NoError(t, err)
	assert.Equal(t, "outside", string(data), "Revert must not write through a linked parent directory")
}

// TestRevertReplacesSwappedLinkWithSameContent: a tracked regular file that
// is a symbolic link now is a regular file again after Revert, also when the
// target has the same content.
func TestRevertReplacesSwappedLinkWithSameContent(t *testing.T) {
	skipWithoutSymlinks(t)
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")

	goMod := filepath.Join(tmp, "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")
	s := newStateManager()
	require.NoError(t, s.Track(goMod))

	outside := filepath.Join(t.TempDir(), "go.mod")
	mustWriteFile(t, outside, "module example.com/app\n")
	require.NoError(t, os.Remove(goMod))
	require.NoError(t, os.Symlink(outside, goMod))

	require.NoError(t, s.Revert())
	info, err := os.Lstat(goMod)
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular())
}

// TestStateManagerDiscardFailureKeepsState: when the manifest cannot be
// removed, the snapshots stay, so the state on disk stays complete.
func TestStateManagerDiscardFailureKeepsState(t *testing.T) {
	skipWithoutPermissions(t)
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)

	goMod := filepath.Join(tmp, "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")
	s := newStateManager()
	require.NoError(t, s.Track(goMod))

	// A directory without write permission: its entries cannot be removed.
	require.NoError(t, os.Chmod(util.GetBuildTempDir(), 0o555))
	t.Cleanup(func() { _ = os.Chmod(util.GetBuildTempDir(), 0o755) })

	require.Error(t, s.Discard())
	assert.FileExists(t, util.GetBuildTemp(stateFileName))
	assert.FileExists(t, filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(goMod)),
		"the snapshots stay with the manifest")
}

// TestStateScopeWorkspace: in a workspace, a manifest can name the
// go.work.sum file of the workspace, but no other file outside the otelc work
// directory, also not a file next to go.work.
func TestStateScopeWorkspace(t *testing.T) {
	ws := t.TempDir()
	workDir := filepath.Join(ws, "app")
	mustWriteFile(t, filepath.Join(ws, "go.work"), "go 1.25.0\n\nuse ./app\n")
	mustWriteFile(t, filepath.Join(workDir, "go.mod"), "module example.com/app\n")
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv("GOWORK", "")

	scope := newStateScope()
	assert.True(t, scope.allows(filepath.Join(workDir, "go.mod")))
	assert.True(t, scope.allows(filepath.Join(ws, goWorkSumFileName)))
	assert.False(t, scope.allows(filepath.Join(ws, "sibling", "important.txt")),
		"a file next to go.work is not in the scope")
	assert.False(t, scope.allows(filepath.Join(ws, "go.work")))

	other := t.TempDir()
	t.Setenv("GOWORK", filepath.Join(other, "go.work"))
	scope = newStateScope()
	assert.True(t, scope.allows(filepath.Join(other, goWorkSumFileName)), "GOWORK names the workspace")
	assert.False(t, scope.allows(filepath.Join(other, "victim.txt")))
	assert.False(t, scope.allows(filepath.Join(ws, goWorkSumFileName)), "GOWORK wins over the go.work file")
}

// TestCleanupRejectsWorkspaceSiblingInManifest: a crafted manifest names a
// file next to the go.work file. otelc cleanup must not remove it.
func TestCleanupRejectsWorkspaceSiblingInManifest(t *testing.T) {
	ws := t.TempDir()
	workDir := filepath.Join(ws, "app")
	mustWriteFile(t, filepath.Join(ws, "go.work"), "go 1.25.0\n\nuse ./app\n")
	victim := filepath.Join(ws, "sibling", "important.txt")
	mustWriteFile(t, victim, "keep me")
	t.Chdir(t.TempDir())
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv("GOWORK", "")

	data, err := json.Marshal([]string{"-" + victim})
	require.NoError(t, err)
	mustWriteFile(t, util.GetBuildTemp(stateFileName), string(data))

	require.Error(t, Cleanup(t.Context(), true))
	assert.FileExists(t, victim)
}

// TestRevertWorkspaceSumAndExternalPath: Revert restores the go.work.sum file
// of the workspace. For a path outside the work directory (only in the build
// that tracked it), a tracked directory that is a link now is an error.
func TestRevertWorkspaceSumAndExternalPath(t *testing.T) {
	ws := t.TempDir()
	workDir := filepath.Join(ws, "app")
	mustWriteFile(t, filepath.Join(ws, "go.work"), "go 1.25.0\n\nuse ./app\n")
	sum := filepath.Join(ws, goWorkSumFileName)
	mustWriteFile(t, sum, "before")
	t.Chdir(t.TempDir())
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv("GOWORK", "")

	s := newStateManager()
	require.NoError(t, s.Track(sum))
	mustWriteFile(t, sum, "after")
	require.NoError(t, s.Revert())
	data, err := os.ReadFile(sum)
	require.NoError(t, err)
	assert.Equal(t, "before", string(data))

	if runtime.GOOS == "windows" {
		return
	}
	external := filepath.Join(t.TempDir(), "mod")
	file := filepath.Join(external, "vendor", "a.go")
	s = newStateManager()
	require.NoError(t, s.TrackAll(external, file))
	mustWriteFile(t, filepath.Join(t.TempDir(), "x"), "")
	outside := t.TempDir()
	require.NoError(t, os.Symlink(outside, external))
	require.Error(t, s.Revert(), "a tracked directory outside the work directory that is a link now")
}

func TestRemoveCreated(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, removeCreated(filepath.Join(dir, "absent")), "a path that does not exist")
	empty := filepath.Join(dir, "empty")
	require.NoError(t, os.MkdirAll(empty, 0o755))
	require.NoError(t, removeCreated(empty))
	assert.NoDirExists(t, empty)
}

func TestRevertPermissionErrors(t *testing.T) {
	skipWithoutPermissions(t)
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")

	locked := filepath.Join(tmp, "locked")
	require.NoError(t, os.MkdirAll(locked, 0o755))
	existing := filepath.Join(locked, "go.mod")
	mustWriteFile(t, existing, "module x\n")
	created := filepath.Join(locked, "created.go")
	unreadable := filepath.Join(tmp, "unreadable")

	s := newStateManager()
	require.NoError(t, s.TrackAll(existing, created, unreadable))
	require.NoError(t, os.Remove(existing))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "target"), existing))
	mustWriteFile(t, created, "package x")
	require.NoError(t, os.MkdirAll(filepath.Join(unreadable, "sub"), 0o755))
	require.NoError(t, os.Chmod(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })
	require.NoError(t, os.Chmod(locked, 0o555))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	err := s.Revert()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbolic link", "the link cannot be removed")
	assert.FileExists(t, created, "the created file cannot be removed")
}

// TestRevertRestoresSymlink: a tracked path that is a symbolic link is a
// symbolic link again after Revert, with the same target. The target does
// not change. This is also true for a link with a target that does not
// exist, and when a new process loads the state.
func TestRevertRestoresSymlink(t *testing.T) {
	skipWithoutSymlinks(t)
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")

	target := filepath.Join(tmp, "shared", "go.sum")
	mustWriteFile(t, target, "shared sum\n")
	relTarget := filepath.Join("shared", "go.sum")
	linked := filepath.Join(tmp, "go.sum")
	require.NoError(t, os.Symlink(relTarget, linked))
	dangling := filepath.Join(tmp, "dangling.txt")
	danglingTarget := filepath.Join(tmp, "absent.txt")
	require.NoError(t, os.Symlink(danglingTarget, dangling))

	s := newStateManager()
	require.NoError(t, s.TrackAll(linked, dangling))

	snapshot := filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(linked))
	info, err := os.Lstat(snapshot)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the snapshot of a link is a link")

	// The build replaces the link with a regular file, and removes the
	// dangling link.
	require.NoError(t, util.WriteFileAtomic(linked, []byte("changed sum\n")))
	require.NoError(t, os.Remove(dangling))

	loaded, err := loadStateManager()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.NoError(t, loaded.Revert())

	for path, want := range map[string]string{linked: relTarget, dangling: danglingTarget} {
		info, err = os.Lstat(path)
		require.NoError(t, err)
		assert.NotZero(t, info.Mode()&os.ModeSymlink, "%s is a symbolic link again", path)
		got, readErr := os.Readlink(path)
		require.NoError(t, readErr)
		assert.Equal(t, want, got, "%s has the same target", path)
	}
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "shared sum\n", string(data), "the target does not change")
	assert.NoFileExists(t, danglingTarget, "Revert does not create the target of a link")
}

// TestRevertRestoresSymlinkInPlaceOfLink: a tracked link that the build
// changes to a link to a different target gets its old target again. Revert
// does not write through the new link.
func TestRevertRestoresSymlinkInPlaceOfLink(t *testing.T) {
	skipWithoutSymlinks(t)
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")

	original := filepath.Join(tmp, "original.txt")
	mustWriteFile(t, original, "original")
	other := filepath.Join(t.TempDir(), "other.txt")
	mustWriteFile(t, other, "other")
	linked := filepath.Join(tmp, "linked.txt")
	require.NoError(t, os.Symlink(original, linked))

	s := newStateManager()
	require.NoError(t, s.Track(linked))
	require.NoError(t, os.Remove(linked))
	require.NoError(t, os.Symlink(other, linked))

	require.NoError(t, s.Revert())
	got, err := os.Readlink(linked)
	require.NoError(t, err)
	assert.Equal(t, original, got)
	data, err := os.ReadFile(other)
	require.NoError(t, err)
	assert.Equal(t, "other", string(data), "Revert does not write through the new link")
}

// TestRevertRestoresReadOnlyFile: setup keeps the permissions of the files
// that it changes, so a read-only file (for example go.mod or
// vendor/modules.txt) stays read-only. Revert must restore it, with its
// contents and its permissions. A new build must also replace a read-only
// snapshot that an earlier build left.
//
// The test runs on Windows too: there, a rename cannot replace a read-only
// file (see util.RenameReplace).
func TestRevertRestoresReadOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions")
	}
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")

	goMod := filepath.Join(tmp, "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")
	require.NoError(t, os.Chmod(goMod, 0o444))

	s := newStateManager()
	require.NoError(t, s.Track(goMod))
	require.NoError(t, util.WriteFileAtomic(goMod, []byte("module example.com/app\n\nrequire x v1.0.0\n")))

	require.NoError(t, s.Revert())
	data, err := os.ReadFile(goMod)
	require.NoError(t, err)
	assert.Equal(t, "module example.com/app\n", string(data))
	info, err := os.Stat(goMod)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), info.Mode().Perm(), "the permissions are restored")

	// The snapshot of the first build is still there, and it is read-only.
	snapshot := filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(goMod))
	info, err = os.Stat(snapshot)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o444), info.Mode().Perm())

	require.NoError(t, util.WriteFileAtomic(goMod, []byte("module example.com/app\n\ngo 1.25.0\n")))
	next := newStateManager()
	require.NoError(t, next.Track(goMod), "a read-only snapshot of an earlier build is replaced")
	data, err = os.ReadFile(snapshot)
	require.NoError(t, err)
	assert.Equal(t, "module example.com/app\n\ngo 1.25.0\n", string(data))
}

// TestStateScopeModuleRoot: otelc can run in a package directory below the
// module root (for example cmd/app). The manifest then names go.mod, go.sum
// and vendor files at the module root. loadStateManager must accept them,
// and Revert must restore them. A path outside the module stays rejected,
// and Revert does not write through a linked directory of the module.
func TestStateScopeModuleRoot(t *testing.T) {
	modRoot := filepath.Join(t.TempDir(), "mod")
	workDir := filepath.Join(modRoot, "cmd", "app")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	t.Chdir(workDir)
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv("GOWORK", "off")

	goMod := filepath.Join(modRoot, "go.mod")
	goSum := filepath.Join(modRoot, "go.sum")
	modulesTxt := filepath.Join(modRoot, "vendor", "modules.txt")
	newVendorFile := filepath.Join(modRoot, "vendor", "example.com", "dep", "a.go")
	mustWriteFile(t, goMod, "module example.com/mod\n")
	mustWriteFile(t, modulesTxt, "# before\n")

	scope := newStateScope()
	assert.True(t, scope.allows(goMod))
	assert.True(t, scope.allows(filepath.Join(workDir, otelcRuntimeFile)))
	assert.False(t, scope.allows(filepath.Join(filepath.Dir(modRoot), "other", "go.mod")),
		"a path next to the module is not in the scope")
	root, ok := scope.rootOf(filepath.Join(workDir, otelcRuntimeFile))
	require.True(t, ok)
	assert.Equal(t, workDir, root, "the work directory comes before the module root")
	// In the module root, only the paths that otelc tracks there are in the
	// scope.
	for _, rel := range []string{
		goModFileName, goSumFileName, toolFileCanonical, toolFileAlias,
		vendorDirName, filepath.Join(vendorDirName, modulesTxtFileName),
		filepath.Join(vendorDirName, "example.com", "dep", "a.go"),
	} {
		assert.True(t, scope.allows(filepath.Join(modRoot, rel)), rel)
	}
	for _, rel := range []string{
		"private-key.txt", "vendorx", filepath.Join("vendorx", "a.go"),
		filepath.Join("cmd", "other", "main.go"), filepath.Join("sub", goModFileName),
	} {
		assert.False(t, scope.allows(filepath.Join(modRoot, rel)), rel)
	}

	s := newStateManager()
	require.NoError(t, s.TrackAll(goMod, goSum, modulesTxt, newVendorFile))
	mustWriteFile(t, goMod, "module example.com/mod\n\nreplace x => ./x\n")
	mustWriteFile(t, goSum, "x v1.0.0 h1:abc\n")
	mustWriteFile(t, modulesTxt, "# after\n")
	mustWriteFile(t, newVendorFile, "package dep\n")

	loaded, err := loadStateManager()
	require.NoError(t, err, "the manifest can name paths at the module root")
	require.NotNil(t, loaded)
	require.NoError(t, loaded.Revert())

	data, err := os.ReadFile(goMod)
	require.NoError(t, err)
	assert.Equal(t, "module example.com/mod\n", string(data))
	data, err = os.ReadFile(modulesTxt)
	require.NoError(t, err)
	assert.Equal(t, "# before\n", string(data))
	assert.NoFileExists(t, goSum)
	assert.NoFileExists(t, newVendorFile)

	outside := filepath.Join(filepath.Dir(modRoot), "outside.txt")
	mustWriteFile(t, outside, "keep me")
	manifest, err := json.Marshal([]string{"-" + outside})
	require.NoError(t, err)
	mustWriteFile(t, util.GetBuildTemp(stateFileName), string(manifest))
	_, err = loadStateManager()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the otelc work directory")
	assert.FileExists(t, outside)

	if runtime.GOOS == "windows" {
		return
	}
	// The vendor directory at the module root (not tracked) becomes a link
	// to a directory outside the module. Revert must not write through it.
	linkTarget := t.TempDir()
	mustWriteFile(t, filepath.Join(linkTarget, "modules.txt"), "outside")
	s = newStateManager()
	require.NoError(t, s.Track(modulesTxt))
	require.NoError(t, os.RemoveAll(filepath.Join(modRoot, "vendor")))
	require.NoError(t, os.Symlink(linkTarget, filepath.Join(modRoot, "vendor")))
	require.Error(t, s.Revert())
	data, err = os.ReadFile(filepath.Join(linkTarget, "modules.txt"))
	require.NoError(t, err)
	assert.Equal(t, "outside", string(data), "Revert must not write through a linked directory of the module")
}

// TestLoadStateManagerRejectsUntrackedModuleRootPath: otelc runs in a package
// directory below the module root. A forged manifest names a file in the
// module root that otelc never tracks. loadStateManager must reject it, and
// the file must stay.
func TestLoadStateManagerRejectsUntrackedModuleRootPath(t *testing.T) {
	modRoot := t.TempDir()
	workDir := filepath.Join(modRoot, "cmd", "app")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	t.Chdir(workDir)
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv("GOWORK", "off")
	mustWriteFile(t, filepath.Join(modRoot, goModFileName), "module example.com/app\n")
	unrelated := filepath.Join(modRoot, "private-key.txt")
	mustWriteFile(t, unrelated, "keep")

	manifest, err := json.Marshal([]string{"-" + unrelated})
	require.NoError(t, err)
	mustWriteFile(t, util.GetBuildTemp(stateFileName), string(manifest))
	_, err = loadStateManager()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the otelc work directory")
	assert.Contains(t, err.Error(), "is not a go.mod, go.sum, tool or vendor file of its module")
	assert.FileExists(t, unrelated)
}

// TestLoadStateManagerRejectsBuildTempPaths: otelc never tracks a path in the
// build temp directory (.otelc-build). A forged manifest can name such a
// path, for example with a snapshot that is a symbolic link to a directory
// outside the project. loadStateManager must reject it, so that the link is
// not restored (a later extraction of the otelc bundle would write through
// it).
func TestLoadStateManagerRejectsBuildTempPaths(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	t.Setenv(util.EnvOtelcWorkDir, work)
	t.Setenv("GOWORK", "off")

	for _, path := range []string{
		util.GetBuildTempDir(),
		util.GetBuildTemp(unzippedPkgDir),
		filepath.Join(util.GetBuildTemp(unzippedPkgDir), "a.go"),
	} {
		manifest, err := json.Marshal([]string{path})
		require.NoError(t, err)
		mustWriteFile(t, util.GetBuildTemp(stateFileName), string(manifest))
		_, err = loadStateManager()
		require.Error(t, err, path)
		assert.Contains(t, err.Error(), "in the otelc build directory", path)
	}
	assert.False(t, newStateScope().allows(util.GetBuildTemp(unzippedPkgDir)))
	assert.True(t, newStateScope().allows(filepath.Join(work, util.BuildTempDir+"x")),
		"a different directory with the same prefix is in the work directory")

	if runtime.GOOS == "windows" {
		return
	}
	// The probe of the security review: a snapshot of .otelc-build/pkg is a
	// link to a directory outside the project.
	outside := t.TempDir()
	pkg := util.GetBuildTemp(unzippedPkgDir)
	snap := filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(pkg))
	require.NoError(t, os.MkdirAll(filepath.Dir(snap), 0o755))
	require.NoError(t, os.Symlink(outside, snap))
	manifest, err := json.Marshal([]string{pkg})
	require.NoError(t, err)
	mustWriteFile(t, util.GetBuildTemp(stateFileName), string(manifest))
	_, err = loadStateManager()
	require.Error(t, err)
	assert.False(t, lexists(pkg), "the link is not restored")
}

// TestRevertDoesNotRemoveThroughRestoredLink: a forged manifest names a
// directory "linked" (its snapshot is a symbolic link to a directory outside
// the project) and created files in "linked". Revert restores the link. It
// must then not remove the files at the target of the link. Before the fix,
// the result changed with the order of the map, so the test makes many files
// and runs more than one time.
func TestRevertDoesNotRemoveThroughRestoredLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege on Windows")
	}
	const runs, victims = 5, 20
	for range runs {
		work := t.TempDir()
		outside := t.TempDir()
		t.Chdir(work)
		t.Setenv(util.EnvOtelcWorkDir, work)
		t.Setenv("GOWORK", "off")
		parent := filepath.Join(work, "linked")
		mustWriteFile(t, parent, "file")
		snap := filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(parent))
		require.NoError(t, os.MkdirAll(filepath.Dir(snap), 0o755))
		require.NoError(t, os.Symlink(outside, snap))

		entries := make([]string, 0, 1+victims)
		entries = append(entries, parent)
		for i := range victims {
			name := "victim" + strings.Repeat("x", i) + ".txt"
			mustWriteFile(t, filepath.Join(outside, name), "keep")
			entries = append(entries, "-"+filepath.Join(parent, name))
		}
		manifest, err := json.Marshal(entries)
		require.NoError(t, err)
		mustWriteFile(t, util.GetBuildTemp(stateFileName), string(manifest))

		manager, err := loadStateManager()
		require.NoError(t, err)
		require.Error(t, manager.Revert(), "the created paths are below a link now")
		target, err := os.Readlink(parent)
		require.NoError(t, err)
		require.Equal(t, outside, target, "the link is restored")
		for i := range victims {
			require.FileExists(t, filepath.Join(outside, "victim"+strings.Repeat("x", i)+".txt"),
				"Revert must not remove a file through a restored link")
		}
	}
}

// TestReplaceWithCopyErrors: replaceWithCopy copies only a regular file
// that it can read. When it fails, dst does not change.
func TestReplaceWithCopyErrors(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	mustWriteFile(t, dst, "old")

	absent := filepath.Join(dir, "absent")
	err := replaceWithCopy(absent, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading "+absent)

	err = replaceWithCopy(dir, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), dir+" is not a regular file")

	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		src := filepath.Join(dir, "unreadable")
		mustWriteFile(t, src, "new")
		require.NoError(t, os.Chmod(src, 0o000))
		t.Cleanup(func() { _ = os.Chmod(src, 0o644) })
		err = replaceWithCopy(src, dst)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading "+src)
	}
	assert.Equal(t, "old", readAppFile(t, dir, "dst"))
}

// TestReplaceWithSymlinkErrors: when replaceWithSymlink cannot make the
// link, path does not change, and no temporary link stays.
func TestReplaceWithSymlinkErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "link")
	err := replaceWithSymlink("target", path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating a temporary file for "+path)

	skipWithoutSymlinks(t)
	dir := t.TempDir()
	path = filepath.Join(dir, "full")
	mustWriteFile(t, filepath.Join(path, "keep"), "keep")
	err = replaceWithSymlink("target", path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replacing "+path+" with a symbolic link")
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "the temporary link is removed")
	assert.Equal(t, "full", entries[0].Name())
	assert.FileExists(t, filepath.Join(path, "keep"))
}

// TestRestoreSnapshotErrors: restoreSnapshot fails when it cannot make the
// directory of path, or when the snapshot is not a file.
func TestRestoreSnapshotErrors(t *testing.T) {
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "snapshot")
	mustWriteFile(t, snapshot, "content")
	mustWriteFile(t, filepath.Join(dir, "file"), "a file")

	path := filepath.Join(dir, "file", "below", "go.mod")
	err := restoreSnapshot(snapshot, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating the directory of "+path)

	path = filepath.Join(dir, "go.mod")
	err = restoreSnapshot(t.TempDir(), path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restoring "+path)
	assert.NoFileExists(t, path)
}

// TestSnapshotPathAbsent: a path that does not exist has no snapshot.
func TestSnapshotPathAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent")
	dst := filepath.Join(dir, "snapshots", "absent")
	err := snapshotPath(path, dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading "+path)
	assert.NoFileExists(t, dst)
}

// TestRemoveCreatedBelowFile: a path below a regular file is not "not
// found": removeCreated returns the error.
func TestRemoveCreatedBelowFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows, a path below a file is not found")
	}
	file := filepath.Join(t.TempDir(), "file")
	mustWriteFile(t, file, "a file")
	require.Error(t, removeCreated(filepath.Join(file, "below")))
	assert.FileExists(t, file)
}
