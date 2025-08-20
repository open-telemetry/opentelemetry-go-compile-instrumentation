// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"errors"
	"os"
	"path/filepath"
)

const (
	EnvOtelWorkDir = "OTEL_WORK_DIR"
	BuildTempDir   = ".otel-build"
	OtelRoot       = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation"
)

func GetMatchedRuleFile() string {
	const matchedRuleFile = "matched.json"
	return GetBuildTemp(matchedRuleFile)
}

func GetOtelWorkDir() string {
	wd := os.Getenv(EnvOtelWorkDir)
	if wd == "" {
		wd, _ = os.Getwd()
		return wd
	}
	return wd
}

// GetBuildTemp returns the path to the build temp directory $BUILD_TEMP/name
func GetBuildTemp(name string) string {
	return filepath.Join(GetOtelWorkDir(), BuildTempDir, name)
}

func copyBackupFiles(names []string, src, dst string) error {
	var err error
	for _, name := range names {
		srcFile := filepath.Join(src, name)
		dstFile := filepath.Join(dst, name)
		err = errors.Join(err, CopyFile(srcFile, dstFile))
	}
	return err
}

// BackupFile backups the source file to $BUILD_TEMP/backup/name.
func BackupFile(names []string) error {
	return copyBackupFiles(names, ".", GetBuildTemp("backup"))
}

// RestoreFile restores the source file from $BUILD_TEMP/backup/name.
func RestoreFile(names []string) error {
	return copyBackupFiles(names, GetBuildTemp("backup"), ".")
}
