// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/util"
)

func runInitStats(t *testing.T, args ...string) error {
	t.Helper()
	app := &cli.Command{
		Flags:  []cli.Flag{&cli.BoolFlag{Name: "stats"}},
		Before: initStats,
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	return app.Run(context.Background(), append([]string{"otelc"}, args...))
}

func TestInitStats(t *testing.T) {
	t.Run("without stats flag is a no-op", func(t *testing.T) {
		t.Setenv(util.EnvOtelcStats, "")
		require.NoError(t, runInitStats(t))
		assert.Empty(t, os.Getenv(util.EnvOtelcStats))
	})

	t.Run("stats flag sets env for subprocess propagation", func(t *testing.T) {
		t.Setenv(util.EnvOtelcStats, "")
		require.NoError(t, runInitStats(t, "--stats"))
		assert.Equal(t, "1", os.Getenv(util.EnvOtelcStats))
	})
}
