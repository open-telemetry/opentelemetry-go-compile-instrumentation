// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"runtime"

	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandVersion = &cli.Command{
	Name:        "version",
	Description: "Print the version of the tool",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "verbose",
			Usage: "Print additional information about the tool",
		},
	},
	Action: func(cCtx *cli.Context) error {
		_, err := fmt.Fprintf(cCtx.App.Writer, "otel version %s", Version)
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}

		if CommitHash != "unknown" {
			_, err := fmt.Fprintf(cCtx.App.Writer, "+%s", CommitHash)
			if err != nil {
				return cli.Exit(err, exitCodeFailure)
			}
		}

		if BuildTime != "unknown" {
			_, err := fmt.Fprintf(cCtx.App.Writer, " (%s)", BuildTime)
			if err != nil {
				return cli.Exit(err, exitCodeFailure)
			}
		}

		_, err = fmt.Fprint(cCtx.App.Writer, "\n")
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}

		if cCtx.Bool("verbose") {
			_, err := fmt.Fprintf(cCtx.App.Writer, "%s\n", runtime.Version())
			if err != nil {
				return cli.Exit(err, exitCodeFailure)
			}
		}

		return nil
	},
}
