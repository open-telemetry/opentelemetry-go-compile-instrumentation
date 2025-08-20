// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument"
	"github.com/urfave/cli/v3"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandToolexec = cli.Command{
	Name:            "toolexec",
	Description:     "Wrap a command run by the go toolchain",
	SkipFlagParsing: true,
	Hidden:          true,
	Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
		_, ok := os.LookupEnv("TOOLEXEC_IMPORT_PATH")
		if !ok {
			return ctx, cli.Exit("toolexec can only be invoked by the go toolchain", exitCodeUsageError)
		}

		return addLoggerPhaseAttribute(ctx, cmd)
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		err := instrument.Toolexec(ctx, cmd.Args().Slice())
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}
		return nil
	},
}
