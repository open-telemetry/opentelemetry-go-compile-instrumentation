// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestValidateSelectedConfigurationMetadata(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", `configurations:
  - ref: general.http.client.request_captured_headers
`)
	matched := matchedConfigurationRules(module)
	sp := newTestSetupPhase()

	err := sp.validateSelectedConfigurationMetadata(matched)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(module.dir, "hooks"), matched[0].AllFuncRules()[0].ResolvedPath)
}

func TestValidateSelectedConfigurationMetadataUnknownReference(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", `configurations:
  - ref: missing.definition
`)
	matched := matchedConfigurationRules(module)

	err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)
	require.ErrorContains(t, err, `unknown configuration ref "missing.definition"`)
	require.ErrorContains(t, err, filepath.Join(module.dir, "metadata.yaml"))
}

func TestValidateSelectedConfigurationMetadataSkipsUnselectedModules(t *testing.T) {
	selected := makeConfigurationModule(t, "example.com/selected", "")
	unselected := makeConfigurationModule(t, "example.com/unselected", `configurations:
  - ref: missing.definition
`)
	matched := matchedConfigurationRules(selected)
	assert.NotContains(t, selectedHookPackageDirs(matched), filepath.Join(unselected.dir, "hooks"))

	err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)
	require.NoError(t, err)
}

func TestValidateSelectedConfigurationMetadataReportsMetadataReadErrors(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "")
	require.NoError(t, os.Mkdir(filepath.Join(module.dir, instrumentationMetadataFilename), 0o755))
	matched := matchedConfigurationRules(module)

	err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)

	require.ErrorContains(t, err, "read selected instrumentation metadata")
}

func TestValidateSelectedConfigurationMetadataReportsParseErrors(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "configurations: [\n")
	matched := matchedConfigurationRules(module)

	err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)

	require.ErrorContains(t, err, filepath.Join(module.dir, instrumentationMetadataFilename))
	require.ErrorContains(t, err, "parse")
}

func TestValidateSelectedConfigurationMetadataWithNoMatchedHooks(t *testing.T) {
	sp := newTestSetupPhase()

	err := sp.validateSelectedConfigurationMetadata(nil)

	require.NoError(t, err)
}

func TestModuleRootForPackage(t *testing.T) {
	moduleDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module example.com/test\n"), 0o644))
	packageDir := filepath.Join(moduleDir, "nested", "hooks")
	require.NoError(t, os.MkdirAll(packageDir, 0o755))

	got, err := moduleRootForPackage(packageDir)

	require.NoError(t, err)
	assert.Equal(t, moduleDir, got)

	withoutModuleDir := t.TempDir()
	withoutModulePackage := filepath.Join(withoutModuleDir, "nested", "hooks")
	require.NoError(t, os.MkdirAll(withoutModulePackage, 0o755))
	_, err = moduleRootForPackage(withoutModulePackage)
	require.ErrorContains(t, err, "no parent go.mod found")

	notDirectory := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notDirectory, []byte("not a directory"), 0o644))
	_, err = moduleRootForPackage(filepath.Join(notDirectory, "nested"))
	require.Error(t, err)
}

func TestSelectedHookPackageDirsDeduplicatesAndSorts(t *testing.T) {
	matched := []*rule.InstRuleSet{
		nil,
		{
			FileRules: []*rule.InstFileRule{{ResolvedPath: "/z/hooks"}, {ResolvedPath: "/a/hooks"}},
			FuncRules: map[string][]*rule.InstFuncRule{
				"http.go": {{ResolvedPath: "/z/hooks"}, {ResolvedPath: ""}},
			},
		},
		{
			FuncRules: map[string][]*rule.InstFuncRule{
				"client.go": {{ResolvedPath: "/m/hooks"}},
			},
		},
	}

	assert.Equal(t, []string{"/a/hooks", "/m/hooks", "/z/hooks"}, selectedHookPackageDirs(matched))
}

func TestWarnIfNoInstrumentationMatched(t *testing.T) {
	var logs bytes.Buffer
	sp := &setupPhase{logger: slog.New(slog.NewTextHandler(&logs, nil))}

	sp.warnIfNoInstrumentationMatched(nil)

	assert.Contains(t, logs.String(), "no instrumentation rules matched any dependencies")
}

type configurationTestModule struct {
	dir      string
	hookPath string
}

func matchedConfigurationRules(module configurationTestModule) []*rule.InstRuleSet {
	return []*rule.InstRuleSet{{
		FuncRules: map[string][]*rule.InstFuncRule{
			"http.go": {{
				Path:         module.hookPath,
				ResolvedPath: filepath.Join(module.dir, "hooks"),
			}},
		},
	}}
}

func makeConfigurationModule(t *testing.T, modulePath, metadata string) configurationTestModule {
	t.Helper()
	moduleDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(moduleDir, "go.mod"),
		[]byte("module "+modulePath+"\n\ngo 1.26.0\n"),
		0o644,
	))
	hooksDir := filepath.Join(moduleDir, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "hooks.go"), []byte("package hooks\n"), 0o644))
	if metadata != "" {
		require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "metadata.yaml"), []byte(metadata), 0o644))
	}
	return configurationTestModule{dir: moduleDir, hookPath: modulePath + "/hooks"}
}

// vendoredHook is the application of makeVendoredHook.
type vendoredHook struct {
	appDir  string
	hookDir string
}

// makeVendoredHook creates an application (with a go.mod and a metadata.yaml)
// that vendors the module "example.com/hooks" and has the hook package
// "example.com/hooks/hooks". Like "go mod vendor", it does not create a go.mod
// in vendor/. It returns the application directory and the hook package
// directory.
func makeVendoredHook(t *testing.T, appMetadata, hookMetadata string) vendoredHook {
	t.Helper()
	appDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "go.mod"), []byte("module example.com/app\n"), 0o644))
	if appMetadata != "" {
		require.NoError(t, os.WriteFile(
			filepath.Join(appDir, instrumentationMetadataFilename), []byte(appMetadata), 0o644,
		))
	}
	vendorDir := filepath.Join(appDir, "vendor")
	hookDir := filepath.Join(vendorDir, "example.com", "hooks", "hooks")
	require.NoError(t, os.MkdirAll(hookDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hookDir, "hooks.go"), []byte("package hooks\n"), 0o644))
	if hookMetadata != "" {
		require.NoError(t, os.WriteFile(
			filepath.Join(vendorDir, "example.com", "hooks", instrumentationMetadataFilename),
			[]byte(hookMetadata), 0o644,
		))
	}
	modulesTxt := "# example.com/hooks v1.0.0\n## explicit; go 1.26.0\nexample.com/hooks/hooks\n"
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "modules.txt"), []byte(modulesTxt), 0o644))
	return vendoredHook{appDir: appDir, hookDir: hookDir}
}

func TestModuleRootForPackageInVendorDir(t *testing.T) {
	app := makeVendoredHook(t, "", "")
	appDir, hookDir := app.appDir, app.hookDir

	got, err := moduleRootForPackage(hookDir)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(appDir, "vendor", "example.com", "hooks"), got)

	// A nested module without packages (an explicit block with no package) is
	// not the owner of a package whose path starts with its path: the module
	// that lists the package is.
	modulesTxt := "# example.com/hooks v1.0.0\n## explicit; go 1.21\nexample.com/hooks/hooks\n" +
		"# example.com/hooks/hooks v1.0.0\n## explicit; go 1.21\n"
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "vendor", "modules.txt"), []byte(modulesTxt), 0o644))
	got, err = moduleRootForPackage(hookDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(appDir, "vendor", "example.com", "hooks"), got)

	// A package that no module of vendor/modules.txt owns is an error, rather
	// than a silent fall back to the module of the application.
	orphan := filepath.Join(appDir, "vendor", "example.com", "other", "pkg")
	require.NoError(t, os.MkdirAll(orphan, 0o755))
	_, err = moduleRootForPackage(orphan)
	require.ErrorContains(t, err, "no module of vendor/modules.txt owns package")

	require.NoError(t, os.Remove(filepath.Join(appDir, "vendor", "modules.txt")))
	_, err = moduleRootForPackage(hookDir)
	require.ErrorContains(t, err, "vendor/modules.txt")
}

func TestValidateSelectedConfigurationMetadataVendoredHookIgnoresApplicationMetadata(t *testing.T) {
	// The metadata.yaml of the application is not an otelc metadata file, and
	// must not be read in place of the one of the hook module.
	hookDir := makeVendoredHook(t, "configurations: {}\n", "").hookDir
	matched := []*rule.InstRuleSet{{
		FuncRules: map[string][]*rule.InstFuncRule{
			"http.go": {{Path: "example.com/hooks/hooks", ResolvedPath: hookDir}},
		},
	}}

	require.NoError(t, newTestSetupPhase().validateSelectedConfigurationMetadata(matched))
}

func TestValidateSelectedConfigurationMetadataVendoredHookReadsModuleMetadata(t *testing.T) {
	hookDir := makeVendoredHook(t, "", "configurations:\n  - ref: missing.definition\n").hookDir
	matched := []*rule.InstRuleSet{{
		FuncRules: map[string][]*rule.InstFuncRule{
			"http.go": {{Path: "example.com/hooks/hooks", ResolvedPath: hookDir}},
		},
	}}

	err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)

	require.ErrorContains(t, err, `unknown configuration ref "missing.definition"`)
}
