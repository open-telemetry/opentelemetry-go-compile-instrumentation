// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

// TestCommandVendorHelp: -h and --help show the help of otelc vendor. They
// must not run otelc vendor, which changes the project.
func TestCommandVendorHelp(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			t.Chdir(t.TempDir())
			var buf bytes.Buffer
			vendor := commandVendor
			vendor.Before = nil
			app := &cli.Command{Name: "otelc", Writer: &buf, Commands: []*cli.Command{&vendor}}
			require.NoError(t, app.Run(context.Background(), []string{"otelc", "vendor", flag}))
			assert.Contains(t, buf.String(), "vendor")
			assert.Contains(t, buf.String(), "go build flags")
		})
	}
}

// TestCommandVendorRuns: other arguments go to otelc vendor, which fails in a
// module without a vendor directory.
func TestCommandVendorRuns(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(util.EnvOtelcWorkDir, dir)
	t.Setenv("GOWORK", "off")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
	vendor := commandVendor
	vendor.Before = nil
	app := &cli.Command{Name: "otelc", Commands: []*cli.Command{&vendor}}
	err := app.Run(context.Background(), []string{"otelc", "vendor", "-v", "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go mod vendor")
	assert.False(t, isHelpFlag("-v"))
}
