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
		err := util.BackupFile(backupFiles)
		if err != nil {
			logger.Warn("failed to back up go.mod, go.sum, go.work, go.work.sum, proceeding despite this", "error", err)
		}
		defer func() {
			err := util.RestoreFile(backupFiles)
			if err != nil {
				logger.Warn("failed to restore go.mod, go.sum, go.work, go.work.sum", "error", err)
			}
		}()

		err = setup.Setup(cCtx.Context)
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}

		err = setup.BuildWithToolexec(cCtx.Context, cCtx.Args().Slice())
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}

		return nil
	},
}
