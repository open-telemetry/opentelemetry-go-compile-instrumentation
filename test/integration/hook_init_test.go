//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHookPackageInitialization(t *testing.T) {
	binaryName := "otelc"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	otelc, err := filepath.Abs(filepath.Join("..", "..", binaryName))
	require.NoError(t, err)
	app := t.TempDir()
	require.NoError(t, os.CopyFS(app, os.DirFS(filepath.Join("..", "apps", "hookinit"))))
	pkg, err := filepath.Abs(filepath.Join("..", "..", "pkg"))
	require.NoError(t, err)
	cmd := exec.CommandContext(
		t.Context(),
		"go",
		"mod",
		"edit",
		"-replace=go.opentelemetry.io/otelc/pkg="+filepath.ToSlash(pkg),
	)
	cmd.Dir = app
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	binary := filepath.Join(t.TempDir(), "app.exe")
	runOtelcCommand(t, app, append(os.Environ(), "OTELC_RULES=rules.yml"), otelc, "go", "build", "-o", binary, ".")
	out, err = exec.CommandContext(t.Context(), binary).CombinedOutput()
	require.NoError(t, err, "%s", out)
	output := string(out)
	require.Contains(t, output, "registered: [early early-again own-init last-init late]")
	require.Contains(t, output, "before: [late]")
	require.Contains(t, output, "after: [late]")
	require.Equal(
		t,
		1,
		strings.Count(
			output,
			"otelc: skipping hooks in example.com/hookinit/hooks before package initialization completes",
		),
	)
	require.NotContains(t, output, "failed to exec")
}
