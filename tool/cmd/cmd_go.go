// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
	"github.com/urfave/cli/v3"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandGo = cli.Command{
	Name:            "go",
	Description:     "Invoke the go toolchain with toolexec mode",
	ArgsUsage:       "[go toolchain flags]",
	SkipFlagParsing: true,
	Before:          addLoggerPhaseAttribute,
	Action: func(ctx context.Context, cmd *cli.Command) error {
<<<<<<< HEAD
		return setup.GoBuild(ctx, cmd.Args().Slice())
=======
		logger := util.LoggerFromContext(ctx)
		backupFiles := []string{"go.mod", "go.sum", "go.work", "go.work.sum"}
		err := util.BackupFile(backupFiles)
		if err != nil {
			logger.Warn("failed to back up go.mod, go.sum, go.work, go.work.sum, proceeding despite this", "error", err)
		}
		defer func() {
			err = os.RemoveAll(setup.OtelRuntimeFile)
			if err != nil {
				logger.Warn("failed to remove otel runtime file", "error", err)
			}
			err = util.RestoreFile(backupFiles)
			if err != nil {
				logger.Warn("failed to restore go.mod, go.sum, go.work, go.work.sum", "error", err)
			}
		}()

		err = setup.Setup(ctx, os.Args[1:])
		if err != nil {
			return ex.Wrapf(err, "failed to build with toolexec with exit code %d", exitCodeFailure)
		}
		err = setup.BuildWithToolexec(ctx, cmd.Args().Slice())
		if err != nil {
			return ex.Wrapf(err, "failed to build with toolexec with exit code %d", exitCodeFailure)
		}

		return nil
>>>>>>> main
	},
}
