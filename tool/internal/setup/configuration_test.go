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

	result, err := sp.validateSelectedConfigurationMetadata(matched)
	require.NoError(t, err)
	require.Len(t, result.Definitions, 1)
	assert.Empty(t, result.ModulesWithoutMetadata)
	assert.Equal(t, filepath.Join(module.dir, "hooks"), matched[0].AllFuncRules()[0].ResolvedPath)
}

func TestValidateSelectedConfigurationMetadataUnknownReference(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", `configurations:
  - ref: missing.definition
`)
	matched := matchedConfigurationRules(module)

	_, err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)
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

	result, err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)
	require.NoError(t, err)
	assert.Equal(t, []string{selected.dir}, result.ModulesWithoutMetadata)
}

func TestValidateSelectedConfigurationMetadataReportsMetadataReadErrors(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "")
	require.NoError(t, os.Mkdir(filepath.Join(module.dir, instrumentationMetadataFilename), 0o755))
	var base rule.InstBaseRule
	base.SetModuleRoot(module.dir)
	base.SetMetadataPath(filepath.Join(module.dir, instrumentationMetadataFilename))
	set := rule.NewInstRuleSet(module.hookPath)
	set.AddFuncRule(filepath.Join(module.dir, "hooks", "hooks.go"), &rule.InstFuncRule{InstBaseRule: base})
	matched := []*rule.InstRuleSet{set}

	_, err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)

	require.ErrorContains(t, err, "read selected instrumentation metadata")
}

func TestValidateSelectedConfigurationMetadataReportsParseErrors(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "configurations: [\n")
	matched := matchedConfigurationRules(module)

	_, err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)

	require.ErrorContains(t, err, filepath.Join(module.dir, instrumentationMetadataFilename))
	require.ErrorContains(t, err, "parse")
}

func TestValidateSelectedConfigurationMetadataFileDisappearsAfterDiscovery(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "configurations: []\n")
	matched := matchedConfigurationRules(module)
	require.NoError(t, os.Remove(filepath.Join(module.dir, instrumentationMetadataFilename)))

	result, err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)

	require.NoError(t, err)
	assert.Empty(t, result.Definitions)
	assert.Equal(t, []string{module.dir}, result.ModulesWithoutMetadata)
}

func TestValidateSelectedConfigurationMetadataWithNoMatchedHooks(t *testing.T) {
	sp := newTestSetupPhase()

	result, err := sp.validateSelectedConfigurationMetadata(nil)

	require.NoError(t, err)
	assert.Empty(t, result.Definitions)
	assert.Empty(t, result.ModulesWithoutMetadata)
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

func TestFindInstrumentationMetadataForNestedInRepoModules(t *testing.T) {
	instrumentationRoot, err := filepath.Abs(filepath.Join("testdata", "instrumentation"))
	require.NoError(t, err)
	moduleBase := filepath.Join(instrumentationRoot, "github.com", "segmentio", "kafka-go")
	metadataPath := filepath.Join(moduleBase, instrumentationMetadataFilename)

	consumerRoot := filepath.Join(moduleBase, "consumer")
	producerRoot := filepath.Join(moduleBase, "producer")
	consumerMetadata, err := findInstrumentationMetadata(consumerRoot, instrumentationRoot)
	require.NoError(t, err)
	producerMetadata, err := findInstrumentationMetadata(producerRoot, instrumentationRoot)
	require.NoError(t, err)
	assert.Equal(t, metadataPath, consumerMetadata)
	assert.Equal(t, metadataPath, producerMetadata)

	consumerRules := matchedModuleConfigurationRules(consumerRoot, consumerMetadata)
	producerRules := matchedModuleConfigurationRules(producerRoot, producerMetadata)
	consumerResult, err := newTestSetupPhase().validateSelectedConfigurationMetadata([]*rule.InstRuleSet{consumerRules})
	require.NoError(t, err)
	require.Len(t, consumerResult.Definitions, 1)
	assert.Empty(t, consumerResult.ModulesWithoutMetadata)

	combinedResult, err := newTestSetupPhase().validateSelectedConfigurationMetadata(
		[]*rule.InstRuleSet{consumerRules, producerRules},
	)
	require.NoError(t, err)
	require.Len(
		t,
		combinedResult.Definitions,
		1,
		"shared metadata should be parsed once and resolved entries deduplicated",
	)
	assert.Empty(t, combinedResult.ModulesWithoutMetadata)
}

func TestFindInstrumentationMetadataDoesNotEscapeRoot(t *testing.T) {
	parent := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(parent, instrumentationMetadataFilename), []byte("configurations: []\n"), 0o644),
	)
	instrumentationRoot := filepath.Join(parent, "instrumentation")
	moduleRoot := filepath.Join(instrumentationRoot, "example.com", "module")
	require.NoError(t, os.MkdirAll(moduleRoot, 0o755))

	metadataPath, err := findInstrumentationMetadata(moduleRoot, instrumentationRoot)
	require.NoError(t, err)
	assert.Empty(t, metadataPath, "an unrelated ancestor's metadata must not be used")
}

func TestFindInstrumentationMetadataOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	_, err := findInstrumentationMetadata(outside, root)

	require.ErrorContains(t, err, "outside metadata search root")
}

func TestFindInstrumentationMetadataStatError(t *testing.T) {
	root := t.TempDir()
	notDirectory := filepath.Join(root, "not-a-directory")
	require.NoError(t, os.WriteFile(notDirectory, []byte("file"), 0o644))
	module := filepath.Join(notDirectory, "module")

	_, err := findInstrumentationMetadata(module, root)

	require.ErrorContains(t, err, "checking instrumentation metadata")
}

func TestFindInstrumentationMetadataInExternalModuleRoot(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/external", "configurations: []\n")
	metadataPath, err := findInstrumentationMetadata(module.dir, module.dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(module.dir, instrumentationMetadataFilename), metadataPath)
}

func matchedModuleConfigurationRules(moduleRoot, metadataPath string) *rule.InstRuleSet {
	var base rule.InstBaseRule
	base.SetModuleRoot(moduleRoot)
	base.SetMetadataPath(metadataPath)
	set := rule.NewInstRuleSet("example.com/app")
	set.AddCallRule(filepath.Join(moduleRoot, "hook.go"), &rule.InstCallRule{InstBaseRule: base})
	return set
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

func TestMatchedRuleTypesRetainConfigurationModuleMetadata(t *testing.T) {
	moduleRoot := filepath.Join(t.TempDir(), "module")
	sourceFile := filepath.Join(t.TempDir(), "app.go")
	metadataPath := filepath.Join(moduleRoot, instrumentationMetadataFilename)
	var base rule.InstBaseRule
	base.SetModuleRoot(moduleRoot)
	base.SetMetadataPath(metadataPath)
	assert.Equal(t, moduleRoot, base.GetModuleRoot())
	assert.Equal(t, metadataPath, base.GetMetadataPath())
	set := rule.NewInstRuleSet("example.com/app")
	set.AddRawRule(sourceFile, &rule.InstRawRule{InstBaseRule: base})
	set.AddFuncRule(sourceFile, &rule.InstFuncRule{InstBaseRule: base})
	set.AddStructRule(sourceFile, &rule.InstStructRule{InstBaseRule: base})
	set.AddCallRule(sourceFile, &rule.InstCallRule{InstBaseRule: base})
	set.AddLitRule(sourceFile, &rule.InstLitRule{InstBaseRule: base})
	set.AddDirectiveRule(sourceFile, &rule.InstDirectiveRule{InstBaseRule: base})
	set.AddDeclRule(sourceFile, &rule.InstDeclRule{InstBaseRule: base})
	set.AddFileRule(&rule.InstFileRule{InstBaseRule: base})

	assert.Equal(t, map[string]string{moduleRoot: metadataPath}, set.ConfigurationModuleMetadata())
}

func TestWarnIfNoInstrumentationMatched(t *testing.T) {
	var logs bytes.Buffer
	sp := &setupPhase{logger: slog.New(slog.NewTextHandler(&logs, nil))}

	sp.warnIfNoInstrumentationMatched(nil)

	assert.Contains(t, logs.String(), "no instrumentation rules matched any dependencies")
}

type configurationTestModule struct {
	dir         string
	hookPath    string
	hasMetadata bool
}

func matchedConfigurationRules(module configurationTestModule) []*rule.InstRuleSet {
	var base rule.InstBaseRule
	base.SetModuleRoot(module.dir)
	if module.hasMetadata {
		base.SetMetadataPath(filepath.Join(module.dir, instrumentationMetadataFilename))
	}
	set := rule.NewInstRuleSet(module.hookPath)
	set.AddFuncRule(filepath.Join(module.dir, "hooks", "hooks.go"), &rule.InstFuncRule{
		InstBaseRule: base,
		Path:         module.hookPath,
		ResolvedPath: filepath.Join(module.dir, "hooks"),
	})
	return []*rule.InstRuleSet{set}
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
	return configurationTestModule{dir: moduleDir, hookPath: modulePath + "/hooks", hasMetadata: metadata != ""}
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
	var base rule.InstBaseRule
	moduleRoot := filepath.Dir(hookDir)
	base.SetModuleRoot(moduleRoot)
	set := rule.NewInstRuleSet("example.com/hooks/hooks")
	set.AddFuncRule(filepath.Join(hookDir, "hooks.go"), &rule.InstFuncRule{
		InstBaseRule: base, Path: "example.com/hooks/hooks", ResolvedPath: hookDir,
	})
	matched := []*rule.InstRuleSet{set}

	result, err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)
	require.NoError(t, err)
	assert.Equal(t, []string{moduleRoot}, result.ModulesWithoutMetadata)
}

func TestValidateSelectedConfigurationMetadataVendoredHookReadsModuleMetadata(t *testing.T) {
	hookDir := makeVendoredHook(t, "", "configurations:\n  - ref: missing.definition\n").hookDir
	var base rule.InstBaseRule
	moduleRoot := filepath.Dir(hookDir)
	base.SetModuleRoot(moduleRoot)
	base.SetMetadataPath(filepath.Join(moduleRoot, instrumentationMetadataFilename))
	set := rule.NewInstRuleSet("example.com/hooks/hooks")
	set.AddFuncRule(filepath.Join(hookDir, "hooks.go"), &rule.InstFuncRule{
		InstBaseRule: base, Path: "example.com/hooks/hooks", ResolvedPath: hookDir,
	})
	matched := []*rule.InstRuleSet{set}

	_, err := newTestSetupPhase().validateSelectedConfigurationMetadata(matched)

	require.ErrorContains(t, err, `unknown configuration ref "missing.definition"`)
}
