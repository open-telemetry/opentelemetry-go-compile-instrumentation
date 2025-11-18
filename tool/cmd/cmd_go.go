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
		return setup.GoBuild(ctx, cmd.Args().Slice())
	},
}
