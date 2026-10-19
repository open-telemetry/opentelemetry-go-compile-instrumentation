// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dave/dst"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestVendorNeedsVendorDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
	t.Chdir(dir)
	t.Setenv(util.EnvOtelcWorkDir, dir)
	t.Setenv("GOWORK", "off")

	err := Vendor(t.Context(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go mod vendor")
	assert.NoFileExists(t, filepath.Join(dir, toolFileCanonical), "nothing is written")
}

// newVendoredModule writes a module in a new directory that vendors the
// module corp.example/private, which no proxy has. It returns the directory.
func newVendoredModule(t *testing.T, goVersion string) string {
	t.Helper()
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+goVersion+"\n\nrequire corp.example/private v1.0.0\n")
	mustWriteFile(t, filepath.Join(dir, "main.go"),
		"package main\n\nimport \"corp.example/private\"\n\nfunc main() { private.Hello() }\n")
	mustWriteFile(t, filepath.Join(dir, "vendor", "corp.example", "private", "private.go"),
		"package private\n\nfunc Hello() {}\n")
	mustWriteFile(t, filepath.Join(dir, "vendor", "modules.txt"),
		"# corp.example/private v1.0.0\n## explicit\ncorp.example/private\n")
	t.Chdir(dir)
	t.Setenv(util.EnvOtelcWorkDir, dir)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOPROXY", "off")
	return dir
}

func TestVendorRejectsModFlag(t *testing.T) {
	dir := newVendoredModule(t, "1.25.0")

	err := Vendor(t.Context(), []string{"-mod=mod", "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-mod=mod is not supported")

	t.Setenv("GOFLAGS", "-mod=readonly")
	err = Vendor(t.Context(), []string{"."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-mod=readonly is not supported")

	t.Setenv("GOFLAGS", "")
	err = Vendor(t.Context(), []string{"-modfile=alt.mod", "."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-modfile is not supported")
	t.Setenv("GOFLAGS", "-modfile=alt.mod")
	err = Vendor(t.Context(), []string{"."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-modfile is not supported")
	assert.NoFileExists(t, filepath.Join(dir, toolFileCanonical), "nothing is written")
}

// TestVendorRecoversAndRevertsOnFailure: a killed build left a manifest and a
// changed go.mod. otelc vendor restores the original go.mod first (else a
// later recovery removes the changes of otelc vendor). Then otelc vendor
// fails, because it cannot download the modules of the hook code: all its
// changes are reverted, and no manifest stays.
func TestVendorRecoversAndRevertsOnFailure(t *testing.T) {
	dir := newVendoredModule(t, "1.25.0")
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOSUMDB", "off")
	goMod := filepath.Join(dir, "go.mod")
	original, err := os.ReadFile(goMod)
	require.NoError(t, err)
	modulesTxt, err := os.ReadFile(filepath.Join(dir, "vendor", "modules.txt"))
	require.NoError(t, err)

	killed := newStateManager()
	require.NoError(t, killed.Track(goMod))
	mustWriteFile(t, goMod, string(original)+"\nreplace example.com/x => ./.otelc-build/x\n")

	err = Vendor(t.Context(), []string{"."})
	require.Error(t, err, "the modules of the hook code cannot be downloaded")
	assert.Contains(t, err.Error(), "modules of the otelc hook code", "otelc vendor got to the merge step")

	data, readErr := os.ReadFile(goMod)
	require.NoError(t, readErr)
	assert.Equal(t, string(original), string(data), "go.mod is the original one")
	data, readErr = os.ReadFile(filepath.Join(dir, "vendor", "modules.txt"))
	require.NoError(t, readErr)
	assert.Equal(t, string(modulesTxt), string(data), "vendor/modules.txt does not change")
	assert.NoFileExists(t, filepath.Join(dir, toolFileCanonical), "the generated tool file is removed")
	assert.NoFileExists(t, util.GetBuildTemp(stateFileName), "no manifest stays")
}

func TestSetGoflagsModVendor(t *testing.T) {
	t.Setenv("GOFLAGS", "-trimpath -mod=readonly")
	restore, err := setGoflagsModVendor()
	require.NoError(t, err)
	assert.Equal(t, "-trimpath -mod=vendor", os.Getenv("GOFLAGS"))
	restore()
	assert.Equal(t, "-trimpath -mod=readonly", os.Getenv("GOFLAGS"))
}

// TestVendorGoflagsLoadOldGoModule: with a go version lower than 1.14 in
// go.mod, the go command does not use vendor/ by default. otelc vendor sets
// -mod=vendor, so the package load does not try to download the vendored
// module.
func TestVendorGoflagsLoadOldGoModule(t *testing.T) {
	newVendoredModule(t, "1.13")

	_, _, err := getBuildPackages(t.Context(), subcmdBuild, []string{"."})
	require.Error(t, err, "the fixture must need -mod=vendor")

	restore, err := setGoflagsModVendor()
	require.NoError(t, err)
	t.Cleanup(restore)
	pkgs, _, err := getBuildPackages(t.Context(), subcmdBuild, []string{"."})
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Empty(t, pkgs[0].Errors)
}

// TestVendorPinOptions: in vendor mode, otelc does not add the tool directive
// of otelc, so the //go:generate directive is not written: it would fail.
func TestVendorPinOptions(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.25.0\n")
	generate := true
	opts := PinOptions{Generate: &generate}

	got := vendorPinOptions(contextWithVendorMode(t.Context(), false), dir, opts)
	assert.Same(t, opts.Generate, got.Generate, "not in vendor mode: no change")

	got = vendorPinOptions(contextWithVendorMode(t.Context(), true), dir, opts)
	require.NotNil(t, got.Generate)
	assert.False(t, *got.Generate, "vendor mode without a tool directive: no //go:generate directive")
	assert.NotContains(t, generateOtelInstrumentationGo(map[string]bool{"example.com/x": true}, got).Decs.Start.All(),
		generateDirective(got))
	f := &dst.File{Name: dst.NewIdent("tools")}
	f.Decs.Start.Append(generateDirective(opts))
	updateGenerateDirective(f, got)
	assert.Empty(t, f.Decs.Start.All(), "an existing //go:generate directive is removed")

	mustWriteFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo 1.25.0\n\ntool "+util.OtelcToolCmdRoot+"\n")
	got = vendorPinOptions(contextWithVendorMode(t.Context(), true), dir, opts)
	assert.Same(t, opts.Generate, got.Generate, "go.mod has the tool directive: no change")
}

// TestFinishVendor: the manifest of otelc vendor must be removed, else a
// later recovery reverts the changes of otelc vendor. When it stays, the
// result is an error (and Vendor reverts its changes). Snapshots that stay
// without a manifest are only a warning.
func TestFinishVendor(t *testing.T) {
	skipWithoutPermissions(t)
	setup := func(t *testing.T) *stateManager {
		t.Helper()
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv(util.EnvOtelcWorkDir, dir)
		goMod := filepath.Join(dir, "go.mod")
		mustWriteFile(t, goMod, "module example.com/app\n")
		s := newStateManager()
		require.NoError(t, s.Track(goMod))
		return s
	}

	t.Run("manifest cannot be removed", func(t *testing.T) {
		s := setup(t)
		require.NoError(t, os.Chmod(util.GetBuildTempDir(), 0o555))
		t.Cleanup(func() { _ = os.Chmod(util.GetBuildTempDir(), 0o755) })
		require.Error(t, finishVendor(t.Context(), s))
		assert.FileExists(t, util.GetBuildTemp(stateFileName))
	})

	t.Run("snapshots cannot be removed", func(t *testing.T) {
		s := setup(t)
		require.NoError(t, os.Chmod(util.GetBuildTemp(stateDir), 0o555))
		t.Cleanup(func() { _ = os.Chmod(util.GetBuildTemp(stateDir), 0o755) })
		require.NoError(t, finishVendor(t.Context(), s))
		assert.NoFileExists(t, util.GetBuildTemp(stateFileName), "the manifest is removed first")
	})
}

// TestVendorWithToolFile: otelc vendor with an existing tool file adds the
// modules of its imports to vendor/ and keeps them. An import that is not an
// instrumentation package is pruned from the tool file. No manifest stays.
func TestVendorWithToolFile(t *testing.T) {
	appDir := newVendorSyncApp(t)
	toolFile := filepath.Join(appDir, toolFileCanonical)
	mustWriteFile(t, toolFile, "//go:build tools\n\npackage tools\n\nimport _ \"example.com/extra\"\n")

	require.NoError(t, Vendor(t.Context(), []string{"."}))

	assert.Contains(t, readAppFile(t, appDir, "vendor/modules.txt"), "# example.com/extra v1.0.0\n")
	assert.Contains(t, readAppFile(t, appDir, goModFileName), "example.com/extra v1.0.0")
	assert.NotContains(t, readAppFile(t, appDir, toolFileCanonical), "example.com/extra",
		"example.com/extra is not an instrumentation package")
	assert.NoFileExists(t, util.GetBuildTemp(stateFileName), "no manifest stays after success")
	assert.Equal(t, "-modcacherw", os.Getenv("GOFLAGS"), "GOFLAGS is restored")
}

func TestVendorFailsOnBadManifest(t *testing.T) {
	dir := newVendoredModule(t, "1.25.0")
	outside := filepath.Join(t.TempDir(), "x")
	mustWriteFile(t, util.GetBuildTemp(stateFileName), `["-`+filepath.ToSlash(outside)+`"]`)

	err := Vendor(t.Context(), []string{"."})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the otelc work directory")
	assert.NoFileExists(t, filepath.Join(dir, toolFileCanonical), "nothing is written")
}

func TestSetGoflagsModVendorUnset(t *testing.T) {
	t.Setenv("GOFLAGS", "")
	require.NoError(t, os.Unsetenv("GOFLAGS"))
	restore, err := setGoflagsModVendor()
	require.NoError(t, err)
	assert.Equal(t, "-mod=vendor", os.Getenv("GOFLAGS"))
	restore()
	_, set := os.LookupEnv("GOFLAGS")
	assert.False(t, set, "an unset GOFLAGS is unset again")
}

func TestVendorPinOptionsWithoutGoMod(t *testing.T) {
	generate := true
	got := vendorPinOptions(contextWithVendorMode(t.Context(), true), t.TempDir(), PinOptions{Generate: &generate})
	require.NotNil(t, got.Generate)
	assert.False(t, *got.Generate, "without a go.mod file there is no tool directive")

	require.Error(t, vendorSyncToolFiles(t.Context(), []string{filepath.Join(t.TempDir(), "absent.go")}))
}

func TestVendorBadPackage(t *testing.T) {
	newVendoredModule(t, "1.25.0")
	err := Vendor(t.Context(), []string{"./absent"})
	require.Error(t, err)
	assert.NoFileExists(t, util.GetBuildTemp(stateFileName))
}
