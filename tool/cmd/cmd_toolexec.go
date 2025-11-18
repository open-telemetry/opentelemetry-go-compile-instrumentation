// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandToolexec = cli.Command{
	Name:            "toolexec",
	Description:     "Wrap a command run by the go toolchain",
	SkipFlagParsing: true,
	Hidden:          true,
	Before:          addLoggerPhaseAttribute,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		return instrument.Toolexec(ctx, cmd.Args().Slice())
	},
}
