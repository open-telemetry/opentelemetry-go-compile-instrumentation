// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
	"github.com/urfave/cli/v2"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandSetup = &cli.Command{
	Name:        "setup",
	Description: "Set up the environment for instrumentation",
	Action: func(cCtx *cli.Context) error {
		err := setup.Setup(cCtx.Context)
		if err != nil {
			return cli.Exit(err, exitCodeFailure)
		}
		return nil
	},
}
