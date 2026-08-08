// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/data"
)

func TestGenerate(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "parent", "example.com/parent")
	writeRuleFile(t, root, "parent/otelc.yaml", `
later:
  target: example.com/target
  version: v2.0.0
earlier:
  target: example.com/target
  version: v1.0.0
empty:
  version: v3.0.0
`)
	writeRuleFile(t, root, "parent/client.otelc.yml", `
client:
  target: example.com/client
`)
	writeRuleFile(t, root, "parent/ignored.yaml", `
ignored:
  target: example.com/ignored
`)

	writeModule(t, root, "parent/nested", "example.com/nested")
	writeRuleFile(t, root, "parent/nested/server.otelc.yaml", `
server:
  target: example.com/server
  version: v1.5.0
`)

	got, err := Generate(root)
	require.NoError(t, err)
	require.Equal(t, Manifest{
		{ModulePath: "example.com/nested", Target: "example.com/server", VersionRange: "v1.5.0"},
		{ModulePath: "example.com/parent", Target: "example.com/client"},
		{ModulePath: "example.com/parent", Target: "example.com/target", VersionRange: "v1.0.0"},
		{ModulePath: "example.com/parent", Target: "example.com/target", VersionRange: "v2.0.0"},
	}, got)
}

func TestGenerateErrors(t *testing.T) {
	tests := []struct {
		name    string
		goMod   string
		rule    string
		wantErr string
	}{
		{
			name:    "invalid go.mod",
			goMod:   "invalid",
			wantErr: "parsing",
		},
		{
			name:    "missing module directive",
			goMod:   "go 1.25\n",
			wantErr: "has no module directive",
		},
		{
			name:    "invalid rule YAML",
			goMod:   "module example.com/test\n",
			rule:    "invalid: yaml: {",
			wantErr: "parsing rule file",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			moduleDir := filepath.Join(root, "module")
			require.NoError(t, os.Mkdir(moduleDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(test.goMod), 0o644))
			if test.rule != "" {
				require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "otelc.yaml"), []byte(test.rule), 0o644))
			}

			_, err := Generate(root)
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestGenerateMissingRoot(t *testing.T) {
	_, err := Generate(filepath.Join(t.TempDir(), "missing"))
	require.ErrorContains(t, err, "generating manifest")
}

func TestLoadModulePathMissingFile(t *testing.T) {
	_, err := loadModulePath(filepath.Join(t.TempDir(), "go.mod"))
	require.ErrorContains(t, err, "reading")
}

func TestLoadModuleEntriesMissingDirectory(t *testing.T) {
	_, err := loadModuleEntries(filepath.Join(t.TempDir(), "missing"), "example.com/missing")
	require.ErrorContains(t, err, "opening module root")
}

func TestIsRuleFile(t *testing.T) {
	tests := map[string]bool{
		"otelc.yaml":        true,
		"otelc.yml":         true,
		"client.otelc.yaml": true,
		"server.otelc.yml":  true,
		"rules.yaml":        false,
		"otelc.client.yaml": false,
		"otelc":             false,
		"otelc.txt":         false,
		"otelc.yaml.bak":    false,
	}

	for filename, expected := range tests {
		t.Run(filename, func(t *testing.T) {
			assert.Equal(t, expected, isRuleFile(filename))
		})
	}
}

func TestLoad(t *testing.T) {
	got, err := Load()
	require.NoError(t, err)
	require.NotEmpty(t, got)

	var decoded Manifest
	require.NoError(t, json.Unmarshal(data.GetManifestJSON(), &decoded))
	require.Equal(t, decoded, got)
}

func writeModule(t *testing.T, root, relative, modulePath string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(relative))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module "+modulePath+"\n"),
		0o644,
	))
}

func writeRuleFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
