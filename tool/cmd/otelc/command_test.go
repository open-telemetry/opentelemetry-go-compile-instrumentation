// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
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

// TestCommandToolexecNestedKeepsToolexec: a nested toolexec puts otelc back in
// GOFLAGS, which startup stripped. A compile it instruments can resolve an
// added import with a nested build of its own, and that build must run through
// otelc too.
func TestCommandToolexecNestedKeepsToolexec(t *testing.T) {
	goBin, err := exec.LookPath("go")
	require.NoError(t, err)
	exe, err := os.Executable()
	require.NoError(t, err)
	innerFlag, err := util.BuildToolexecFlag(exe)
	require.NoError(t, err)
	wantToken, err := util.QuoteGoflagsToken(innerFlag)
	require.NoError(t, err)

	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcNestedToolexec, "1")
	t.Setenv("GOFLAGS", "-mod=mod")
	toolexec := commandToolexec
	toolexec.Before = nil
	app := &cli.Command{Name: "otelc", Commands: []*cli.Command{&toolexec}}
	require.NoError(t, app.Run(context.Background(), []string{"otelc", "toolexec", goBin, "env", "GOROOT"}))

	tokens := util.SplitGoflags(os.Getenv("GOFLAGS"))
	assert.Contains(t, tokens, "-mod=mod")
	assert.Contains(t, tokens, wantToken)
}
