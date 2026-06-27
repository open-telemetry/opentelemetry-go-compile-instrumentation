// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"runtime"

	"github.com/urfave/cli/v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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
		_, err := fmt.Fprintf(cmd.Writer, "otelc version %s", util.Version)
		if err != nil {
			return ex.Wrapf(err, "failed to print version")
		}

		if util.CommitHash != "unknown" {
			_, err = fmt.Fprintf(cmd.Writer, "+%s", util.CommitHash)
			if err != nil {
				return ex.Wrapf(err, "failed to print commit hash")
			}
		}

		if util.BuildTime != "unknown" {
			_, err = fmt.Fprintf(cmd.Writer, " (%s)", util.BuildTime)
			if err != nil {
				return ex.Wrapf(err, "failed to print build time")
			}
		}

		_, err = fmt.Fprint(cmd.Writer, "\n")
		if err != nil {
			return ex.Wrapf(err, "failed to print newline")
		}

		if cmd.Bool("verbose") {
			_, err = fmt.Fprintf(cmd.Writer, "%s\n", runtime.Version())
			if err != nil {
				return ex.Wrapf(err, "failed to print runtime version")
			}
		}

		return nil
	},
}
