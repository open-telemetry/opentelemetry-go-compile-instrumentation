// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const (
	backupDir       = "backup"
	backupStateFile = "backup.json"
)

func getBackupFiles(ctx context.Context, moduleDirs map[string]bool) ([]string, error) {
	var files []string

	// Find all go.mod and go.sum files
	for moduleDir := range moduleDirs {
		goModFile := filepath.Join(moduleDir, "go.mod")
		if util.PathExists(goModFile) {
			files = append(files, goModFile)
		}
		goSumFile := filepath.Join(moduleDir, "go.sum")
		if util.PathExists(goSumFile) {
			files = append(files, goSumFile)
		}
	}

	// Find go.work.sum if go.work exists
	goWorkCmd := exec.CommandContext(ctx, "go", "env", "GOWORK")
	goWorkOutput, err := goWorkCmd.Output()
	if err != nil {
		return nil, ex.Wrapf(err, "failed to get GOWORK environment variable")
	}
	goWorkPath := strings.TrimSpace(string(goWorkOutput))
	if goWorkPath != "" {
		goWorkSumPath := filepath.Join(filepath.Dir(goWorkPath), "go.work.sum")
		if util.PathExists(goWorkSumPath) {
			files = append(files, goWorkSumPath)
		}
	}

	return files, nil
}

func backupFilePath(path string) string {
	p := filepath.Clean(path)
	sum := sha256.Sum256([]byte(p))
	return filepath.Base(p) + "." + hex.EncodeToString(sum[:])
}

// backupFiles copies go.mod, go.sum and go.work.sum files to the backup directory
// and records their paths in a state file for later restoration.
func backupFiles(ctx context.Context, moduleDirs map[string]bool) error {
	files, err := getBackupFiles(ctx, moduleDirs)
	if err != nil {
		return ex.Wrapf(err, "failed to get backup files")
	}

	bakDir := util.GetBuildTemp(backupDir)
	for _, src := range files {
		dst := filepath.Join(bakDir, backupFilePath(src))
		err = ex.Join(err, util.CopyFile(src, dst))
	}
	if err != nil {
		return ex.Wrapf(err, "failed to copy backup files")
	}

	f := util.GetBuildTemp(backupStateFile)
	file, createErr := os.Create(f)
	if createErr != nil {
		return ex.Wrapf(createErr, "failed to create file %s", f)
	}
	defer file.Close()

	bs, marshalErr := json.Marshal(files)
	if marshalErr != nil {
		return ex.Wrapf(marshalErr, "failed to marshal backup state to JSON")
	}

	if _, writeErr := file.Write(bs); writeErr != nil {
		return ex.Wrapf(writeErr, "failed to write JSON to file %s", f)
	}

	return nil
}

// restoreBackupFiles reads the backup state file to get the list of original file paths,
// then copies the backed-up files from the backup directory back to their original locations.
func restoreBackupFiles() error {
	f := util.GetBuildTemp(backupStateFile)
	file, err := os.Open(f)
	if err != nil {
		return ex.Wrapf(err, "failed to open backup state file %s", f)
	}
	defer file.Close()

	var files []string
	decoder := json.NewDecoder(file)
	if err = decoder.Decode(&files); err != nil {
		return ex.Wrapf(err, "failed to decode backup state JSON from file %s", f)
	}

	bakDir := util.GetBuildTemp(backupDir)
	for _, src := range files {
		dst := filepath.Join(bakDir, backupFilePath(src))
		err = ex.Join(err, util.CopyFile(dst, src))
	}

	return err
}
