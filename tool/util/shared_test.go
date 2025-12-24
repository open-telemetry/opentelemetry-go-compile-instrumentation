// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkDirAndBuildTempPaths(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	tests := []struct {
		name string
		env  string
		run  func(t *testing.T)
	}{
		{
			name: "GetOtelWorkDir uses cwd when env not set",
			env:  "",
			run: func(t *testing.T) {
				assert.Equal(t, wd, GetOtelWorkDir())
			},
		},
		{
			name: "GetOtelWorkDir uses env when set",
			env:  "/test/path",
			run: func(t *testing.T) {
				assert.Equal(t, "/test/path", GetOtelWorkDir())
			},
		},
		{
			name: "GetBuildTempDir and GetBuildTemp",
			env:  "/somewhere",
			run: func(t *testing.T) {
				assert.Equal(
					t,
					filepath.Join("/somewhere", BuildTempDir),
					GetBuildTempDir(),
				)

				assert.Equal(
					t,
					filepath.Join("/somewhere", BuildTempDir, "foo.txt"),
					GetBuildTemp("foo.txt"),
				)
			},
		},
		{
			name: "GetMatchedRuleFile",
			env:  "/somewhere",
			run: func(t *testing.T) {
				assert.Equal(
					t,
					filepath.Join("/somewhere", BuildTempDir, "matched.json"),
					GetMatchedRuleFile(),
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvOtelWorkDir, tt.env)
			tt.run(t)
		})
	}
}

func TestBackupAndRestoreFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvOtelWorkDir, dir)

	// Setup dummy file to backup
	err := os.MkdirAll(filepath.Join(dir, BuildTempDir, "backup"), 0o755)
	require.NoError(t, err)

	fn := filepath.Join(dir, "file1.txt")
	err = os.WriteFile(fn, []byte("some content"), 0o644)
	require.NoError(t, err)

	cwd, err := os.Getwd()
	require.NoError(t, err)

	err = os.Chdir(dir)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	// backup
	err = BackupFile([]string{"file1.txt"})
	require.NoError(t, err)

	backupLoc := filepath.Join(dir, BuildTempDir, "backup", "file1.txt")
	backedData, err := os.ReadFile(backupLoc)
	require.NoError(t, err)
	assert.Equal(t, []byte("some content"), backedData)

	// Change file, then restore
	err = os.WriteFile(fn, []byte("other content"), 0o644)
	require.NoError(t, err)

	err = RestoreFile([]string{"file1.txt"})
	require.NoError(t, err)

	out, err := os.ReadFile(fn)
	require.NoError(t, err)
	assert.Equal(t, []byte("some content"), out)
}

func TestRestoreFile_MissingBackup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvOtelWorkDir, dir)

	err := RestoreFile([]string{"missing.txt"})
	require.Error(t, err)
}
