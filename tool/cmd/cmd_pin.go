// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandPin = cli.Command{
	Name:        "pin",
	Description: "Generate or update otel.instrumentation.go to pin instrumentation packages for the current module.",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "prune",
			Usage: "Prune invalid imports within otel.instrumentation.go",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "validate",
			Usage: "Validate that all imports in otel.instrumentation.go contain valid rules",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "generate",
			Usage: "Manages //go:generate directive in otel.instrumentation.go",
		},
	},
	Before: addLoggerPhaseAttribute,
	Action: func(ctx context.Context, cmd *cli.Command) error {
		opts := setup.PinOptions{
			Prune:    cmd.Bool("prune"),
			Generate: nil,
			Validate: cmd.Bool("validate"),
			Args:     cmd.Args().Slice(),
		}
		if cmd.IsSet("generate") {
			generate := cmd.Bool("generate")
			opts.Generate = &generate
		}

		_, err := setup.Pin(ctx, opts)
		return err
	},
}
