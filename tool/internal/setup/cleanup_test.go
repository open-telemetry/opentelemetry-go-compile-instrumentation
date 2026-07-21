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
				stateManager := NewStateManager()
				otelcRuntimeGoPath := filepath.Join(dir, OtelcRuntimeFile)
				stateManager.Track(otelcRuntimeGoPath)
				mustWriteFile(t, otelcRuntimeGoPath, "package main \n\n// dummy runtime file")
				// The instrumentation package is extracted inside .otelc-build/pkg/,
				// not at the project root. It is removed as part of .otelc-build/ cleanup.
				mustWriteFile(t, filepath.Join(dir, util.BuildTempDir, unzippedPkgDir, "a.go"), "dummy")
				mustWriteFile(t, filepath.Join(dir, util.BuildTempDir, "matched.json"), "{}")
				return ContextWithStateManager(t.Context(), stateManager)
			},
			expectRemoved: []string{
				OtelcRuntimeFile,
				util.BuildTempDir,
			},
		},
		{
			name:  "idempotent when no artifacts exist",
			setup: func(_ *testing.T, _ string) context.Context { return t.Context() },
			expectRemoved: []string{
				OtelcRuntimeFile,
				util.BuildTempDir,
			},
		},
		{
			name: "partial cleanup when only runtime file exists",
			setup: func(t *testing.T, dir string) context.Context {
				t.Helper()
				stateManager := NewStateManager()
				otelcRuntimeGoPath := filepath.Join(dir, OtelcRuntimeFile)
				stateManager.Track(otelcRuntimeGoPath)
				mustWriteFile(t, otelcRuntimeGoPath, "package main\n\n// dummy runtime file")
				return ContextWithStateManager(t.Context(), stateManager)
			},
			expectRemoved: []string{
				OtelcRuntimeFile,
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
				OtelcRuntimeFile,
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
	otelcRuntimeGoPath := filepath.Join(tmpDir, OtelcRuntimeFile)
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
			path:     filepath.Join(tmpDir, "pkgA", OtelcRuntimeFile),
			original: "",
			modified: "package main\n",
		},
		{
			path:     filepath.Join(tmpDir, "pkgB", "go.mod"),
			original: "module original.pkgb.com\n\ngo 1.24.0\n",
			modified: "module modified.pkgb.com\n\ngo 1.24.0\n",
		},
		{
			path:     filepath.Join(tmpDir, "pkgB", OtelcRuntimeFile),
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
	otelcRuntimeGoPath := filepath.Join(tmpDir, OtelcRuntimeFile)
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
	if util.PathExists(filepath.Join(tmpDir, OtelcRuntimeFile)) {
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

	dying := NewStateManager()
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

	dying := NewStateManager()
	require.NoError(t, dying.Track(goMod))
	require.NoError(t, dying.Track(otherFile))

	// Sabotage one snapshot so Revert fails partially.
	require.NoError(t, os.Remove(filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(otherFile))))

	require.NoError(t, Cleanup(t.Context(), true))

	// Even with cleanAll, a failed revert must not delete the remaining
	// snapshots as they are the only path left to recovery.
	assert.True(t, util.PathExists(util.GetBuildTemp(stateFileName)),
		"manifest must survive a failed revert even with cleanAll")
	assert.True(t, util.PathExists(filepath.Join(util.GetBuildTemp(stateDir), stateSnapshotPath(goMod))),
		"snapshots must survive a failed revert even with cleanAll")
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
