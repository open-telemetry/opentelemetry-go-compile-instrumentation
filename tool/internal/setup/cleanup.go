// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path/filepath"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/urfave/cli/v3"
)

// Cleanup removes artifacts created by the setup and build phases.
// It is idempotent and best-effort: individual failures are logged as warnings
// but do not stop the overall cleanup.
//
// When cleanAll is false, backed-up files are restored and the generated runtime
// file is removed, but .otelc-build/ is kept for debugging. When cleanAll is
// true, .otelc-build/ is also removed.
func Cleanup(ctx context.Context, buildDir string, args []string, cleanAll bool) error {
	logger := util.LoggerFromContext(ctx)

	err := restoreBackupFiles()
	if err != nil {
		logger.WarnContext(ctx, "failed to restore backup files", "error", err)
	}

	// Remove otelc.runtime.go from each instrumented package directory.
	pkgs, pkgErr := getBuildPackages(ctx, buildDir, args)
	if pkgErr != nil {
		logger.DebugContext(ctx, "failed to get build packages", "error", pkgErr)
	}
	for _, pkg := range pkgs {
		path := filepath.Join(pkg.Dir, OtelcRuntimeFile)
		if err = os.RemoveAll(path); err != nil {
			logger.DebugContext(ctx, "failed to remove generated file from package",
				"file", path, "error", err)
		}
	}

	if cleanAll {
		// Remove the entire .otelc-build/ temp directory last.
		// The extracted instrumentation package lives inside .otelc-build/pkg/,
		// so this also covers removing it.
		if err = os.RemoveAll(util.GetBuildTempDir()); err != nil {
			logger.WarnContext(ctx, "failed to remove build temp dir", "error", err)
		}
	} else {
		logger.InfoContext(ctx, "keeping build working directory for debugging",
			"path", util.GetBuildTempDir(),
			"cleanup", "otelc cleanup")
	}

	return nil
}

// CleanupCommand is the CLI entrypoint for `otelc cleanup`.
func CleanupCommand(ctx context.Context, cmd *cli.Command, cleanAll bool) error {
	invocation, err := parseGoInvocation(cmd.Args().Slice())
	if err != nil {
		return err
	}
	return Cleanup(ctx, invocation.buildDir, invocation.args, cleanAll)
}
