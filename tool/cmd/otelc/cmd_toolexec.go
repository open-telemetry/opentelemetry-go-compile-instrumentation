// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/internal/instrument"
	"go.opentelemetry.io/otelc/tool/util"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandToolexec = cli.Command{
	Name:            "toolexec",
	Description:     "Wrap a command run by the go toolchain",
	SkipFlagParsing: true,
	Hidden:          true,
	Before:          addLoggerPhaseAttribute,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		nested := os.Getenv(util.EnvOtelcNestedToolexec) != ""
		if !nested {
			// Here, not in instrument.Toolexec, so os.Executable resolves to
			// this binary for the go commands it spawns.
			if err := instrument.EnableNestedToolexec(); err != nil {
				return err
			}
		}
		return instrument.Toolexec(ctx, cmd.Args().Slice(), nested)
	},
}
