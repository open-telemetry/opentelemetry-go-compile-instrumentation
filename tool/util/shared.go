// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"path/filepath"
)

const (
	BuildTempDir = ".otel-build"
	OtelRoot     = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation"
)

// GetBuildTemp returns the path to the build temp directory $BUILD_TEMP/name
func GetBuildTemp(name string) string {
	return filepath.Join(BuildTempDir, name)
}

func copyBackupFiles(names []string, src, dst string) {
	for _, name := range names {
		srcFile := filepath.Join(src, name)
		dstFile := filepath.Join(dst, name)
		_ = CopyFile(srcFile, dstFile)
	}
}

// BackupFile backups the source file to $BUILD_TEMP/backup/name, error is
// tolerated as it's not critical.
func BackupFile(names []string) {
	copyBackupFiles(names, ".", GetBuildTemp("backup"))
}

// RestoreFile restores the source file from $BUILD_TEMP/backup/name, error is
// tolerated as it's not critical.
func RestoreFile(names []string) {
	copyBackupFiles(names, GetBuildTemp("backup"), ".")
}
