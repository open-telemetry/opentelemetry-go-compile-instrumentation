// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCmd(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		expectErr bool
	}{
		{
			name:      "simple go command",
			args:      []string{"go", "version"},
			expectErr: false,
		},
		{
			name:      "command with multiple arguments",
			args:      []string{"go", "env", "GOOS"},
			expectErr: false,
		},
		{
			name:      "nonexistent command",
			args:      []string{"nonexistent-command-xyz"},
			expectErr: true,
		},
		{
			name:      "empty command slice",
			args:      []string{},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunCmd(t.Context(), tt.args...)
			if (err != nil) != tt.expectErr {
				t.Errorf("RunCmd() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestRunCmdEmptyArgs(t *testing.T) {
	err := RunCmd(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no command specified")
}

func TestRunCmdWithEnv(t *testing.T) {
	programPath := filepath.Join(t.TempDir(), "check_env.go")
	err := os.WriteFile(programPath, []byte(`package main

import "os"

func main() {
	if os.Getenv("TEST_VAR") == "test_value" {
		os.Exit(0)
	}
	os.Exit(1)
}
`), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test program: %v", err)
	}

	t.Run("passes environment variable to subprocess", func(t *testing.T) {
		env := append(os.Environ(), "TEST_VAR=test_value")
		err = RunCmdWithEnv(t.Context(), env, "go", "run", programPath)
		if err != nil {
			t.Errorf("Expected success when TEST_VAR is set, got: %v", err)
		}
	})

	t.Run("fails when required variable is missing", func(t *testing.T) {
		env := append(os.Environ(), "OTHER_VAR=other_value")
		err = RunCmdWithEnv(t.Context(), env, "go", "run", programPath)
		if err == nil {
			t.Error("Expected failure when TEST_VAR is not set")
		}
	})

	t.Run("works with multiple environment variables", func(t *testing.T) {
		env := append(os.Environ(), "TEST_VAR=test_value", "OTHER_VAR=other_value")
		err = RunCmdWithEnv(t.Context(), env, "go", "run", programPath)
		if err != nil {
			t.Errorf("Expected success with multiple env vars, got: %v", err)
		}
	})
}

func TestRunCmdInDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	tests := []struct {
		name      string
		dir       string
		expectErr bool
	}{
		{
			name:      "run command in valid directory",
			dir:       tmpDir,
			expectErr: false,
		},
		{
			name:      "run command in subdirectory",
			dir:       subDir,
			expectErr: false,
		},
		{
			name:      "run command in nonexistent directory",
			dir:       filepath.Join(tmpDir, "nonexistent"),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunCmdInDir(t.Context(), tt.dir, "go", "version")
			if (err != nil) != tt.expectErr {
				t.Errorf("RunCmdInDir() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestRunCmdErrorMessages(t *testing.T) {
	t.Run("error message includes command path", func(t *testing.T) {
		err := RunCmd(t.Context(), "nonexistent-command-xyz", "arg1", "arg2")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, "nonexistent-command-xyz") {
			t.Errorf("Error message should contain command name, got: %s", errMsg)
		}
	})

	t.Run("error message includes directory for RunCmdInDir", func(t *testing.T) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get current working directory: %v", err)
		}
		dir := filepath.Join(cwd, "nonexistent", "dir")
		err = RunCmdInDir(t.Context(), dir, "go", "version")
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, dir) {
			t.Errorf("Error message should contain directory %q, got: %s", dir, errMsg)
		}
	})
}

func TestBuildFlagsRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
	}{
		{
			name:  "simple flags",
			flags: []string{"-race", "-tags=foo"},
		},
		{
			name:  "flags with spaces in values",
			flags: []string{"-tags", "foo bar baz"},
		},
		{
			name:  "modfile with spaces in path",
			flags: []string{"-modfile", "/path/with spaces/go.mod"},
		},
		{
			name:  "mixed flags with spaces",
			flags: []string{"-race", "-tags", "integration e2e", "-mod=vendor"},
		},
		{
			name:  "empty flags",
			flags: []string{},
		},
		{
			name:  "nil flags",
			flags: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeBuildFlags(tt.flags)
			if len(tt.flags) == 0 {
				if encoded != "" {
					t.Errorf("EncodeBuildFlags(%v) = %q, expected empty", tt.flags, encoded)
				}
				return
			}

			// Set environment variable and read back
			t.Setenv(EnvOtelcBuildFlags, encoded)
			result := GetBuildFlags()

			if len(result) != len(tt.flags) {
				t.Errorf("GetBuildFlags() returned %d flags, expected %d", len(result), len(tt.flags))
				return
			}

			for i, f := range tt.flags {
				if result[i] != f {
					t.Errorf("GetBuildFlags()[%d] = %q, expected %q", i, result[i], f)
				}
			}
		})
	}
}

func TestGetBuildFlags_InvalidJSON(t *testing.T) {
	t.Setenv(EnvOtelcBuildFlags, "not valid json")
	result := GetBuildFlags()
	if result != nil {
		t.Errorf("GetBuildFlags() with invalid JSON should return nil, got %v", result)
	}
}

func TestGetBuildFlags_Empty(t *testing.T) {
	// Ensure env var is not set
	t.Setenv(EnvOtelcBuildFlags, "")
	result := GetBuildFlags()
	if result != nil {
		t.Errorf("GetBuildFlags() with empty env should return nil, got %v", result)
	}
}

func TestListFiles_HiddenFileDoesNotSkipSiblings(t *testing.T) {
	tmpDir := t.TempDir()

	visible1 := filepath.Join(tmpDir, "visible1.txt")
	hidden := filepath.Join(tmpDir, ".hidden")
	visible2 := filepath.Join(tmpDir, "visible2.txt")

	for _, file := range []string{visible1, hidden, visible2} {
		err := os.WriteFile(file, []byte("test"), 0o644)
		if err != nil {
			t.Fatalf("failed to create test file %s: %v", file, err)
		}
	}

	files, err := ListFiles(tmpDir)
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}

	var foundVisible1, foundVisible2 bool

	for _, file := range files {
		switch filepath.Base(file) {
		case "visible1.txt":
			foundVisible1 = true
		case "visible2.txt":
			foundVisible2 = true
		case ".hidden":
			t.Fatalf("hidden file should not be returned")
		}
	}

	if !foundVisible1 || !foundVisible2 {
		t.Fatalf("expected visible sibling files to be returned, got %v", files)
	}
}

func TestListFiles_SkipsHiddenDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	hiddenDir := filepath.Join(tmpDir, ".git")
	err := os.MkdirAll(hiddenDir, 0o755)
	if err != nil {
		t.Fatalf("failed to create hidden directory: %v", err)
	}

	err = os.WriteFile(filepath.Join(hiddenDir, "config"), []byte("test"), 0o644)
	if err != nil {
		t.Fatalf("failed to create hidden file: %v", err)
	}

	visible := filepath.Join(tmpDir, "visible.txt")
	err = os.WriteFile(visible, []byte("test"), 0o644)
	if err != nil {
		t.Fatalf("failed to create visible file: %v", err)
	}

	files, err := ListFiles(tmpDir)
	if err != nil {
		t.Fatalf("ListFiles() error = %v", err)
	}

	for _, file := range files {
		if strings.Contains(file, ".git") {
			t.Fatalf("hidden directory contents should not be returned: %v", files)
		}
	}

	var foundVisible bool

	for _, file := range files {
		if filepath.Base(file) == "visible.txt" {
			foundVisible = true
		}
	}

	if !foundVisible {
		t.Fatalf("expected visible file to be returned")
	}
}

func TestListFiles_HiddenRoot(t *testing.T) {
	tmpDir := t.TempDir()

	hiddenDir := filepath.Join(tmpDir, ".hidden")
	require.NoError(t, os.MkdirAll(hiddenDir, 0o755))

	file := filepath.Join(hiddenDir, "file.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello"), 0o644))

	files, err := ListFiles(hiddenDir)
	require.NoError(t, err)

	require.Len(t, files, 1)
	require.Equal(t, file, files[0])
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")
	content := []byte("hello world")

	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("failed to write src: %v", err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read dst: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("got content %q, want %q", string(got), string(content))
	}
}

func TestCopyFileNestedDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "src_nested.txt")
	dst := filepath.Join(tmpDir, "a", "b", "c", "dst_nested.txt")
	content := []byte("nested hello")

	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("failed to write src: %v", err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read dst: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("got content %q, want %q", string(got), string(content))
	}
}

func TestCopyFilePreservesPermissions(t *testing.T) {
	if IsWindows() {
		t.Skip("Skipping permission test on Windows")
	}

	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "src_perms.txt")
	dst := filepath.Join(tmpDir, "dst_perms.txt")

	if err := os.WriteFile(src, []byte("perms"), 0o700); err != nil {
		t.Fatalf("failed to write src: %v", err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("failed to stat dst: %v", err)
	}

	if info.Mode().Perm() != 0o700 {
		t.Errorf("got perm %o, want %o", info.Mode().Perm(), 0o700)
	}
}

func TestCopyFileSourceDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()

	err := CopyFile(
		filepath.Join(tmpDir, "nonexistent"),
		filepath.Join(tmpDir, "out"),
	)

	if err == nil {
		t.Error("expected error for nonexistent source")
	}
}

func TestCopyFileSameFile(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "same.txt")
	content := []byte("hello")

	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("failed to write src: %v", err)
	}

	if err := CopyFile(src, src); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	got, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("failed to read src: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	for _, tt := range []struct {
		name        string
		initialData []byte
		initialPerm os.FileMode
		writePerm   []os.FileMode
		wantPerm    os.FileMode
	}{
		{
			name:      "new file uses default permissions",
			writePerm: nil,
			wantPerm:  0o644,
		},
		{
			name:      "new file uses provided permissions",
			writePerm: []os.FileMode{0o600},
			wantPerm:  0o600,
		},
		{
			name:        "existing file preserves permissions",
			initialData: []byte("old"),
			initialPerm: 0o755,
			writePerm:   nil,
			wantPerm:    0o755,
		},
		{
			name:        "existing file uses provided permissions",
			initialData: []byte("old"),
			initialPerm: 0o755,
			writePerm:   []os.FileMode{0o600},
			wantPerm:    0o600,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.txt")

			if tt.initialData != nil {
				require.NoError(t, os.WriteFile(path, tt.initialData, tt.initialPerm))
				require.NoError(t, os.Chmod(path, tt.initialPerm))
			}
			require.NoError(t, WriteFileAtomic(path, []byte("new content"), tt.writePerm...))

			got, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			require.Equal(t, []byte("new content"), got)

			if runtime.GOOS != "windows" {
				info, statErr := os.Stat(path)
				require.NoError(t, statErr)
				require.Equal(t, tt.wantPerm, info.Mode().Perm())
			}

			matches, matchesErr := filepath.Glob(filepath.Join(filepath.Dir(path), filepath.Base(path)+".tmp-*"))
			require.NoError(t, matchesErr)
			require.Empty(t, matches)
		})
	}
}

func TestCRC32(t *testing.T) {
	// CRC32 is deterministic and returns a decimal string.
	got := CRC32("hello")
	assert.Equal(t, CRC32("hello"), got, "must be deterministic")
	assert.NotEqual(t, CRC32("hello"), CRC32("world"), "distinct inputs differ")

	// The empty string hashes to 0.
	assert.Equal(t, "0", CRC32(""))
}

func TestIsUnixAndIsWindows(t *testing.T) {
	// Exactly the current platform family must report true; the two are
	// mutually exclusive on every supported OS.
	assert.NotEqual(t, isUnix(), IsWindows())
}

func TestNormalizePath(t *testing.T) {
	// NormalizePath cleans and converts separators to forward slashes.
	assert.Equal(t, "a/b/c", NormalizePath("a/b/./c"))
	assert.Equal(t, "a/c", NormalizePath("a/b/../c"))
	assert.Equal(t, ".", NormalizePath(""))
}

// windowsRename is os.Rename with a rule of Windows: it cannot replace a
// read-only file.
func windowsRename(oldPath, newPath string) error {
	if info, err := os.Lstat(newPath); err == nil && info.Mode().Perm()&0o200 == 0 {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: os.ErrPermission}
	}
	return os.Rename(oldPath, newPath)
}

// TestRenameReplaceReadOnly: on Windows, a rename cannot replace a read-only
// file. renameReplace makes the file writable and does the rename again.
// The new file keeps its own permissions. When the second rename fails too,
// the old file keeps its permissions.
func TestRenameReplaceReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions")
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "go.mod")
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o444))
	src := filepath.Join(dir, "go.mod.tmp")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o444))

	require.Error(t, renameReplace(src, dst, windowsRename, false, hasOneLink),
		"not Windows: no second rename")
	require.NoError(t, renameReplace(src, dst, windowsRename, true, hasOneLink))
	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), info.Mode().Perm(), "the new file keeps its permissions")
	assert.NoFileExists(t, src)

	alwaysFails := func(string, string) error { return os.ErrPermission }
	require.NoError(t, os.WriteFile(src, []byte("next"), 0o644))
	require.ErrorIs(t, renameReplace(src, dst, alwaysFails, true, hasOneLink), os.ErrPermission)
	info, err = os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), info.Mode().Perm(), "the permissions are set back")
	data, err = os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

// TestRenameReplaceReadOnlyHardLink: on Windows, when the read-only file is
// a hard link to a different file (for example a file outside the project),
// renameReplace must not make it writable: the permissions belong to the
// file, so the other link would stay writable. The rename fails instead.
func TestRenameReplaceReadOnlyHardLink(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions")
	}
	dir := t.TempDir()
	other := filepath.Join(t.TempDir(), "outside.txt")
	require.NoError(t, os.WriteFile(other, []byte("outside"), 0o444))
	dst := filepath.Join(dir, "go.mod")
	if err := os.Link(other, dst); err != nil {
		t.Skipf("no hard links: %v", err)
	}
	src := filepath.Join(dir, "go.mod.tmp")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
	assert.False(t, hasOneLink(dst), "dst has two links")

	require.ErrorIs(t, renameReplace(src, dst, windowsRename, true, hasOneLink), os.ErrPermission)
	info, err := os.Stat(other)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o444), info.Mode().Perm(), "the other link stays read-only")
	data, err := os.ReadFile(other)
	require.NoError(t, err)
	assert.Equal(t, "outside", string(data))
}

// TestHasOneLink: hasOneLink counts the hard links of a file.
func TestHasOneLink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a")
	require.NoError(t, os.WriteFile(path, []byte("a"), 0o644))
	assert.True(t, hasOneLink(path))
	assert.False(t, hasOneLink(filepath.Join(dir, "absent")), "a file that does not exist")
	if err := os.Link(path, filepath.Join(dir, "b")); err != nil {
		t.Skipf("no hard links: %v", err)
	}
	assert.False(t, hasOneLink(path))
}

// TestRenameReplaceOtherErrors: on Windows, renameReplace tries again only
// for a read-only file that it can make writable. Else it returns the error
// of the rename.
func TestRenameReplaceOtherErrors(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "go.mod.tmp")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
	calls := 0
	failingRename := func(string, string) error {
		calls++
		return os.ErrPermission
	}

	require.ErrorIs(t, renameReplace(src, filepath.Join(dir, "absent"), failingRename, true, hasOneLink),
		os.ErrPermission, "the destination does not exist")
	assert.Equal(t, 1, calls, "no second rename")

	// The destination is removed after the link count check, so it cannot
	// be made writable.
	dst := filepath.Join(dir, "go.mod")
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o444))
	removed := func(path string) bool {
		_ = os.Chmod(path, 0o644)
		_ = os.Remove(path)
		return true
	}
	calls = 0
	require.ErrorIs(t, renameReplace(src, dst, failingRename, true, removed), os.ErrPermission)
	assert.Equal(t, 1, calls, "no second rename")
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data), "the source does not change")
}
