// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func TestValidateSelectedConfigurationMetadata(t *testing.T) {
	module := makeConfigurationModule(t, "example.com/instrumentation", `configurations:
  - ref: general.http.client.request_captured_headers/development
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
