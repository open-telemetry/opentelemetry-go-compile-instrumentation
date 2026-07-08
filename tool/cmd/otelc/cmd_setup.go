// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/internal/setup"
)

//nolint:gochecknoglobals // Implementation of a CLI command
var commandSetup = cli.Command{
	Name:        "setup",
	Description: "Set up the environment for instrumentation",
	Before:      addLoggerPhaseAttribute,
	Action:      setup.Setup,
}
