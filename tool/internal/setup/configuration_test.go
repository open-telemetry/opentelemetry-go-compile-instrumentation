// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/configuration"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestValidateSelectedConfigurationMetadata(t *testing.T) {
	module := makeConfigurationModule(
		t,
		"example.com/instrumentation",
		"configurations:\n  - ref: general.http.client.request_captured_headers\n",
	)
	matched := matchedConfigurationRules(module)
	sp := setupPhaseForConfigurationModule(matched, module.dir)

	result, err := sp.validateSelectedConfigurationMetadata(matched, configuration.CatalogDefinitions)
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

	_, err := setupPhaseForConfigurationModule(matched, module.dir).validateSelectedConfigurationMetadata(
		matched, configuration.CatalogDefinitions,
	)
	require.ErrorContains(t, err, `unknown configuration ref "missing.definition"`)
	require.ErrorContains(t, err, filepath.Join(module.dir, "metadata.yaml"))
}

func TestValidateSelectedConfigurationMetadataSkipsUnselectedModules(t *testing.T) {
	selected := makeConfigurationModule(t, "example.com/selected", "")
	_ = makeConfigurationModule(t, "example.com/unselected", `configurations:
  - ref: missing.definition
`)
	matched := matchedConfigurationRules(selected)
	sp := setupPhaseForConfigurationModule(matched, selected.dir)
	assert.Equal(t, []string{selected.dir}, sp.selectedConfigurationModuleRoots(matched))
	result, err := sp.validateSelectedConfigurationMetadata(matched, configuration.CatalogDefinitions)
	require.NoError(t, err)
	assert.Equal(t, []string{selected.dir}, result.ModulesWithoutMetadata)
}

func TestValidateSelectedConfigurationMetadataReportsMetadataReadErrors(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "")
	require.NoError(t, os.Mkdir(filepath.Join(module.dir, instrumentationMetadataFilename), 0o755))
	matched := matchedConfigurationRules(module)

	_, err := setupPhaseForConfigurationModule(matched, module.dir).validateSelectedConfigurationMetadata(
		matched, configuration.CatalogDefinitions,
	)

	require.ErrorContains(t, err, "read selected instrumentation metadata")
}

func TestValidateSelectedConfigurationMetadataReportsParseErrors(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "configurations: [\n")
	matched := matchedConfigurationRules(module)

	_, err := setupPhaseForConfigurationModule(matched, module.dir).validateSelectedConfigurationMetadata(
		matched, configuration.CatalogDefinitions,
	)

	require.ErrorContains(t, err, filepath.Join(module.dir, instrumentationMetadataFilename))
	require.ErrorContains(t, err, "parse")
}

func TestValidateSelectedConfigurationMetadataReportsCatalogLoadErrors(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "configurations: []\n")
	matched := matchedConfigurationRules(module)
	sp := setupPhaseForConfigurationModule(matched, module.dir)
	loadCatalog := func() (map[string]configuration.Entry, error) {
		return nil, errors.New("catalog unavailable")
	}

	_, err := sp.validateSelectedConfigurationMetadata(matched, loadCatalog)

	require.ErrorContains(t, err, "load pinned configuration catalog")
	require.ErrorContains(t, err, "catalog unavailable")
}

func TestValidateSelectedConfigurationMetadataFileDisappearsAfterDiscovery(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "configurations: []\n")
	matched := matchedConfigurationRules(module)
	require.NoError(t, os.Remove(filepath.Join(module.dir, instrumentationMetadataFilename)))

	result, err := setupPhaseForConfigurationModule(matched, module.dir).validateSelectedConfigurationMetadata(
		matched, configuration.CatalogDefinitions,
	)

	require.NoError(t, err)
	assert.Empty(t, result.Definitions)
	assert.Equal(t, []string{module.dir}, result.ModulesWithoutMetadata)
}

func TestValidateSelectedConfigurationMetadataWithNoMatchedHooks(t *testing.T) {
	sp := newTestSetupPhase()

	result, err := sp.validateSelectedConfigurationMetadata(
		[]*rule.InstRuleSet{nil}, configuration.CatalogDefinitions,
	)

	require.NoError(t, err)
	assert.Empty(t, result.Definitions)
	assert.Empty(t, result.ModulesWithoutMetadata)
}

func TestValidateAndStoreSelectedConfigurationMetadataReturnsValidationError(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "configurations: [\n")
	matched := matchedConfigurationRules(module)
	sp := setupPhaseForConfigurationModule(matched, module.dir)

	err := sp.validateAndStoreSelectedConfigurationMetadata(matched)

	require.ErrorContains(t, err, "validating selected instrumentation configuration metadata")
	require.ErrorContains(t, err, "parse")
	assert.Empty(t, sp.configurationMetadata.Definitions)
}

func TestValidateAndStoreApplicationConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "instrumentation.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        request_captured_headers: [x-request-id]
`), 0o600))

	sp := newTestSetupPhase()
	sp.instrumentationConfigPath = configPath
	sp.configurationMetadata.Definitions = []configuration.Entry{{
		Name: "general.http.client.request_captured_headers",
		Type: "list",
	}}

	require.NoError(t, sp.loadApplicationConfiguration())
	require.NoError(t, sp.validateAndStoreApplicationConfiguration())
	assert.Equal(
		t,
		[]any{"x-request-id"},
		sp.validatedApplicationConfig.Values["general.http.client.request_captured_headers"],
	)
}

func TestValidateAndStoreApplicationConfigurationWithoutFlag(t *testing.T) {
	sp := newTestSetupPhase()

	require.NoError(t, sp.validateAndStoreApplicationConfiguration())
	assert.Empty(t, sp.validatedApplicationConfig.Values)
}

func TestValidateAndStoreApplicationConfigurationReportsReadError(t *testing.T) {
	sp := newTestSetupPhase()
	sp.instrumentationConfigPath = filepath.Join(t.TempDir(), "missing.yaml")

	err := sp.loadApplicationConfiguration()

	require.ErrorContains(t, err, "read application configuration")
}

func TestValidateAndStoreApplicationConfigurationRequiresMissingMetadata(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "instrumentation.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`file_format: "1.2"
instrumentation/development:
  general:
    http:
      client:
        request_captured_headers: [x-request-id]
`), 0o600))
	sp := newTestSetupPhase()
	sp.instrumentationConfigPath = configPath
	sp.configurationMetadata.ModulesWithoutMetadata = []string{"/instrumentation/net/http/client"}
	require.NoError(t, sp.loadApplicationConfiguration())

	err := sp.validateAndStoreApplicationConfiguration()

	require.ErrorContains(t, err, "selected instrumentation modules lack metadata.yaml")
	require.ErrorContains(t, err, "/instrumentation/net/http/client")
}

func TestLoadApplicationConfigurationReportsParseErrorsBeforeMetadataDiscovery(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "instrumentation.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("file_format: [\n"), 0o600))
	sp := newTestSetupPhase()
	sp.instrumentationConfigPath = configPath

	err := sp.loadApplicationConfiguration()

	require.ErrorContains(t, err, "parse application configuration")
	assert.Empty(t, sp.applicationConfiguration.Instrumentation)
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

func TestModuleRootForPackageInVendor(t *testing.T) {
	moduleDir := t.TempDir()
	vendorDir := filepath.Join(moduleDir, vendorDirName)
	packageDir := filepath.Join(vendorDir, "example.com", "dependency", "hooks")
	require.NoError(t, os.MkdirAll(packageDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(vendorDir, "modules.txt"),
		[]byte("# example.com/dependency v1.2.3\n## explicit; go 1.26\nexample.com/dependency/hooks\n"),
		0o644,
	))

	got, err := moduleRootForPackage(packageDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(vendorDir, "example.com", "dependency"), got)
}

func TestModuleRootForPackageInVendorReportsMissingOwner(t *testing.T) {
	moduleDir := t.TempDir()
	vendorDir := filepath.Join(moduleDir, vendorDirName)
	packageDir := filepath.Join(vendorDir, "example.com", "dependency")
	require.NoError(t, os.MkdirAll(packageDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(vendorDir, "modules.txt"),
		[]byte("# example.com/other v1.0.0\n"),
		0o644,
	))

	_, err := moduleRootForPackage(packageDir)
	require.ErrorContains(t, err, "no module of vendor/modules.txt owns package")
}

func TestSetInstrumentationConfigPath(t *testing.T) {
	sp := newTestSetupPhase()
	require.NoError(t, sp.setInstrumentationConfigPath("config/instrumentation.yaml"))
	assert.True(t, filepath.IsAbs(sp.instrumentationConfigPath))

	sp.instrumentationConfigPath = "previous"
	require.NoError(t, sp.setInstrumentationConfigPath(""))
	assert.Equal(t, "previous", sp.instrumentationConfigPath)
}

func TestLoadConfiguredApplication(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "instrumentation.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("file_format: '1.2'\n"), 0o600))

	sp := newTestSetupPhase()
	require.NoError(t, sp.loadConfiguredApplication(configPath))
	assert.Equal(t, configPath, sp.instrumentationConfigPath)
	assert.NotNil(t, sp.applicationConfiguration.Document)
	assert.Equal(t, configPath, sp.applicationConfiguration.Source)
}

func TestLoadConfiguredApplicationReturnsReadError(t *testing.T) {
	sp := newTestSetupPhase()

	err := sp.loadConfiguredApplication(filepath.Join(t.TempDir(), "missing.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read application configuration")
}

func TestValidateAndStoreApplicationConfigurationRequiresLoadedDocument(t *testing.T) {
	sp := newTestSetupPhase()
	sp.instrumentationConfigPath = "instrumentation.yaml"

	err := sp.validateAndStoreApplicationConfiguration()
	require.ErrorContains(t, err, "was not loaded before setup validation")
}

func TestValidateAndStoreApplicationConfigurationWritesWarnings(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "instrumentation.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`file_format: "1.2"
instrumentation/development:
  go:
    unselected:
      enabled: true
`), 0o600))

	sp := newTestSetupPhase()
	sp.instrumentationConfigPath = configPath
	require.NoError(t, sp.loadApplicationConfiguration())
	require.NoError(t, sp.validateAndStoreApplicationConfiguration())
	require.Len(t, sp.validatedApplicationConfig.Warnings, 1)
}

func TestValidateSelectedConfigurationMetadataForNestedInRepoModules(t *testing.T) {
	instrumentationRoot, err := filepath.Abs(filepath.Join("testdata", "instrumentation"))
	require.NoError(t, err)
	moduleBase := filepath.Join(instrumentationRoot, "github.com", "segmentio", "kafka-go")

	consumerRoot := filepath.Join(moduleBase, "consumer")
	producerRoot := filepath.Join(moduleBase, "producer")
	consumerRules, consumerRule := matchedModuleConfigurationRules(consumerRoot)
	producerRules, producerRule := matchedModuleConfigurationRules(producerRoot)
	sp := newTestSetupPhase()
	sp.recordRuleModuleRoot(consumerRule, consumerRoot)
	sp.recordRuleModuleRoot(producerRule, producerRoot)

	consumerResult, err := sp.validateSelectedConfigurationMetadata(
		[]*rule.InstRuleSet{consumerRules}, configuration.CatalogDefinitions,
	)
	require.NoError(t, err)
	require.Len(t, consumerResult.Definitions, 1)
	assert.Empty(t, consumerResult.ModulesWithoutMetadata)

	combinedResult, err := sp.validateSelectedConfigurationMetadata(
		[]*rule.InstRuleSet{consumerRules, producerRules}, configuration.CatalogDefinitions,
	)
	require.NoError(t, err)
	require.Len(
		t,
		combinedResult.Definitions,
		1,
		"equivalent definitions in separate module-local metadata should be deduplicated",
	)
	assert.Empty(t, combinedResult.ModulesWithoutMetadata)
}

func TestValidateSelectedConfigurationMetadataDoesNotUseParentMetadata(t *testing.T) {
	parent := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(parent, instrumentationMetadataFilename),
			[]byte("configurations:\n  - ref: missing.definition\n"),
			0o644,
		),
	)
	moduleRoot := filepath.Join(parent, "instrumentation", "example.com", "module")
	require.NoError(t, os.MkdirAll(moduleRoot, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(moduleRoot, goModFileName),
		[]byte("module example.com/module\n"),
		0o644,
	))
	rules, instRule := matchedModuleConfigurationRules(moduleRoot)
	sp := newTestSetupPhase()
	sp.recordRuleModuleRoot(instRule, moduleRoot)

	result, err := sp.validateSelectedConfigurationMetadata(
		[]*rule.InstRuleSet{rules}, configuration.CatalogDefinitions,
	)
	require.NoError(t, err)
	assert.Empty(t, result.Definitions)
	assert.Equal(t, []string{moduleRoot}, result.ModulesWithoutMetadata)
}

func matchedModuleConfigurationRules(moduleRoot string) (*rule.InstRuleSet, *rule.InstCallRule) {
	set := rule.NewInstRuleSet("example.com/app")
	instRule := &rule.InstCallRule{}
	set.AddCallRule(filepath.Join(moduleRoot, "hook.go"), instRule)
	return set, instRule
}

func TestSelectedConfigurationModuleRootsDeduplicatesAndSorts(t *testing.T) {
	root := t.TempDir()
	zModule := filepath.Join(root, "z", "module")
	aModule := filepath.Join(root, "a", "module")
	setZ, ruleZ1 := matchedModuleConfigurationRules(zModule)
	setA, ruleA := matchedModuleConfigurationRules(aModule)
	setZAgain, ruleZ2 := matchedModuleConfigurationRules(zModule)
	sp := newTestSetupPhase()
	sp.recordRuleModuleRoot(ruleZ1, zModule)
	sp.recordRuleModuleRoot(ruleA, aModule)
	sp.recordRuleModuleRoot(ruleZ2, zModule)

	got := sp.selectedConfigurationModuleRoots([]*rule.InstRuleSet{setZ, nil, setA, setZAgain})

	assert.Equal(t, []string{aModule, zModule}, got)
}

func TestMatchedRuleTypesRetainModuleOwnershipOutsideRules(t *testing.T) {
	moduleRoot := filepath.Join(t.TempDir(), "module")
	sourceFile := filepath.Join(t.TempDir(), "app.go")
	set := rule.NewInstRuleSet("example.com/app")
	rules := []rule.InstRule{
		&rule.InstRawRule{},
		&rule.InstFuncRule{},
		&rule.InstStructRule{},
		&rule.InstCallRule{},
		&rule.InstLitRule{},
		&rule.InstDirectiveRule{},
		&rule.InstDeclRule{},
		&rule.InstFileRule{},
	}
	set.AddRawRule(sourceFile, rules[0].(*rule.InstRawRule))
	set.AddFuncRule(sourceFile, rules[1].(*rule.InstFuncRule))
	set.AddStructRule(sourceFile, rules[2].(*rule.InstStructRule))
	set.AddCallRule(sourceFile, rules[3].(*rule.InstCallRule))
	set.AddLitRule(sourceFile, rules[4].(*rule.InstLitRule))
	set.AddDirectiveRule(sourceFile, rules[5].(*rule.InstDirectiveRule))
	set.AddDeclRule(sourceFile, rules[6].(*rule.InstDeclRule))
	set.AddFileRule(rules[7].(*rule.InstFileRule))

	sp := newTestSetupPhase()
	for instRule := range set.AllRules() {
		sp.recordRuleModuleRoot(instRule, moduleRoot)
	}
	result, err := sp.validateSelectedConfigurationMetadata(
		[]*rule.InstRuleSet{set}, configuration.CatalogDefinitions,
	)
	require.NoError(t, err)
	assert.Equal(t, []string{moduleRoot}, result.ModulesWithoutMetadata)
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
	set := rule.NewInstRuleSet(module.hookPath)
	set.AddFuncRule(filepath.Join(module.dir, "hooks", "hooks.go"), &rule.InstFuncRule{
		Path:         module.hookPath,
		ResolvedPath: filepath.Join(module.dir, "hooks"),
	})
	return []*rule.InstRuleSet{set}
}

func setupPhaseForConfigurationModule(matched []*rule.InstRuleSet, moduleRoot string) *setupPhase {
	sp := newTestSetupPhase()
	for _, ruleset := range matched {
		if ruleset == nil {
			continue
		}
		for _, rules := range ruleset.FuncRules {
			for _, instRule := range rules {
				sp.recordRuleModuleRoot(instRule, moduleRoot)
			}
		}
	}
	return sp
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
