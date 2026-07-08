//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

const vendoredApp = "vendored"

const vendoredAppGoMod = `module vendored

go 1.25.0

require golang.org/x/time v0.14.0
`

const vendoredAppMain = `package main

import (
	"time"

	"golang.org/x/time/rate"
)

func main() {
	println(rate.Every(time.Duration(1)))
}
`

func TestVendoredBuild(t *testing.T) {
	t.Parallel()
	buildVendored(t, "go", "build")
}

// An explicit -mod=vendor on the CLI beats the GOFLAGS=-mod=mod otelc sets, so
// otelc has to rewrite it back or the build runs in vendor mode and fails.
func TestVendoredBuildModVendor(t *testing.T) {
	t.Parallel()
	buildVendored(t, "go", "build", "-mod=vendor")
}

func buildVendored(t *testing.T, args ...string) {
	t.Helper()

	appsDir := t.TempDir()
	app := filepath.Join(appsDir, vendoredApp)
	require.NoError(t, os.MkdirAll(app, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(app, "go.mod"), []byte(vendoredAppGoMod), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(app, "main.go"), []byte(vendoredAppMain), 0o644))

	goMod(t, app, "tidy")
	goMod(t, app, "vendor")
	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	// setup edits go.mod but not vendor/modules.txt, so the build needs -mod=mod
	// to pass the vendor consistency check.
	testutil.Build(t, appsDir, vendoredApp, args...)
	output := testutil.Run(t, appsDir, vendoredApp, nil)

	// The basic rules gate rate.Every by version. At the pinned x/time v0.14.0,
	// Every1 and Every3 match but Every2 (>= v0.15.0) does not; a vendored build
	// that dropped the version would change that set.
	require.Contains(t, output, "Every1")
	require.Contains(t, output, "Every3")
	require.NotContains(t, output, "Every2")

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Equal(t, before, after, "otelc must not modify the vendor directory")
}

func goMod(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", append([]string{"mod"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
