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
	matched := []*rule.InstRuleSet{{
		FuncRules: map[string][]*rule.InstFuncRule{
			"http.go": {{Path: module.hookPath}},
		},
	}}
	sp := newTestSetupPhase()

	err := sp.validateSelectedConfigurationMetadata(t.Context(), matched, []string{module.dir})
	require.NoError(t, err)
	assert.True(t, sp.rulePathsResolved)
	assert.Equal(t, filepath.Join(module.dir, "hooks"), matched[0].AllFuncRules()[0].ResolvedPath)
}

func TestValidateSelectedConfigurationMetadataUnknownReference(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", `configurations:
  - ref: missing.definition
`)
	matched := []*rule.InstRuleSet{{
		FuncRules: map[string][]*rule.InstFuncRule{
			"http.go": {{Path: module.hookPath}},
		},
	}}

	err := newTestSetupPhase().validateSelectedConfigurationMetadata(t.Context(), matched, []string{module.dir})
	require.ErrorContains(t, err, `unknown configuration ref "missing.definition"`)
	require.ErrorContains(t, err, filepath.Join(module.dir, "metadata.yaml"))
}

func TestValidateSelectedConfigurationMetadataSkipsUnselectedModules(t *testing.T) {
	selected := makeConfigurationModule(t, "example.com/selected", "")
	unselected := makeConfigurationModule(t, "example.com/unselected", `configurations:
  - ref: missing.definition
`)
	matched := []*rule.InstRuleSet{{
		FuncRules: map[string][]*rule.InstFuncRule{
			"http.go": {{Path: selected.hookPath}},
		},
	}}

	err := newTestSetupPhase().validateSelectedConfigurationMetadata(
		t.Context(),
		matched,
		[]string{selected.dir, unselected.dir},
	)
	require.NoError(t, err)
}

func TestValidateSelectedConfigurationMetadataRejectsUnresolvableHookPackage(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "")
	matched := []*rule.InstRuleSet{{
		FuncRules: map[string][]*rule.InstFuncRule{
			"http.go": {{Path: "example.com/instrumentation/missing"}},
		},
	}}
	sp := newTestSetupPhase()

	err := sp.validateSelectedConfigurationMetadata(t.Context(), matched, []string{module.dir})

	require.ErrorContains(t, err, "resolving selected instrumentation packages")
	assert.False(t, sp.rulePathsResolved)
}

func TestValidateSelectedConfigurationMetadataReportsMetadataReadErrors(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "")
	require.NoError(t, os.Mkdir(filepath.Join(module.dir, instrumentationMetadataFilename), 0o755))
	matched := []*rule.InstRuleSet{{
		FuncRules: map[string][]*rule.InstFuncRule{
			"http.go": {{Path: module.hookPath}},
		},
	}}

	err := newTestSetupPhase().validateSelectedConfigurationMetadata(t.Context(), matched, []string{module.dir})

	require.ErrorContains(t, err, "read selected instrumentation metadata")
}

func TestValidateSelectedConfigurationMetadataReportsParseErrors(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", "configurations: [\n")
	matched := []*rule.InstRuleSet{{
		FuncRules: map[string][]*rule.InstFuncRule{
			"http.go": {{Path: module.hookPath}},
		},
	}}

	err := newTestSetupPhase().validateSelectedConfigurationMetadata(t.Context(), matched, []string{module.dir})

	require.ErrorContains(t, err, filepath.Join(module.dir, instrumentationMetadataFilename))
	require.ErrorContains(t, err, "parse")
}

func TestValidateSelectedConfigurationMetadataWithNoMatchedHooks(t *testing.T) {
	sp := newTestSetupPhase()

	err := sp.validateSelectedConfigurationMetadata(t.Context(), nil, nil)

	require.NoError(t, err)
	assert.True(t, sp.rulePathsResolved)
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
