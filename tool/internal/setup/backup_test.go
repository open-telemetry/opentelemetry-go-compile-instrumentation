// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/require"
)

func TestGetBackupFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	moduleDir := filepath.Join(tmp, "mod")
	goMod := filepath.Join(moduleDir, "go.mod")
	goSum := filepath.Join(moduleDir, "go.sum")

	mustWriteFile(t, goMod, "testmod")
	mustWriteFile(t, goSum, "testsum")

	goWork := filepath.Join(tmp, "go.work")
	goWorkSum := filepath.Join(tmp, "go.work.sum")

	mustWriteFile(t, goWork, "testwork")
	mustWriteFile(t, goWorkSum, "testworksum")

	files, err := getBackupFiles(t.Context(), map[string]bool{
		moduleDir: true,
	})
	require.NoError(t, err)

	require.ElementsMatch(t, []string{
		goMod,
		goSum,
		goWorkSum,
	}, files)
}

func TestGetBackupFiles_MissingFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	files, err := getBackupFiles(t.Context(), map[string]bool{
		tmp: true,
	})

	require.NoError(t, err)
	require.Empty(t, files)
}

func TestBackupFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	file1 := filepath.Join(tmp, "go.mod")
	file2 := filepath.Join(tmp, "go.sum")
	const file1Data = "module example.com\n\ngo 1.24.0\n"
	const file2Data = "testsum"

	mustWriteFile(t, file1, file1Data)
	mustWriteFile(t, file2, file2Data)

	err := backupFiles(t.Context(), map[string]bool{
		tmp: true,
	})
	require.NoError(t, err)

	backupDir := util.GetBuildTemp(backupDir)

	backup1 := filepath.Join(backupDir, backupFilePath(file1))
	backup2 := filepath.Join(backupDir, backupFilePath(file2))

	data1, err := os.ReadFile(backup1)
	require.NoError(t, err)
	require.Equal(t, file1Data, string(data1))

	data2, err := os.ReadFile(backup2)
	require.NoError(t, err)
	require.Equal(t, file2Data, string(data2))

	stateData, err := os.ReadFile(util.GetBuildTemp(backupStateFile))
	require.NoError(t, err)

	var files []string
	require.NoError(t, json.Unmarshal(stateData, &files))
	require.ElementsMatch(t, []string{file1, file2}, files)
}

func TestRestoreBackupFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	type fileCase struct {
		path     string
		original string
		modified string
	}

	files := []fileCase{
		{
			path:     filepath.Join(tmp, "go.work.sum"),
			original: "// original go.work.sum content",
			modified: "// modified go.work.sum content",
		},
		{
			path:     filepath.Join(tmp, "pkgA", "go.mod"),
			original: "module original.pkga.com\n\ngo 1.24.0\n",
			modified: "module modified.pkga.com\n\ngo 1.24.0\n",
		},
		{
			path:     filepath.Join(tmp, "pkgB", "go.mod"),
			original: "module original.pkgb.com\n\ngo 1.24.0\n",
			modified: "module modified.pkgb.com\n\ngo 1.24.0\n",
		},
	}

	mustWriteFile(t, filepath.Join(tmp, "go.work"), "go 1.24\n\nuse ./pkgA\nuse ./pkgB")
	for _, f := range files {
		mustWriteFile(t, f.path, f.original)
	}

	_ = backupFiles(t.Context(), map[string]bool{
		filepath.Join(tmp, "pkgA"): true,
		filepath.Join(tmp, "pkgB"): true,
	})

	for _, f := range files {
		mustWriteFile(t, f.path, f.modified)
	}

	require.NoError(t, restoreBackupFiles())

	for _, f := range files {
		data, err := os.ReadFile(f.path)
		require.NoError(t, err)

		require.Equal(t, f.original, string(data))
	}
}

func TestRestoreBackupFiles_MissingStateFile(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	err := restoreBackupFiles()
	require.Error(t, err)
}

func TestRestoreBackupFiles_InvalidStateJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	mustWriteFile(t, util.GetBuildTemp(backupStateFile), "{bad json")

	err := restoreBackupFiles()
	require.Error(t, err)
}

func TestRestoreBackupFiles_BackupMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	original := filepath.Join(tmp, "go.mod")

	state, err := json.Marshal([]string{original})
	require.NoError(t, err)

	mustWriteFile(t, util.GetBuildTemp(backupStateFile), string(state))

	err = restoreBackupFiles()
	require.Error(t, err)
}
