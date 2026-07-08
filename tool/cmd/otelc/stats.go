// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// initStats enables toolexec timing stats if --stats is set.
// It sets OTELC_STATS so child toolexec processes inherit the flag through
// os.Environ() in BuildWithToolexec.
func initStats(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	if !cmd.Bool("stats") {
		return ctx, nil
	}

	if setErr := os.Setenv(util.EnvOtelcStats, "1"); setErr != nil {
		return ctx, ex.Wrapf(setErr, "set %s", util.EnvOtelcStats)
	}

	logger := util.LoggerFromContext(ctx)
	logger.InfoContext(ctx, "toolexec stats enabled")

	return ctx, nil
}
