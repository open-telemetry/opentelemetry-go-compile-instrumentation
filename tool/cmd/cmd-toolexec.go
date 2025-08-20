// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/instrument"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandToolexec = &cli.Command{
	Name:            "toolexec",
	Description:     "Wrap a command run by the go toolchain",
	Args:            true,
	SkipFlagParsing: true,
	Hidden:          true,
	Before: func(*cli.Context) error {
		_, ok := os.LookupEnv("TOOLEXEC_IMPORT_PATH")
		if !ok {
			return cli.Exit("toolexec can only be invoked by the go toolchain", exitCodeUsageError)
		}
		return nil
	},
	Action: func(cCtx *cli.Context) error {
		err := instrument.Toolexec(cCtx.Context, cCtx.Args().Slice())
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}
		return nil
	},
}
