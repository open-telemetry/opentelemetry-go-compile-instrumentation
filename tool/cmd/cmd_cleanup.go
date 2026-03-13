// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/urfave/cli/v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/setup"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandCleanup = cli.Command{
	Name:        "cleanup",
	Description: "Remove all artifacts created by the setup and build phases",
	Before:      addLoggerPhaseAttribute,
	Action: func(ctx context.Context, _ *cli.Command) error {
		return setup.Cleanup(ctx)
	},
}
