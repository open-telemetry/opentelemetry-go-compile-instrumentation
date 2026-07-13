// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package instrument

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

// writeStub writes an executable shell script named name that runs body and
// returns its path. It stands in for a go tool the toolexec wrapper would
// otherwise exec; the name matters because command detection keys off the
// tool's basename (e.g. ".../compile").
func writeStub(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
	return path
}

// TestToolexecInterceptsToolVersion covers the exec plumbing of the `-V=full`
// probe; the rewritten line's content is asserted by TestMarkedToolVersion.
func TestToolexecInterceptsToolVersion(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	t.Run("runs the underlying tool and reports success", func(t *testing.T) {
		stub := writeStub(t, "compile", `echo "compile version go1.26.5"`)
		require.NoError(t, Toolexec(ctx, []string{stub, "-V=full"}, false))
	})

	t.Run("intercepts even in nested mode", func(t *testing.T) {
		stub := writeStub(t, "compile", `echo "compile version go1.26.5"`)
		require.NoError(t, Toolexec(ctx, []string{stub, "-V=full"}, true))
	})

	t.Run("propagates a failure from the underlying tool", func(t *testing.T) {
		failing := writeStub(t, "compile", "exit 3")
		err := Toolexec(ctx, []string{failing, "-V=full"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "running")
	})
}

func TestToolexecPassesThroughNonToolCommands(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	// A command that is neither compile nor link is run unchanged. The stub
	// records that it executed so the test can tell it was actually invoked.
	marker := filepath.Join(t.TempDir(), "ran")
	stub := writeStub(t, "asm", "touch "+marker)

	require.NoError(t, Toolexec(ctx, []string{stub, "-buildid", "x"}, false))
	assert.FileExists(t, marker)
}

// TestToolexecNestedGatesInstrumentation checks that the nested flag, not the
// command shape, decides whether otelc instruments: the same compile-shaped
// command is instrumented when nested is false (and here fails because no
// matched.json exists) but passed straight through when nested is true.
func TestToolexecNestedGatesInstrumentation(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	marker := filepath.Join(t.TempDir(), "ran")
	stub := writeStub(t, "compile", "touch "+marker)
	compileArgs := []string{stub, "-o", "out.a", "-p", "main", "-buildid", "id", "-pack", "main.go"}
	require.True(t, util.IsCompileCommandWithArgs(compileArgs), "stub must look like a compile command")

	t.Run("non-nested attempts instrumentation", func(t *testing.T) {
		err := Toolexec(ctx, compileArgs, false)
		require.Error(t, err, "instrumentation should try to load the absent matched.json")
		assert.Contains(t, err.Error(), "otelc setup")
		assert.NoFileExists(t, marker, "the tool is not run when instrumentation fails first")
	})

	t.Run("nested passes the command straight through", func(t *testing.T) {
		require.NoError(t, Toolexec(ctx, compileArgs, true))
		assert.FileExists(t, marker)
	})
}

func TestToolexecPassesThroughLinkWithoutAddedImports(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	// A link command with no recorded added-imports needs no importcfg
	// rewrite, so the wrapper runs it unchanged. -importcfg only has to be
	// present for the command to be recognized as a link.
	marker := filepath.Join(t.TempDir(), "ran")
	stub := writeStub(t, "link", "touch "+marker)
	linkArgs := []string{stub, "-o", "exe", "-buildid", "id", "-importcfg", "importcfg.link"}
	require.True(t, util.IsLinkCommandWithArgs(linkArgs), "stub must look like a link command")

	require.NoError(t, Toolexec(ctx, linkArgs, false))
	assert.FileExists(t, marker)
}

func TestToolexecRecordsStatsWhenEnabled(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcStats, "1")

	marker := filepath.Join(t.TempDir(), "ran")
	stub := writeStub(t, "asm", "touch "+marker)

	require.NoError(t, Toolexec(ctx, []string{stub, "-buildid", "x"}, false))
	assert.FileExists(t, marker)
}
