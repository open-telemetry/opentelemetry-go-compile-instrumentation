// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandSetup = cli.Command{
	Name:            "setup",
	Description:     "Set up the environment for instrumentation",
	ArgsUsage:       "[go build flags]",
	SkipFlagParsing: true,
	Before:          addLoggerPhaseAttribute,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		args := cmd.Args().Slice()
		err := setup.Setup(ctx, args)
		if err != nil {
			return ex.Wrapf(err, "failed to setup with exit code %d", exitCodeFailure)
		}
		return nil
	},
}
