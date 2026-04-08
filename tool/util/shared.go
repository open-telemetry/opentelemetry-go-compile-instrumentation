// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	EnvOtelcWorkDir    = "OTELC_WORK_DIR"
	EnvOtelcRules      = "OTELC_RULES"
	EnvOtelcBuildFlags = "OTELC_BUILD_FLAGS"
	// EnvOtelcStats enables per-toolexec timing stats when set to "1".
	// Set automatically when --stats is used; propagated to child processes.
	EnvOtelcStats = "OTELC_STATS"
	BuildTempDir  = ".otelc-build"
	OtelcRoot     = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation"
)

func GetMatchedRuleFile() string {
	const matchedRuleFile = "matched.json"
	return GetBuildTemp(matchedRuleFile)
}

// GetAddedImportsFileForProcess returns the per-process import tracking file.
// Each compile process writes to its own file to avoid inter-process race conditions.
func GetAddedImportsFileForProcess() string {
	pid := os.Getpid()
	return GetBuildTemp(fmt.Sprintf("added_imports.%d.json", pid))
}

// GetAddedImportsPattern returns the glob pattern for all import tracking files.
// Used by the link phase to discover and merge all per-process import files.
func GetAddedImportsPattern() string {
	return GetBuildTemp("added_imports.*.json")
}

func GetOtelcWorkDir() string {
	wd := os.Getenv(EnvOtelcWorkDir)
	if wd == "" {
		wd, _ = os.Getwd()
		return wd
	}
	return wd
}

// GetBuildTemp returns the path to the build temp directory $BUILD_TEMP/name
func GetBuildTempDir() string {
	return filepath.Join(GetOtelcWorkDir(), BuildTempDir)
}

// GetBuildTemp returns the path to the build temp directory $BUILD_TEMP/name
func GetBuildTemp(name string) string {
	return filepath.Join(GetOtelcWorkDir(), BuildTempDir, name)
}

func copyBackupFiles(names []string, src, dst string) error {
	for _, name := range names {
		srcFile := filepath.Join(src, name)
		dstFile := filepath.Join(dst, name)
		err := CopyFile(srcFile, dstFile)
		if err != nil {
			return err
		}
	}
	return nil
}

// BackupFile backups the source file to $BUILD_TEMP/backup/name.
func BackupFile(names []string) error {
	return copyBackupFiles(names, ".", GetBuildTemp("backup"))
}

// RestoreFile restores the source file from $BUILD_TEMP/backup/name.
func RestoreFile(names []string) error {
	return copyBackupFiles(names, GetBuildTemp("backup"), ".")
}

// GetBuildFlags returns the build flags from OTELC_BUILD_FLAGS environment variable.
// The flags are stored as a JSON-encoded string array to preserve arguments that contain spaces.
// Returns nil if not set or on decode error.
func GetBuildFlags() []string {
	encoded := os.Getenv(EnvOtelcBuildFlags)
	if encoded == "" {
		return nil
	}
	var flags []string
	if err := json.Unmarshal([]byte(encoded), &flags); err != nil {
		// Malformed JSON, return nil
		return nil
	}
	return flags
}

// EncodeBuildFlags encodes build flags as a JSON string for storage in an environment variable.
// This preserves arguments that contain spaces (e.g., -tags "foo bar").
func EncodeBuildFlags(flags []string) string {
	if len(flags) == 0 {
		return ""
	}
	encoded, err := json.Marshal(flags)
	if err != nil {
		return ""
	}
	return string(encoded)
}
