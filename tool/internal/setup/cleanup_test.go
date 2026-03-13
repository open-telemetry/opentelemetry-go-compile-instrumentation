// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

func TestCleanup(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T, dir string)
		expectRemoved []string
	}{
		{
			name: "removes all artifacts when they exist",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(dir, OtelcRuntimeFile), "dummy")
				// The instrumentation package is extracted inside .otelc-build/pkg/,
				// not at the project root. It is removed as part of .otelc-build/ cleanup.
				mustWriteFile(t, filepath.Join(dir, util.BuildTempDir, unzippedPkgDir, "a.go"), "dummy")
				mustWriteFile(t, filepath.Join(dir, util.BuildTempDir, "matched.json"), "{}")
			},
			expectRemoved: []string{
				OtelcRuntimeFile,
				util.BuildTempDir,
			},
		},
		{
			name:  "idempotent when no artifacts exist",
			setup: func(_ *testing.T, _ string) {},
			expectRemoved: []string{
				OtelcRuntimeFile,
				util.BuildTempDir,
			},
		},
		{
			name: "partial cleanup when only runtime file exists",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(dir, OtelcRuntimeFile), "dummy")
			},
			expectRemoved: []string{
				OtelcRuntimeFile,
				util.BuildTempDir,
			},
		},
		{
			name: "partial cleanup when only build temp dir exists",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				mustWriteFile(t, filepath.Join(dir, util.BuildTempDir, "matched.json"), "{}")
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

			tt.setup(t, tmpDir)

			err := Cleanup(context.Background())
			if err != nil {
				t.Fatalf("Cleanup() returned unexpected error: %v", err)
			}

			for _, path := range tt.expectRemoved {
				full := filepath.Join(tmpDir, path)
				if _, statErr := os.Stat(full); !os.IsNotExist(statErr) {
					t.Errorf("expected %q to be removed, but it still exists", path)
				}
			}
		})
	}
}

func TestCleanupRestoresBackup(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	const originalContent = "module original.com\n\ngo 1.24.0\n"
	const modifiedContent = "module modified.com\n\ngo 1.24.0\n"

	// Simulate what GoBuild does: write a modified go.mod and a backup of the original.
	mustWriteFile(t, filepath.Join(tmpDir, "go.mod"), modifiedContent)
	mustWriteFile(t, filepath.Join(tmpDir, util.BuildTempDir, "backup", "go.mod"), originalContent)

	err := Cleanup(context.Background())
	if err != nil {
		t.Fatalf("Cleanup() returned unexpected error: %v", err)
	}

	// go.mod should be restored to the original content.
	got, readErr := os.ReadFile(filepath.Join(tmpDir, "go.mod"))
	if readErr != nil {
		t.Fatalf("failed to read go.mod after cleanup: %v", readErr)
	}
	if string(got) != originalContent {
		t.Errorf("go.mod content = %q, want %q", string(got), originalContent)
	}

	// .otelc-build/ should be removed after restoration.
	if _, statErr := os.Stat(filepath.Join(tmpDir, util.BuildTempDir)); !os.IsNotExist(statErr) {
		t.Error("expected .otelc-build/ to be removed after cleanup")
	}
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
