// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
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

// TestCommandSetupPassesBuildFlags: setup prepares the module for a later
// build, so it takes the same go build flags and packages as otelc go, such
// as -tags, and must not parse them as its own.
func TestCommandSetupPassesBuildFlags(t *testing.T) {
	var got []string
	setupCmd := commandSetup
	setupCmd.Before = nil
	setupCmd.Action = func(_ context.Context, cmd *cli.Command) error {
		got = cmd.Args().Slice()
		return nil
	}
	app := &cli.Command{Name: "otelc", Commands: []*cli.Command{&setupCmd}}
	require.NoError(t, app.Run(context.Background(), []string{"otelc", "setup", "-tags", "foo", "-trimpath", "./..."}))
	assert.Equal(t, []string{"-tags", "foo", "-trimpath", "./..."}, got)
}

func TestCommandPin_SetsVendor(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")

	root := t.TempDir()
	depDir := filepath.Join(root, "dep")
	otelcDir := filepath.Join(root, "otelc")
	appDir := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(depDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(otelcDir, "tool", "cmd", "otelc"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(appDir, "vendor"), 0o755))

	require.NoError(
		t,
		os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module example.com/dep\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(filepath.Join(depDir, "dep.go"), []byte("package dep\n"), 0o644))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(depDir, "otelc.yml"), []byte("version: \"v1.0.0\"\n"), 0o644),
	)

	require.NoError(t, os.WriteFile(
		filepath.Join(otelcDir, "go.mod"),
		[]byte("module go.opentelemetry.io/otelc\n\ngo 1.25.0\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(otelcDir, "tool", "cmd", "otelc", "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	))

	goMod := `module example.com/app

go 1.25.0

tool go.opentelemetry.io/otelc/tool/cmd/otelc

require (
	example.com/dep v1.0.0
	go.opentelemetry.io/otelc v0.0.0
)

replace (
	example.com/dep v1.0.0 => ../dep
	go.opentelemetry.io/otelc => ../otelc
)
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(goMod), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.sum"), []byte(""), 0o644))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(appDir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "otel.instrumentation.go"),
		[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/dep\"\n"),
		0o644,
	))
	modulesTxt := filepath.Join(appDir, "vendor", "modules.txt")
	require.NoError(t, os.WriteFile(modulesTxt, []byte(""), 0o644))

	t.Setenv(util.EnvOtelcWorkDir, root)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	t.Chdir(appDir)

	pinCmd := commandPin
	pinCmd.Before = nil
	app := &cli.Command{Name: "otelc", Commands: []*cli.Command{&pinCmd}}
	require.NoError(t, app.Run(t.Context(), []string{"otelc", "pin", "--generate=false", "--prune=false", "."}))

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	assert.Contains(t, string(after), "example.com/dep")
}
