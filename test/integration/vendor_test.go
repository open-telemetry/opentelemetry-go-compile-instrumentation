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

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
)

// TestVendoredBuild builds the basic demo from a vendored copy. setup edits
// go.mod for the hook modules but not vendor/modules.txt, so the build must use
// -mod=mod or it fails the vendor consistency check. The copy lives in a temp dir
// to keep the committed demo vendor-free and clear of TestBasic.
func TestVendoredBuild(t *testing.T) {
	t.Parallel()

	appsDir := t.TempDir()
	app := filepath.Join(appsDir, "basic")
	src := filepath.Join("..", "..", "demo", "app", "basic")
	require.NoError(t, os.CopyFS(app, os.DirFS(src)))

	goModVendor(t, app)
	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	testutil.Build(t, appsDir, "basic", "go", "build", "-a")
	output := testutil.Run(t, appsDir, "basic", nil)

	verifyExportedHelloWorldSpan(t, output)

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Equal(t, before, after, "otelc must not modify the vendor directory")
}

func goModVendor(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "mod", "vendor")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
