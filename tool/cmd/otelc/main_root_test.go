// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestNewRootCommand(t *testing.T) {
	cmd := newRootCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "otelc", cmd.Name)

	// All subcommands are wired up.
	names := make(map[string]bool)
	for _, c := range cmd.Commands {
		names[c.Name] = true
	}
	for _, want := range []string{"pin", "setup", "go", "cleanup", "toolexec", "version"} {
		assert.True(t, names[want], "missing subcommand %q", want)
	}

	// Root flags are present.
	flagNames := make(map[string]bool)
	for _, f := range cmd.Flags {
		for _, n := range f.Names() {
			flagNames[n] = true
		}
	}
	assert.True(t, flagNames["work-dir"])
	assert.True(t, flagNames["debug"])
}

// TestNewRootCommandRunVersion drives the whole root command through the
// version subcommand, exercising the Before hook (GOFLAGS strip, initLogger,
// initProfiling, initStats), the After hook (stopProfiling, closeLogger), and
// the version command's Action including the --verbose runtime line.
func TestNewRootCommandRunVersion(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "")
	t.Setenv("GOFLAGS", "")

	var buf bytes.Buffer
	cmd := newRootCommand()
	cmd.Writer = &buf

	require.NoError(t, cmd.Run(context.Background(), []string{"otelc", "version", "--verbose"}))
	out := buf.String()
	assert.Contains(t, out, "otelc version")
	assert.Contains(t, out, util.Version)
	assert.Contains(t, out, "go1.")
}
