//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func runOtelcCommand(t *testing.T, dir string, env []string, otelcPath string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), otelcPath, args...)
	cmd.Dir = dir
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s failed:\n%s", cmd.String(), output)
	return string(output)
}

func goOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s failed:\n%s", cmd.String(), out)
	return string(out)
}

func copyTestApp(t *testing.T, appName string) string {
	t.Helper()
	source, err := filepath.Abs(filepath.Join("..", "apps", appName))
	require.NoError(t, err)
	tempDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	appDir := filepath.Join(tempDir, appName)
	require.NoError(t, os.CopyFS(appDir, os.DirFS(source)))

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	repoRootSlash := filepath.ToSlash(repoRoot)

	err = filepath.WalkDir(appDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "go.mod" {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		goMod := string(raw)
		goMod = strings.ReplaceAll(goMod, "../../../../pkg", filepath.ToSlash(filepath.Join(repoRoot, "pkg")))
		goMod = strings.ReplaceAll(goMod, "../../../pkg", filepath.ToSlash(filepath.Join(repoRoot, "pkg")))
		goMod = strings.ReplaceAll(goMod, "../../../..", repoRootSlash)
		goMod = strings.ReplaceAll(goMod, "../../..", repoRootSlash)
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		return os.WriteFile(path, []byte(goMod), info.Mode())
	})
	require.NoError(t, err)

	return appDir
}
