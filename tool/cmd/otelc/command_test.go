// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/util"
)

// TestCommandVersion runs the version subcommand with --verbose, exercising the
// full Action: the version line plus the Go runtime line. A single invocation
// is used because cli.Command caches parsed flag state on the shared
// package-level command, so reusing it across runs is unreliable.
func TestCommandVersion(t *testing.T) {
	var buf bytes.Buffer
	app := &cli.Command{
		Writer:   &buf,
		Commands: []*cli.Command{&commandVersion},
	}
	require.NoError(t, app.Run(context.Background(), []string{"otelc", "version", "--verbose"}))

	out := buf.String()
	assert.Contains(t, out, "otelc version")
	assert.Contains(t, out, util.Version)
	// --verbose appends the Go runtime version (e.g. "go1.25.0").
	assert.Contains(t, out, "go1.")
}
