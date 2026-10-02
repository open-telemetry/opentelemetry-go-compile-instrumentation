//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

const toolFileCanonical = "otel.instrumentation.go"

func writePinApp(t *testing.T) string {
	t.Helper()

	const pinApp = "pinapp"
	const pinAppGoMod = `module pinapp

go 1.25.0
`
	const pinAppMain = `package main

import "net/http"

func main() {
		_, _ = http.Get("https://example.com")
}
`

	appDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "go.mod"),
		[]byte(pinAppGoMod),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(appDir, "main.go"),
		[]byte(pinAppMain),
		0o644,
	))

	return appDir
}

func runPin(t *testing.T, workDir string, args ...string) (string, error) {
	t.Helper()

	otelc, err := testutil.OtelcPath()
	require.NoError(t, err)

	cmd := exec.CommandContext(t.Context(), otelc, append([]string{"pin"}, args...)...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	if os.Getenv("OTELC_SOURCE_ROOT") == "" {
		cmd.Env = append(cmd.Env, "OTELC_SOURCE_ROOT="+filepath.Dir(otelc))
	}

	out, outErr := cmd.CombinedOutput()
	if outErr != nil {
		t.Logf("pin failed: %v", outErr)
		t.Logf("pin output: %s", string(out))
	}

	return string(out), outErr
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

func TestPin_GeneratesNewToolFile(t *testing.T) {
	t.Parallel()

	workDir := writePinApp(t)

	toolFile := filepath.Join(workDir, toolFileCanonical)
	require.NoError(t, os.RemoveAll(toolFile))

	_, pinErr := runPin(t, workDir)
	require.NoError(t, pinErr)

	content, readErr := os.ReadFile(toolFile)
	require.NoError(t, readErr)

	// Ensure http integration is imported in the tool file.
	require.Contains(
		t,
		string(content),
		"_ "+strconv.Quote("go.opentelemetry.io/otelc/instrumentation/net/http/client"),
	)

	goMod, goModErr := os.ReadFile(filepath.Join(workDir, "go.mod"))
	require.NoError(t, goModErr)

	// Ensure otelc tool directive is within the go.mod file.
	require.Contains(t, string(goMod), "go.opentelemetry.io/otelc/tool/cmd/otelc")

	// Ensure otelc require is within the go.mod file.
	require.Contains(t, string(goMod), "go.opentelemetry.io/otelc")

	// Ensure the http integration is within the go.mod file, which ensures it is pinned as a dependency.
	require.Contains(t, string(goMod), "go.opentelemetry.io/otelc/instrumentation/net/http/client")
}

func TestYAMLSelectionBuildRestoresModule(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		name := "valid"
		if invalid {
			name = "invalid_import"
		}
		t.Run(name, func(t *testing.T) {
			dir := writePinApp(t)
			yaml := "# explicitly selected\ninstrumentations:\n  - go.opentelemetry.io/otelc/instrumentation/net/http/client\n"
			if invalid {
				stale := filepath.Join(dir, "stale")
				writeTestFile(t, stale, "go.mod", "module example.com/stale\n\ngo 1.25\n")
				writeTestFile(t, stale, "stale.go", "package stale\n")
				yaml += "  - example.com/stale\n"
				modPath := filepath.Join(dir, "go.mod")
				mod, err := os.ReadFile(modPath)
				require.NoError(t, err)
				require.NoError(
					t,
					os.WriteFile(
						modPath,
						append(
							mod,
							[]byte("\nrequire example.com/stale v0.0.0\nreplace example.com/stale => ./stale\n")...),
						0o644,
					),
				)
			}
			yamlPath := filepath.Join(dir, "otel.instrumentation.yml")
			require.NoError(t, os.WriteFile(yamlPath, []byte(yaml), 0o644))
			beforeMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			require.NoError(t, err)
			beforeSum, sumErr := os.ReadFile(filepath.Join(dir, "go.sum"))
			require.True(t, sumErr == nil || os.IsNotExist(sumErr))

			otelc, err := testutil.OtelcPath()
			require.NoError(t, err)
			output := runOtelcCommand(t, dir, append(os.Environ(), "OTELC_SOURCE_ROOT="+filepath.Dir(otelc)),
				otelc, "go", "build", "-o", filepath.Join(dir, "app"), ".")
			require.NotContains(t, output, "no instrumentation will be applied")
			require.FileExists(t, filepath.Join(dir, "app"))
			matched, err := os.ReadFile(filepath.Join(dir, ".otelc-build", "matched.json"))
			require.NoError(t, err)
			require.Contains(t, string(matched), "net/http")
			gotYAML, err := os.ReadFile(yamlPath)
			require.NoError(t, err)
			require.Equal(t, yaml, string(gotYAML))
			gotMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			require.NoError(t, err)
			require.Equal(t, beforeMod, gotMod)
			gotSum, afterSumErr := os.ReadFile(filepath.Join(dir, "go.sum"))
			require.Equal(t, sumErr == nil, afterSumErr == nil)
			require.Equal(t, beforeSum, gotSum)
			require.NoFileExists(t, filepath.Join(dir, toolFileCanonical))
		})
	}
}

func TestPin_PrunesInvalidImports(t *testing.T) {
	t.Parallel()

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
}

func TestPin_PrunesImportsWithInvalidRuleFiles(t *testing.T) {
	t.Parallel()

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
}

func TestPin_KeepsInvalidImportsWithoutPruning(t *testing.T) {
	t.Parallel()

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
}

func TestPin_IsIdempotent(t *testing.T) {
	t.Parallel()

	workDir := writePinApp(t)

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
}

func TestPin_PreservesNonImportDeclarations(t *testing.T) {
	t.Parallel()

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
}

func TestAutoPin_RemovesGeneratedToolFile(t *testing.T) {
	t.Parallel()

	workDir := writePinApp(t)

	toolFile := filepath.Join(workDir, toolFileCanonical)
	assert.NoFileExists(t, toolFile)

	testutil.Build(t, workDir, ".", "go", "build", "-a")

	// AutoPin should restore the workspace after the build completes.
	assert.NoFileExists(t, toolFile)
}
