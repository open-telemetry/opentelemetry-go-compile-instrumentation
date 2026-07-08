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

// TestVendoredBuild builds the basic demo from a vendored copy. setup edits
// go.mod for the hook modules but not vendor/modules.txt, so the build must use
// -mod=mod or it fails the vendor consistency check.
func TestVendoredBuild(t *testing.T) {
	t.Parallel()
	buildVendoredBasic(t, "go", "build", "-a")
}

// TestVendoredBuildModVendor covers the flag-precedence path: an explicit
// -mod=vendor on the CLI beats the GOFLAGS=-mod=mod otelc sets, so without
// rewriting it every phase runs in vendor mode again — the consistency check
// fails and the version-constrained x/time/rate rules go unmatched.
func TestVendoredBuildModVendor(t *testing.T) {
	t.Parallel()
	buildVendoredBasic(t, "go", "build", "-a", "-mod=vendor")
}

// buildVendoredBasic vendors a temp copy of the basic demo (kept out of the
// committed tree and clear of TestBasic), builds it with the given otelc args,
// and asserts both the hello-world span and the version-constrained x/time/rate
// instrumentation fired, and that the vendor directory was left untouched.
func buildVendoredBasic(t *testing.T, args ...string) {
	t.Helper()

	appsDir := t.TempDir()
	app := filepath.Join(appsDir, "basic")
	src := filepath.Join("..", "..", "demo", "app", "basic")
	require.NoError(t, os.CopyFS(app, os.DirFS(src)))

	goModVendor(t, app)
	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	testutil.Build(t, appsDir, "basic", args...)
	output := testutil.Run(t, appsDir, "basic", nil)

	verifyExportedHelloWorldSpan(t, output)

	// golang.org/x/time/rate is a vendored third-party dependency instrumented
	// by version-constrained rules. Under -mod=vendor its source path carries no
	// @version, so those rules silently fail to match; asserting Every1/Every3
	// fired proves the vendored build resolved the dependency (and its version)
	// from the module cache rather than vendor/.
	require.Contains(t, output, "Every1")
	require.Contains(t, output, "Every3")

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
