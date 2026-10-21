// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// Cleanup removes artifacts created by the setup and build phases.
// It is idempotent.
//
// When cleanAll is false, backed-up files are restored and the generated runtime
// file is removed, but .otelc-build/ is kept for debugging. When cleanAll is
// true, .otelc-build/ is also removed.
//
// When the restore of the backed-up files fails, Cleanup returns the error and
// keeps the manifest and the snapshots in .otelc-build/, so that a later
// `otelc cleanup` can try again. The removal of .otelc-build/ is best-effort.
//
// Cleanup runs under the build lock; GoBuild's deferred call reuses the
// surrounding lock via the context marker instead of re-acquiring it.
func Cleanup(ctx context.Context, cleanAll bool) error {
	return withBuildLock(ctx, func(ctx context.Context) error {
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

	if err := revertState(stateManager); err != nil {
		return err
	}

	if cleanAll {
		// Remove the entire .otelc-build/ temp directory last.
		// The extracted instrumentation package lives inside .otelc-build/pkg/,
		// so this also covers removing it.
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

// revertState restores the files that stateManager tracks, and then removes
// its manifest and snapshots. When the restore fails, the manifest and the
// snapshots stay: they are the only way left to restore the files.
func revertState(stateManager *stateManager) error {
	if stateManager == nil {
		return nil
	}
	if err := stateManager.Revert(); err != nil {
		return ex.Wrapf(err, "otelc cannot restore the files that it changed; "+
			"the original files are kept in %s, fix the cause and run \"otelc cleanup\"",
			util.GetBuildTemp(stateDir))
	}
	if err := stateManager.Discard(); err != nil {
		return ex.Wrapf(err, "removing the restored state in %s", util.GetBuildTemp(stateDir))
	}
	return nil
}

// recoverLeftoverState restores the files of a build that stopped before its
// cleanup (for example after SIGKILL, OOM, or a CI cancellation). It reads
// the manifest that the build left in .otelc-build/. A new build must do this
// first: else it snapshots the changed files as the original ones.
func recoverLeftoverState(ctx context.Context) error {
	leftover, err := loadStateManager()
	if err != nil {
		return ex.Wrapf(err, "reading the state of a previous otelc build")
	}
	if leftover == nil {
		return nil
	}
	util.LoggerFromContext(ctx).WarnContext(ctx,
		"a previous otelc build did not restore its changes; restoring them now",
		"state", util.GetBuildTemp(stateFileName))
	if err = revertState(leftover); err != nil {
		return ex.Wrapf(err, "restoring the changes of a previous otelc build")
	}
	return nil
}
