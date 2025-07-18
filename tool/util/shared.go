// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"io"
	"os"
	"path/filepath"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

const (
	BuildTempDir = ".otel-build"
)

// GetBuildTemp returns the path to the build temp directory $BUILD_TEMP/name
func GetBuildTemp(name string) string {
	return filepath.Join(BuildTempDir, name)
}

func CopyFile(src, dst string) error {
	_, err := os.Stat(filepath.Dir(dst))
	if os.IsNotExist(err) {
		err = os.MkdirAll(filepath.Dir(dst), 0o755)
		if err != nil {
			return ex.Error(err)
		}
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return ex.Error(err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return ex.Error(err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return ex.Error(err)
	}
	return nil
}

// BackupFile backups the source file to $BUILD_TEMP/backup/name, error is
// tolerated as it's not critical.
func BackupFile(names []string) {
	for _, name := range names {
		src := name
		dst := filepath.Join(GetBuildTemp("backup"), name)
		_ = CopyFile(src, dst)
	}
}

// RestoreFile restores the source file from $BUILD_TEMP/backup/name, error is
// tolerated as it's not critical.
func RestoreFile(names []string) {
	for _, name := range names {
		src := filepath.Join(GetBuildTemp("backup"), name)
		dst := name
		_ = CopyFile(src, dst)
	}
}
