// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
)

const (
	EnvOtelcWorkDir    = "OTELC_WORK_DIR"
	EnvOtelcRules      = "OTELC_RULES"
	EnvOtelcBuildFlags = "OTELC_BUILD_FLAGS"
	// EnvOtelcStats enables per-toolexec timing stats when set to "1".
	// Set automatically when --stats is used; propagated to child processes.
	EnvOtelcStats = "OTELC_STATS"
	// EnvOtelcDebug enables debug-level logging when set to "1".
	// Set automatically when --debug is used; propagated to child processes.
	EnvOtelcDebug = "OTELC_DEBUG"
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
	var err error
	for _, name := range names {
		srcFile := filepath.Join(src, name)
		dstFile := filepath.Join(dst, name)
		err = ex.Join(err, CopyFile(srcFile, dstFile))
	}
	return err
}

// BackupFileFromDir backs up the named files from dir into the backup directory,
// preserving the path of dir relative to cwd so that multiple module directories
// can be backed up without filename collisions.
//
// Example: if cwd is /project and dir is /project/submodule, then go.mod is
// stored at .otelc-build/backup/submodule/go.mod and can later be restored to
// the correct location by RestoreAllBackedUpFiles.
func BackupFileFromDir(names []string, dir string) error {
	wd, err := os.Getwd()
	if err != nil {
		return ex.Wrapf(err, "getting working directory for backup")
	}
	rel, err := filepath.Rel(wd, dir)
	if err != nil {
		rel = filepath.Base(dir)
	}
	dst := GetBuildTemp(filepath.Join("backup", rel))
	if mkErr := os.MkdirAll(dst, 0o755); mkErr != nil {
		return ex.Wrapf(mkErr, "creating backup directory %s", dst)
	}
	return copyBackupFiles(names, dir, dst)
}

// BackupFile backs up the named files from the current working directory.
// Deprecated: prefer BackupFileFromDir with an explicit directory.
func BackupFile(names []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return ex.Wrapf(err, "getting working directory for backup")
	}
	return BackupFileFromDir(names, wd)
}

// RestoreAllBackedUpFiles restores every file in the backup directory tree to
// its original location relative to the current working directory. It is the
// counterpart to BackupFileFromDir: the relative path recorded at backup time
// is used to reconstruct the original destination path.
func RestoreAllBackedUpFiles() error {
	backupRoot := GetBuildTemp("backup")
	wd, err := os.Getwd()
	if err != nil {
		return ex.Wrapf(err, "getting working directory for restore")
	}
	return filepath.WalkDir(backupRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel, relErr := filepath.Rel(backupRoot, path)
		if relErr != nil {
			return relErr
		}
		orig := filepath.Join(wd, rel)
		return CopyFile(path, orig)
	})
}

// RestoreFile restores the source file from $BUILD_TEMP/backup/name.
// Deprecated: use RestoreAllBackedUpFiles which handles multi-module setups.
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
