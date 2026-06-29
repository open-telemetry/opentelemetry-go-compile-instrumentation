//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/test/testutil"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const toolFileCanonical = "otel.instrumentation.go"

func copyTestApp(t *testing.T, app string) string {
	t.Helper()

	src := filepath.Join("../", "apps", app)
	dst := filepath.Join(t.TempDir(), app)

	absRepoPath, err := filepath.Abs("../..")
	require.NoError(t, err)

	require.NoError(t, filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		base := filepath.Base(path)
		if base == ".otelc-build" {
			return filepath.SkipDir
		}
		if base == toolFileCanonical {
			return nil
		}

		rel, relErr := filepath.Rel(src, path)
		require.NoError(t, relErr)

		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		out, openTargetErr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		require.NoError(t, openTargetErr)
		defer out.Close()

		if filepath.Base(path) == "go.mod" {
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)

			content := string(data)
			repoPathSlash := filepath.ToSlash(absRepoPath)
			content = strings.ReplaceAll(content, "../../../", repoPathSlash+"/")
			content = strings.ReplaceAll(content, repoPathSlash+"//", repoPathSlash+"/")

			_, writeErr := out.Write([]byte(content))
			return writeErr
		}

		in, openErr := os.Open(path)
		require.NoError(t, openErr)
		defer in.Close()

		_, err = io.Copy(out, in)
		return err
	}))

	return dst
}

func runPin(t *testing.T, workDir string, args ...string) (string, error) {
	t.Helper()

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)

	cmd := exec.CommandContext(t.Context(), otelc, append([]string{"pin"}, args...)...)
	cmd.Dir = workDir

	out, outErr := cmd.CombinedOutput()
	if outErr != nil {
		return string(out), fmt.Errorf("%w: %s", outErr, string(out))
	}
	return string(out), nil
}

func writeToolFile(t *testing.T, path string, imports ...string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("//go:build tools\n\n")
	b.WriteString("package tools\n\n")
	b.WriteString("import (\n")
	for _, imp := range imports {
		fmt.Fprintf(&b, "\t_ %q\n", imp)
	}
	b.WriteString(")\n")

	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
}

func writeInstrumentationModule(
	t *testing.T,
	root, module string,
	writeInvalidRules bool,
	imports map[string]string,
) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(root, 0o755))

	goMod := fmt.Appendf(nil, "module %s\n\ngo 1.25\n", module)
	for imp := range imports {
		goMod = fmt.Appendf(goMod, "\nrequire %s v0.0.0-00010101000000-000000000000", imp)
	}
	for imp, replace := range imports {
		goMod = fmt.Appendf(goMod, "\nreplace %s => %s\n", imp, replace)
	}
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "go.mod"),
		goMod,
		0o644,
	))

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "dummy.go"),
		[]byte("package dummy\n"),
		0o644,
	))

	if writeInvalidRules {
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "invalid.otelc.yml"),
			[]byte("invalid yaml content"),
			0o644,
		))
	}

	if len(imports) > 0 {
		writeToolFile(t, filepath.Join(root, toolFileCanonical), slices.Collect(maps.Keys(imports))...)
	}

	return filepath.Join(root, toolFileCanonical)
}

func TestPin(t *testing.T) {

	t.Run("generates new tool file", func(t *testing.T) {
		workDir := copyTestApp(t, "httpclient")

		toolFile := filepath.Join(workDir, toolFileCanonical)
		require.NoError(t, os.RemoveAll(toolFile))

		_, pinErr := runPin(t, workDir)
		require.NoError(t, pinErr)

		content, readErr := os.ReadFile(toolFile)
		require.NoError(t, readErr)

		// Ensure otelc is imported in the tool file.
		require.Contains(t, string(content), util.OtelcToolCmdRoot)

		// Ensure http integration is imported in the tool file.
		require.Contains(t, string(content), util.OtelcRoot+"/instrumentation/net/http/client")

		goMod, goModErr := os.ReadFile(filepath.Join(workDir, "go.mod"))
		require.NoError(t, goModErr)

		// Ensure otelc is within the go.mod file, which ensures it is pinned as a dependency.
		require.Contains(t, string(goMod), util.OtelcRoot)

		// Ensure the http integration is within the go.mod file, which ensures it is pinned as a dependency.
		require.Contains(t, string(goMod), util.OtelcRoot+"/instrumentation/net/http/client")
	})

	t.Run("prunes invalid imports", func(t *testing.T) {
		tmp := t.TempDir()

		// "stale" is a valid go module but contains neither a tool file nor any rule files.
		// It should be pruned from the tool file when pinning the "app" module.
		staleDir := filepath.Join(tmp, "stale")
		writeInstrumentationModule(t, staleDir, "example.com/stale", false, nil)

		appDir := filepath.Join(tmp, "app")
		writeInstrumentationModule(t, appDir, "example.com/app", false, map[string]string{
			"example.com/stale": staleDir,
		})

		_, pinErr := runPin(t, appDir)
		require.NoError(t, pinErr)

		content, readErr := os.ReadFile(filepath.Join(appDir, toolFileCanonical))
		require.NoError(t, readErr)

		// Ensure stale is not imported in the tool file.
		require.NotContains(t, string(content), "example.com/stale")

		goMod, goModErr := os.ReadFile(filepath.Join(appDir, "go.mod"))
		require.NoError(t, goModErr)

		// Ensure stale is not within the go.mod file.
		// We say "require" explicitly because replaces may still remain, this is normal go mod tidy behavior.
		require.NotContains(t, string(goMod), "require example.com/stale")
	})

	t.Run("prunes imports with invalid rule files", func(t *testing.T) {
		tmp := t.TempDir()

		// "invalid" is a valid go module but contains an invalid rule file.
		// It should be pruned from the tool file when pinning the "app" module.
		invalidDir := filepath.Join(tmp, "invalid")
		writeInstrumentationModule(t, invalidDir, "example.com/invalid", true, nil)

		appDir := filepath.Join(tmp, "app")
		writeInstrumentationModule(t, appDir, "example.com/app", false, map[string]string{
			"example.com/invalid": invalidDir,
		})

		_, pinErr := runPin(t, appDir, "--validate")
		require.NoError(t, pinErr)

		content, readErr := os.ReadFile(filepath.Join(appDir, toolFileCanonical))
		require.NoError(t, readErr)

		// Ensure invalid is not imported in the tool file.
		require.NotContains(t, string(content), "example.com/invalid")

		goMod, goModErr := os.ReadFile(filepath.Join(appDir, "go.mod"))
		require.NoError(t, goModErr)

		// Ensure invalid is not within the go.mod file.
		// We say "require" explicitly because replaces may still remain, this is normal go mod tidy behavior.
		require.NotContains(t, string(goMod), "require example.com/invalid")
	})

	t.Run("keeps invalid imports without pruning", func(t *testing.T) {
		tmp := t.TempDir()

		// "stale" is a valid go module but contains neither a tool file nor any rule files.
		// It should not be pruned from the tool file when pinning the "app" module
		// because the --prune=false flag is provided.
		staleDir := filepath.Join(tmp, "stale")
		writeInstrumentationModule(t, staleDir, "example.com/stale", false, nil)

		appDir := filepath.Join(tmp, "app")
		writeInstrumentationModule(t, appDir, "example.com/app", false, map[string]string{
			"example.com/stale": staleDir,
		})

		_, pinErr := runPin(t, appDir, "--prune=false")
		require.NoError(t, pinErr)

		content, readErr := os.ReadFile(filepath.Join(appDir, toolFileCanonical))
		require.NoError(t, readErr)

		// Ensure stale is still imported in the tool file.
		require.Contains(t, string(content), "example.com/stale")

		goMod, goModErr := os.ReadFile(filepath.Join(appDir, "go.mod"))
		require.NoError(t, goModErr)

		// Ensure stale is still within the go.mod file.
		// We say "require" explicitly because replaces may always remain, this is normal go mod tidy behavior.
		require.Contains(t, string(goMod), "require example.com/stale")
	})

	t.Run("is idempotent", func(t *testing.T) {
		workDir := copyTestApp(t, "httpclient")

		_, firstPinErr := runPin(t, workDir)
		require.NoError(t, firstPinErr)

		firstTool, firstToolErr := os.ReadFile(
			filepath.Join(workDir, toolFileCanonical),
		)
		require.NoError(t, firstToolErr)

		firstGoMod, firstGoModErr := os.ReadFile(
			filepath.Join(workDir, "go.mod"),
		)
		require.NoError(t, firstGoModErr)

		_, secondPinErr := runPin(t, workDir)
		require.NoError(t, secondPinErr)

		secondTool, secondToolErr := os.ReadFile(
			filepath.Join(workDir, toolFileCanonical),
		)
		require.NoError(t, secondToolErr)

		secondGoMod, secondGoModErr := os.ReadFile(
			filepath.Join(workDir, "go.mod"),
		)
		require.NoError(t, secondGoModErr)

		require.Equal(t, firstTool, secondTool)
		require.Equal(t, firstGoMod, secondGoMod)
	})

	t.Run("preserves non-import declarations", func(t *testing.T) {
		tmp := t.TempDir()

		instrumentationDir := filepath.Join(tmp, "instrumentation")
		writeInstrumentationModule(t, instrumentationDir, "example.com/instrumentation", true, nil)

		appDir := filepath.Join(tmp, "app")
		writeInstrumentationModule(t, appDir, "example.com/app", false, map[string]string{
			"example.com/instrumentation": instrumentationDir,
		})

		toolFile := filepath.Join(appDir, toolFileCanonical)
		require.NoError(t, os.WriteFile(toolFile, []byte(`//go:build tools

package tools

import (
		_ "example.com/instrumentation"
)

const Sentinel = "keep-me"

func Hello() string {
		return Sentinel
}
`), 0o644))

		_, pinErr := runPin(t, appDir)
		require.NoError(t, pinErr)

		content, readErr := os.ReadFile(toolFile)
		require.NoError(t, readErr)

		require.Contains(t, string(content), `_ "example.com/instrumentation"`)
		require.Contains(t, string(content), `const Sentinel = "keep-me"`)
		require.Contains(t, string(content), `func Hello() string`)
	})
}
