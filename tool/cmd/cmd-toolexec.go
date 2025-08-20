// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument"
	"github.com/urfave/cli/v3"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandToolexec = cli.Command{
	Name:            "toolexec",
	Description:     "Wrap a command run by the go toolchain",
	SkipFlagParsing: true,
	Hidden:          true,
	Before:          addLoggerPhaseAttribute,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		err := instrument.Toolexec(ctx, cmd.Args().Slice())
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}
		return nil
	},
}
