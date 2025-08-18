// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandGo = &cli.Command{
	Name:            "go",
	Description:     "Invoke the go toolchain with toolexec mode",
	Args:            true,
	ArgsUsage:       "[go toolchain flags]",
	SkipFlagParsing: true,
	Action: func(cCtx *cli.Context) error {
		logger := util.LoggerFromContext(cCtx.Context)
		backupFiles := []string{"go.mod", "go.sum", "go.work", "go.work.sum"}
		if err := util.BackupFile(backupFiles); err != nil {
			logger.Warn("failed to back up go.mod, go.sum, go.work, go.work.sum, proceeding despite this", "error", err)
		}
		defer func() {
			if err := util.RestoreFile(backupFiles); err != nil {
				logger.Warn("failed to restore go.mod, go.sum, go.work, go.work.sum", "error", err)
			}
		}()

		if err := setup.Setup(cCtx.Context); err != nil {
			return cli.Exit(err, exitCodeFailure)
		}

		if err := setup.BuildWithToolexec(cCtx.Context, cCtx.Args().Slice()); err != nil {
			return cli.Exit(err, exitCodeFailure)
		}

		return nil
	},
}
