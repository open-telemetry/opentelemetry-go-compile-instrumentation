// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupFileFromDir_SingleModule(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvOtelcWorkDir, tmpDir)
	if err := os.MkdirAll(GetBuildTempDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a go.mod in the work dir (simulating cwd == module root).
	gomod := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(gomod, []byte("module example.com/app\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := BackupFileFromDir([]string{"go.mod"}, tmpDir); err != nil {
		t.Fatalf("BackupFileFromDir: %v", err)
	}

	// Backup should be at .otelc-build/backup/./go.mod (relative "." from cwd==tmpDir)
	backed := GetBuildTemp(filepath.Join("backup", ".", "go.mod"))
	if _, err := os.Stat(backed); os.IsNotExist(err) {
		t.Fatalf("backup file not found at %s", backed)
	}
}

func TestBackupFileFromDir_MultiModule(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvOtelcWorkDir, root)
	if err := os.MkdirAll(GetBuildTempDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a nested module directory.
	subDir := filepath.Join(root, "sub", "module")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := filepath.Join(subDir, "go.mod")
	if err := os.WriteFile(gomod, []byte("module example.com/sub\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	if err := BackupFileFromDir([]string{"go.mod"}, subDir); err != nil {
		t.Fatalf("BackupFileFromDir: %v", err)
	}

	// Backup must be namespaced under the relative path "sub/module".
	backed := GetBuildTemp(filepath.Join("backup", "sub", "module", "go.mod"))
	if _, err := os.Stat(backed); os.IsNotExist(err) {
		t.Fatalf("backup file not found at %s", backed)
	}
}

func TestRestoreAllBackedUpFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvOtelcWorkDir, root)
	if err := os.MkdirAll(GetBuildTempDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a nested module directory with a go.mod.
	subDir := filepath.Join(root, "nested")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "module example.com/nested\n\ngo 1.21\n"
	gomod := filepath.Join(subDir, "go.mod")
	if err := os.WriteFile(gomod, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	// Back up the module file.
	if err := BackupFileFromDir([]string{"go.mod"}, subDir); err != nil {
		t.Fatalf("BackupFileFromDir: %v", err)
	}

	// Overwrite the original to simulate a modification.
	if err := os.WriteFile(gomod, []byte("modified content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Restore and verify content is back.
	if err := RestoreAllBackedUpFiles(); err != nil {
		t.Fatalf("RestoreAllBackedUpFiles: %v", err)
	}

	data, err := os.ReadFile(gomod)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Errorf("go.mod content after restore = %q, want %q", string(data), original)
	}
}
