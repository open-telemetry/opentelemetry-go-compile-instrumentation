// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"runtime"

	"github.com/urfave/cli/v3"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandVersion = cli.Command{
	Name:        "version",
	Description: "Print the version of the tool",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "verbose",
			Usage: "Print additional information about the tool",
		},
	},
	Before: addLoggerPhaseAttribute,
	Action: func(_ context.Context, cmd *cli.Command) error {
		_, err := fmt.Fprintf(cmd.Writer, "otel version %s", Version)
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}

		if CommitHash != "unknown" {
			_, err = fmt.Fprintf(cmd.Writer, "+%s", CommitHash)
			if err != nil {
				return cli.Exit(err, exitCodeFailure)
			}
		}

		if BuildTime != "unknown" {
			_, err = fmt.Fprintf(cmd.Writer, " (%s)", BuildTime)
			if err != nil {
				return cli.Exit(err, exitCodeFailure)
			}
		}

		_, err = fmt.Fprint(cmd.Writer, "\n")
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}

		if cmd.Bool("verbose") {
			_, err = fmt.Fprintf(cmd.Writer, "%s\n", runtime.Version())
			if err != nil {
				return cli.Exit(err, exitCodeFailure)
			}
		}

		return nil
	},
}
