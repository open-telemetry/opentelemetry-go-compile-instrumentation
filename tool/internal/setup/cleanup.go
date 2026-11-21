// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// Cleanup removes artifacts created by the setup and build phases.
// It is idempotent and best-effort: individual failures are logged as warnings
// but do not stop the overall cleanup.
//
// When cleanAll is false, backed-up files are restored and the generated runtime
// file is removed, but .otelc-build/ is kept for debugging. When cleanAll is
// true, .otelc-build/ is also removed.
//
// Cleanup runs under the build lock; GoBuild's deferred call reuses the
// surrounding lock via the context marker instead of re-acquiring it.
func Cleanup(ctx context.Context, cleanAll bool) error {
	return withBuildLock(ctx, "", func(ctx context.Context) error {
		return cleanupLocked(ctx, cleanAll)
	})
}

func cleanupLocked(ctx context.Context, cleanAll bool) error {
	logger := util.LoggerFromContext(ctx)
	stateManager, found := stateManagerFromContext(ctx)
	if !found {
		var err error
		stateManager, err = loadStateManager()
		if err != nil {
			return err
		}
	}

	reverted := true
	if stateManager != nil {
		if err := stateManager.Revert(); err != nil {
			reverted = false
			logger.WarnContext(ctx, "failed to revert state", "error", err)
		}

		// If Revert succeeded, discard the consumed state.
		if reverted {
			if discardErr := stateManager.Discard(); discardErr != nil {
				logger.WarnContext(ctx, "failed to discard consumed state", "error", discardErr)
			}
		}
	}

	if cleanAll {
		if !reverted {
			// The manifest and snapshots under .otelc-build/state are the only
			// way left to restore go.mod/go.sum; deleting them now would strand
			// the tree with replace directives pointing at removed directories.
			_, _ = fmt.Fprintf(os.Stderr, "Warning: state could not be fully reverted; "+
				"original file snapshots remain available for recovery at: %s\n",
				util.GetBuildTemp(stateDir))
			return nil
		}
		// Remove the entire .otelc-build/ temp directory last.
		if err := os.RemoveAll(util.GetBuildTempDir()); err != nil {
			logger.WarnContext(ctx, "failed to remove build temp dir", "error", err)
		}
	} else {
		logger.InfoContext(ctx, "keeping build working directory for debugging",
			"path", util.GetBuildTempDir(),
			"cleanup", "otelc cleanup")
	}

	return nil
}

// restoreInterruptedBuild restores the files that an earlier build changed
// and did not restore, because it was killed or its restore failed. Such a
// build leaves its manifest behind. A new build snapshots go.mod as it finds
// it, so without this step the replace directives of the earlier build would
// become the original go.mod of this one, and every later restore would put
// them back.
//
// It runs under the build lock, before the build tracks any file. Nothing is
// done when no manifest exists.
func restoreInterruptedBuild(ctx context.Context) error {
	stateManager, err := loadStateManager()
	if err != nil {
		return err
	}
	if stateManager == nil {
		return nil
	}

	logger := util.LoggerFromContext(ctx)
	logger.WarnContext(ctx, "restoring the files of an interrupted build")
	_, _ = fmt.Fprintf(os.Stderr, "Warning: a previous otelc build did not finish; restoring its go.mod and "+
		"related files before building\n")

	if err = stateManager.Revert(); err != nil {
		return ex.Wrapf(err, "restoring the files of an interrupted build; "+
			"the snapshots are kept in %s", util.GetBuildTemp(stateDir))
	}
	if err = stateManager.Discard(); err != nil {
		logger.WarnContext(ctx, "failed to discard consumed state", "error", err)
	}
	return nil
}
