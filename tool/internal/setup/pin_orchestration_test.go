// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

// discardLogger returns a logger that drops all output, keeping test logs quiet.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPrepareVendoredBuild_NotVendored(t *testing.T) {
	// A plain module with no vendor/ directory must be left untouched: the
	// args come back verbatim and GOFLAGS is not forced to module mode.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))
	t.Setenv(util.EnvOtelcWorkDir, dir)
	t.Setenv("GOFLAGS", "")

	args := []string{"build", "-mod=vendor", "./..."}
	got, err := prepareVendoredBuild(t.Context(), discardLogger(), args)
	require.NoError(t, err)

	// Unchanged: not a vendored project, so no rewriting happens.
	assert.Equal(t, args, got)
	assert.Empty(t, os.Getenv("GOFLAGS"))
}

func TestPrepareVendoredBuild_Vendored(t *testing.T) {
	// A module that vendors its dependencies must be switched to module mode:
	// GOFLAGS gains -mod=mod and an explicit -mod=vendor on the command line
	// is rewritten so it cannot re-select vendoring.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "vendor", "modules.txt"),
		[]byte(""),
		0o644,
	))
	t.Setenv(util.EnvOtelcWorkDir, dir)
	t.Setenv("GOFLAGS", "")
	// Force non-workspace mode; -mod=mod is forbidden in a workspace, so a
	// stray ambient go.work would otherwise suppress vendoring detection.
	t.Setenv("GOWORK", "off")

	args := []string{"build", "-mod=vendor", "./..."}
	got, err := prepareVendoredBuild(t.Context(), discardLogger(), args)
	require.NoError(t, err)

	assert.Equal(t, []string{"build", "-mod=mod", "./..."}, got)
	assert.Contains(t, os.Getenv("GOFLAGS"), "-mod=mod")
}

func TestPinLocked_UpdatesExistingToolFile(t *testing.T) {
	// With ModuleDirs supplied, pinLocked skips dependency discovery and goes
	// straight to updating the existing tool file. A dependency that turns out
	// not to be an instrumentation package must be pruned from it.
	tmp := t.TempDir()

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/notinstrumentation": filepath.Join(tmp, "notinstrumentation"),
	})
	writeInstrumentationModule(
		t,
		filepath.Join(tmp, "notinstrumentation"),
		"example.com/notinstrumentation",
		false,
		nil,
	)

	_, err := pinLocked(t.Context(), PinOptions{
		Prune:      true,
		ModuleDirs: map[string]bool{tmp: true},
	})
	require.NoError(t, err)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "example.com/notinstrumentation")
}

func TestPinLocked_DiscoversModuleDirs(t *testing.T) {
	// With no ModuleDirs supplied, pinLocked must discover them from the build
	// packages in the working directory. With no existing tool file, it falls
	// through to generating one from the dependency graph.
	dir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, dir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755)) // ensure .otelc-build exists
	t.Chdir(dir)

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hi\") }\n"),
		0o644,
	))

	_, err := pinLocked(t.Context(), PinOptions{})
	require.NoError(t, err)

	// A tool file is generated for the discovered module.
	require.FileExists(t, filepath.Join(dir, ToolFileCanonical))
}

func TestPin_UpdatesExistingToolFile(t *testing.T) {
	// Pin wraps pinLocked under the build lock. Point the work dir at the
	// module so the lock is taken in the sandbox rather than an ambient path.
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/notinstrumentation": filepath.Join(tmp, "notinstrumentation"),
	})
	writeInstrumentationModule(
		t,
		filepath.Join(tmp, "notinstrumentation"),
		"example.com/notinstrumentation",
		false,
		nil,
	)

	result, err := Pin(t.Context(), PinOptions{
		Prune:      true,
		ModuleDirs: map[string]bool{tmp: true},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "example.com/notinstrumentation")
}

func TestAutoPin_NoStateManager(t *testing.T) {
	// AutoPin cannot track files to restore without a StateManager in context.
	_, err := AutoPin(t.Context(), map[string]bool{t.TempDir(): true}, subcmdBuild, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state manager not found")
}

func TestAutoPin_TracksAndPins(t *testing.T) {
	// With a StateManager present, AutoPin backs up the mutable files, tracks
	// them, then pins — pruning the non-instrumentation dependency along the way.
	tmp := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tmp)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755)) // ensure .otelc-build exists for state snapshots

	toolFile := writeInstrumentationModule(t, tmp, "example.com/root", false, map[string]string{
		"example.com/notinstrumentation": filepath.Join(tmp, "notinstrumentation"),
	})
	writeInstrumentationModule(
		t,
		filepath.Join(tmp, "notinstrumentation"),
		"example.com/notinstrumentation",
		false,
		nil,
	)

	sm := NewStateManager()
	ctx := ContextWithStateManager(t.Context(), sm)

	_, err := AutoPin(ctx, map[string]bool{tmp: true}, subcmdBuild, nil)
	require.NoError(t, err)

	// The go.mod of the root module should have been tracked for restore.
	goModAbs, err := filepath.Abs(filepath.Join(tmp, "go.mod"))
	require.NoError(t, err)
	assert.Contains(t, sm.files, filepath.Clean(goModAbs),
		"expected root go.mod to be tracked by the state manager")

	data, err := os.ReadFile(toolFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "example.com/notinstrumentation")
}
