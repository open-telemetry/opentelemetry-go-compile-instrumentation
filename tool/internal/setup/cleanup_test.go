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

func TestCleanup(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T, dir string) context.Context
		expectRemoved []string
	}{
		{
			name: "removes all artifacts when they exist",
			setup: func(t *testing.T, dir string) context.Context {
				t.Helper()
				// track otelc.runtime.go in the state manager so it is removed when Cleanup is called
				stateManager := newStateManager()
				otelcRuntimeGoPath := filepath.Join(dir, otelcRuntimeFile)
				require.NoError(t, stateManager.Track(otelcRuntimeGoPath))
				mustWriteFile(t, otelcRuntimeGoPath, "package main \n\n// dummy runtime file")
				// The instrumentation package is extracted inside .otelc-build/pkg/,
				// not at the project root. It is removed as part of .otelc-build/ cleanup.
				mustWriteFile(t, filepath.Join(dir, util.BuildTempDir, unzippedPkgDir, "a.go"), "dummy")
				mustWriteFile(t, filepath.Join(dir, util.BuildTempDir, "matched.json"), "{}")
				return contextWithStateManager(t.Context(), stateManager)
			},
			expectRemoved: []string{
				otelcRuntimeFile,
				util.BuildTempDir,
			},
		},
		{
			name:  "idempotent when no artifacts exist",
			setup: func(_ *testing.T, _ string) context.Context { return t.Context() },
			expectRemoved: []string{
				otelcRuntimeFile,
				util.BuildTempDir,
			},
		},
		{
			name: "partial cleanup when only runtime file exists",
			setup: func(t *testing.T, dir string) context.Context {
				t.Helper()
				stateManager := newStateManager()
				otelcRuntimeGoPath := filepath.Join(dir, otelcRuntimeFile)
				require.NoError(t, stateManager.Track(otelcRuntimeGoPath))
				mustWriteFile(t, otelcRuntimeGoPath, "package main\n\n// dummy runtime file")
				return contextWithStateManager(t.Context(), stateManager)
			},
			expectRemoved: []string{
				otelcRuntimeFile,
				util.BuildTempDir,
			},
		},
		{
			name: "partial cleanup when only build temp dir exists",
			setup: func(t *testing.T, dir string) context.Context {
				t.Helper()
				mustWriteFile(t, filepath.Join(dir, util.BuildTempDir, "matched.json"), "{}")
				return t.Context()
			},
			expectRemoved: []string{
				otelcRuntimeFile,
				util.BuildTempDir,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Chdir(tmpDir)

			ctx := tt.setup(t, tmpDir)

			err := Cleanup(ctx, true)
			if err != nil {
				t.Fatalf("Cleanup() returned unexpected error: %v", err)
			}

			for _, path := range tt.expectRemoved {
				full := filepath.Join(tmpDir, path)
				if util.PathExists(full) {
					t.Errorf("expected %q to be removed, but it still exists", path)
				}
			}
		})
	}
}

func TestCleanupRestoresState(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	const originalContent = "module original.com\n\ngo 1.24.0\n"
	const modifiedContent = "module modified.com\n\ngo 1.24.0\n"

	goModPath := filepath.Join(tmpDir, "go.mod")
	otelcRuntimeGoPath := filepath.Join(tmpDir, otelcRuntimeFile)
	stateJSONContent, err := json.Marshal([]string{goModPath, "-" + otelcRuntimeGoPath})
	if err != nil {
		t.Fatalf("failed to marshal state to JSON: %v", err)
	}

	// Simulate what GoBuild does: write an otelc.runtime.go file,
	// then write a modified go.mod and a backup of the original.
	mustWriteFile(t, otelcRuntimeGoPath, "package main\n")
	mustWriteFile(t, goModPath, modifiedContent)
	mustWriteFile(
		t,
		filepath.Join(tmpDir, util.BuildTempDir, stateDir, stateSnapshotPath(goModPath)),
		originalContent,
	)
	mustWriteFile(t, filepath.Join(tmpDir, util.BuildTempDir, stateFileName), string(stateJSONContent))

	if err = Cleanup(t.Context(), true); err != nil {
		t.Fatalf("Cleanup() returned unexpected error: %v", err)
	}

	// go.mod should be restored to the original content.
	got, readErr := os.ReadFile(goModPath)
	if readErr != nil {
		t.Fatalf("failed to read go.mod after cleanup: %v", readErr)
	}
	if string(got) != originalContent {
		t.Errorf("go.mod content = %q, want %q", string(got), originalContent)
	}

	// otelc.runtime.go should be removed after cleanup.
	if util.PathExists(otelcRuntimeGoPath) {
		t.Errorf("expected otelc.runtime.go to be removed after cleanup, but it still exists at %s", otelcRuntimeGoPath)
	}

	// .otelc-build/ should be removed after restoration.
	if util.PathExists(filepath.Join(tmpDir, util.BuildTempDir)) {
		t.Error("expected .otelc-build/ to be removed after cleanup")
	}
}

func TestCleanupRestoresMultiModState(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	type fileCase struct {
		path     string
		original string
		modified string
	}

	files := []fileCase{
		{
			path:     filepath.Join(tmpDir, "go.work.sum"),
			original: "// original go.work.sum content",
			modified: "// modified go.work.sum content",
		},
		{
			path:     filepath.Join(tmpDir, "pkgA", "go.mod"),
			original: "module original.pkga.com\n\ngo 1.24.0\n",
			modified: "module modified.pkga.com\n\ngo 1.24.0\n",
		},
		{
			path:     filepath.Join(tmpDir, "pkgA", otelcRuntimeFile),
			original: "",
			modified: "package main\n",
		},
		{
			path:     filepath.Join(tmpDir, "pkgB", "go.mod"),
			original: "module original.pkgb.com\n\ngo 1.24.0\n",
			modified: "module modified.pkgb.com\n\ngo 1.24.0\n",
		},
		{
			path:     filepath.Join(tmpDir, "pkgB", otelcRuntimeFile),
			original: "",
			modified: "package main\n",
		},
	}

	mustWriteFile(t, filepath.Join(tmpDir, "go.work"), "go 1.24\n\nuse ./pkgA\nuse ./pkgB")

	statePaths := make([]string, 0, len(files))
	for _, f := range files {
		mustWriteFile(t, f.path, f.modified)
		if f.original != "" {
			statePath := filepath.Join(tmpDir, util.BuildTempDir, stateDir, stateSnapshotPath(f.path))
			mustWriteFile(t, statePath, f.original)
			statePaths = append(statePaths, f.path)
		} else {
			statePaths = append(statePaths, "-"+f.path)
		}
	}

	stateJSONContent, err := json.Marshal(statePaths)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	mustWriteFile(t, filepath.Join(tmpDir, util.BuildTempDir, stateFileName), string(stateJSONContent))

	if err = Cleanup(t.Context(), false); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	for _, f := range files {
		if f.original == "" {
			if util.PathExists(f.path) {
				t.Errorf("%s should not exist", f.path)
			}
			continue
		}

		got, readErr := os.ReadFile(f.path)
		if readErr != nil {
			t.Fatalf("read %s: %v", f.path, readErr)
		}

		if string(got) != f.original {
			t.Errorf("%s content = %q, want %q", f.path, string(got), f.original)
		}
	}
}

func TestCleanupKeepsBuildDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	const originalContent = "module original.com\n\ngo 1.24.0\n"
	const modifiedContent = "module modified.com\n\ngo 1.24.0\n"

	goModPath := filepath.Join(tmpDir, "go.mod")
	otelcRuntimeGoPath := filepath.Join(tmpDir, otelcRuntimeFile)
	stateJSONContent, err := json.Marshal([]string{goModPath, "-" + otelcRuntimeGoPath})
	if err != nil {
		t.Fatalf("failed to marshal backup state to JSON: %v", err)
	}

	// Simulate a completed build: modified go.mod, backup, runtime file, and build artifacts.
	mustWriteFile(t, goModPath, modifiedContent)
	mustWriteFile(
		t,
		filepath.Join(tmpDir, util.BuildTempDir, stateDir, stateSnapshotPath(goModPath)),
		originalContent,
	)
	mustWriteFile(t, filepath.Join(tmpDir, util.BuildTempDir, stateFileName), string(stateJSONContent))
	mustWriteFile(t, filepath.Join(tmpDir, util.BuildTempDir, "matched.json"), "{}")
	mustWriteFile(t, otelcRuntimeGoPath, "package main \n\n// dummy runtime file")

	err = Cleanup(t.Context(), false)
	if err != nil {
		t.Fatalf("Cleanup(cleanAll=false) returned unexpected error: %v", err)
	}

	// go.mod should be restored to the original content.
	got, readErr := os.ReadFile(goModPath)
	if readErr != nil {
		t.Fatalf("failed to read go.mod after cleanup: %v", readErr)
	}
	if string(got) != originalContent {
		t.Errorf("go.mod content = %q, want %q", string(got), originalContent)
	}

	// otelc.runtime.go should be removed.
	if util.PathExists(filepath.Join(tmpDir, otelcRuntimeFile)) {
		t.Error("expected otelc.runtime.go to be removed after Cleanup(cleanAll=false)")
	}

	// .otelc-build/ should still exist (kept for debugging).
	if !util.PathExists(filepath.Join(tmpDir, util.BuildTempDir)) {
		t.Error("expected .otelc-build/ to be kept after Cleanup(cleanAll=false), but it was removed")
	}
}

func TestCleanupKeepsBuildDirNoArtifacts(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// No artifacts exist — Cleanup(cleanAll=false) should not panic or fail.
	err := Cleanup(t.Context(), false)
	if err != nil {
		t.Fatalf("Cleanup(cleanAll=false) returned unexpected error: %v", err)
	}
}

func TestCleanup_RecoversAfterCrash(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	goMod := filepath.Join(tmp, "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")

	dying := newStateManager()
	require.NoError(t, dying.Track(goMod))
	mustWriteFile(t, goMod, "module example.com/app\n\nreplace x => ./.otelc-build/x\n")

	// Fresh process, no state manager in the context: Cleanup must load the
	// manifest from disk, restore the tree, and discard the consumed state.
	require.NoError(t, Cleanup(t.Context(), false))

	content, err := os.ReadFile(goMod)
	require.NoError(t, err)
	assert.Equal(t, "module example.com/app\n", string(content))
	assert.False(t, util.PathExists(util.GetBuildTemp(stateFileName)), "consumed manifest must be discarded")
	assert.False(t, util.PathExists(util.GetBuildTemp(stateDir)), "consumed snapshots must be discarded")
}

func TestCleanup_KeepsSnapshotsWhenRevertFails(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	goMod := filepath.Join(tmp, "go.mod")
	otherFile := filepath.Join(tmp, "other.txt")
	mustWriteFile(t, goMod, "module example.com/app\n")
	mustWriteFile(t, otherFile, "keep me\n")

	dying := newStateManager()
	require.NoError(t, dying.Track(goMod))
	require.NoError(t, dying.Track(otherFile))

	// Sabotage one snapshot so Revert fails partially.
	require.NoError(t, os.Remove(filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(otherFile))))

	err := Cleanup(t.Context(), true)
	require.Error(t, err, "a failed restore must be reported")
	assert.Contains(t, err.Error(), "otelc cleanup", "the error tells how to try again")

	// Even with cleanAll, a failed revert must not delete the remaining
	// snapshots as they are the only path left to recovery.
	assert.True(t, util.PathExists(util.GetBuildTemp(stateFileName)),
		"manifest must survive a failed revert even with cleanAll")
	assert.True(t, util.PathExists(filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(goMod))),
		"snapshots must survive a failed revert even with cleanAll")
}

func TestRecoverLeftoverState(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)

	require.NoError(t, recoverLeftoverState(t.Context()), "no manifest: nothing to do")

	goMod := filepath.Join(tmp, "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")
	dying := newStateManager()
	require.NoError(t, dying.Track(goMod))
	mustWriteFile(t, goMod, "module example.com/app\n\nreplace x => ./.otelc-build/x\n")

	require.NoError(t, recoverLeftoverState(t.Context()))
	content, err := os.ReadFile(goMod)
	require.NoError(t, err)
	assert.Equal(t, "module example.com/app\n", string(content))
	assert.False(t, util.PathExists(util.GetBuildTemp(stateFileName)), "consumed manifest must be discarded")
}

func TestRecoverLeftoverStateFailureKeepsManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)

	goMod := filepath.Join(tmp, "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")
	dying := newStateManager()
	require.NoError(t, dying.Track(goMod))
	require.NoError(t, os.Remove(filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(goMod))))

	err := recoverLeftoverState(t.Context())
	require.Error(t, err, "a build must not start when the old changes cannot be restored")
	assert.Contains(t, err.Error(), "previous otelc build")
	assert.True(t, util.PathExists(util.GetBuildTemp(stateFileName)), "the manifest must stay for recovery")
}

// mustWriteFile creates a file with the given content, creating parent dirs as needed.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create dirs for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

// TestBuildAndSetupFailOnBadManifest: a manifest that otelc cannot trust
// stops otelc go build and a standalone otelc setup before they change
// anything.
func TestBuildAndSetupFailOnBadManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	t.Setenv("GOWORK", "off")
	outside := filepath.Join(t.TempDir(), "x")
	manifest, err := json.Marshal([]string{"-" + outside})
	require.NoError(t, err)
	mustWriteFile(t, util.GetBuildTemp(stateFileName), string(manifest))

	err = GoBuild(t.Context(), newParsedCommand(t, "go", "build", "."))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the otelc work directory")

	err = Setup(t.Context(), newParsedCommand(t, "setup"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the otelc work directory")
}

// newParsedCommand returns the command name after it parsed args (args[0]
// is the name).
func newParsedCommand(t *testing.T, args ...string) *cli.Command {
	t.Helper()
	var parsed *cli.Command
	cmd := &cli.Command{
		Name:            args[0],
		SkipFlagParsing: true,
		Action: func(_ context.Context, c *cli.Command) error {
			parsed = c
			return nil
		},
	}
	require.NoError(t, cmd.Run(t.Context(), args))
	return parsed
}

// TestCleanupFromPackageDir: otelc cleanup in a package directory below the
// module root restores go.mod at the module root from the manifest.
func TestCleanupFromPackageDir(t *testing.T) {
	modRoot := t.TempDir()
	workDir := filepath.Join(modRoot, "cmd", "app")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	t.Chdir(workDir)
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv("GOWORK", "off")

	goMod := filepath.Join(modRoot, "go.mod")
	mustWriteFile(t, goMod, "module example.com/mod\n")
	s := newStateManager()
	require.NoError(t, s.Track(goMod))
	mustWriteFile(t, goMod, "module example.com/mod\n\nreplace x => ./x\n")

	require.NoError(t, Cleanup(t.Context(), true))
	data, err := os.ReadFile(goMod)
	require.NoError(t, err)
	assert.Equal(t, "module example.com/mod\n", string(data))
	assert.NoDirExists(t, util.GetBuildTempDir())
}

// TestRevertStateDiscardFails: the files are restored, but the state cannot
// be removed. The error names the state directory, and the manifest stays.
func TestRevertStateDiscardFails(t *testing.T) {
	skipWithoutPermissions(t)
	tmp := t.TempDir()
	t.Chdir(tmp)
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	goMod := filepath.Join(tmp, goModFileName)
	mustWriteFile(t, goMod, "module example.com/app\n")
	s := newStateManager()
	require.NoError(t, s.Track(goMod))
	mustWriteFile(t, goMod, "module example.com/changed\n")
	makeReadOnly(t, util.GetBuildTempDir())

	err := revertState(s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing the restored state in "+util.GetBuildTemp(stateDir))
	assert.Equal(t, "module example.com/app\n", readAppFile(t, tmp, goModFileName), "go.mod is restored")
	assert.FileExists(t, util.GetBuildTemp(stateFileName))
}
