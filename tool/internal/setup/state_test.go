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
