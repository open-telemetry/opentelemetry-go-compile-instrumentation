// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"

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
	return withBuildLock(ctx, func(ctx context.Context) error {
		return cleanupLocked(ctx, cleanAll)
	})
}

func cleanupLocked(ctx context.Context, cleanAll bool) error {
	logger := util.LoggerFromContext(ctx)
	stateManager, found := StateManagerFromContext(ctx)
	if !found {
		var err error
		stateManager, err = LoadStateManager()
		if err != nil {
			return err
		}
	}

	if stateManager != nil {
		if err := stateManager.Revert(); err != nil {
			logger.WarnContext(ctx, "failed to revert state", "error", err)
		}
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
