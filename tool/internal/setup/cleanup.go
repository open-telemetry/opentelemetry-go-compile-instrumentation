// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path/filepath"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

// Cleanup removes all artifacts created by the setup and build phases.
// It is idempotent and best-effort: individual failures are logged as warnings
// but do not stop the overall cleanup.
func Cleanup(ctx context.Context) error {
	logger := util.LoggerFromContext(ctx)

	backupFiles := []string{"go.mod", "go.sum", "go.work", "go.work.sum"}

	// Restore backed-up files before removing .otelc-build/, since backups
	// live inside .otelc-build/backup/.
	// Only restore files that were actually backed up: repos without go.work
	// or go.sum will not have those files in the backup dir, and attempting
	// to restore absent files would produce spurious warnings.
	backupDir := util.GetBuildTemp("backup")
	if util.PathExists(backupDir) {
		var toRestore []string
		for _, f := range backupFiles {
			if util.PathExists(filepath.Join(backupDir, f)) {
				toRestore = append(toRestore, f)
			}
		}
		if err := util.RestoreFile(toRestore); err != nil {
			logger.WarnContext(ctx, "failed to restore backed up files", "error", err)
		}
	}

	// Remove the generated otel runtime bridge file from the current working directory.
	if err := os.RemoveAll(OtelcRuntimeFile); err != nil {
		logger.WarnContext(ctx, "failed to remove otel runtime file", "error", err)
	}

	// Remove the entire .otelc-build/ temp directory last.
	// The extracted instrumentation package lives inside .otelc-build/pkg/,
	// so this also covers removing it.
	if err := os.RemoveAll(util.GetBuildTempDir()); err != nil {
		logger.WarnContext(ctx, "failed to remove build temp dir", "error", err)
	}

	return nil
}
