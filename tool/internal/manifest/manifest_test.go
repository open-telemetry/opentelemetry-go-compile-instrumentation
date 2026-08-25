// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
duplicate:
  target: example.com/target
  version: v1.0.0
empty:
  version: v3.0.0
`)
	writeRuleFile(t, root, "parent/client.otelc.yml", `
client:
  target: example.com/client
`)
	writeRuleFile(t, root, "parent/ranged.otelc.yaml", `
ranged:
  target: example.com/ranged
  version: v1.0.0,v2.0.0
`)
	writeRuleFile(t, root, "parent/nonmodule/worker.otelc.yaml", `
worker:
  target: example.com/worker
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
		{ModulePath: "example.com/parent", Target: "example.com/ranged", VersionRange: "v1.0.0,v2.0.0"},
		{ModulePath: "example.com/parent", Target: "example.com/target", VersionRange: "v1.0.0"},
		{ModulePath: "example.com/parent", Target: "example.com/target", VersionRange: "v2.0.0"},
		{ModulePath: "example.com/parent", Target: "example.com/worker"},
	}, got)
}

func TestGenerateRejectsInvalidVersionRanges(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		version string
		wantErr string
	}{
		{
			name:    "empty end bound",
			target:  "example.com/target",
			version: "v1.0.0,",
			wantErr: `version "v1.0.0," must use non-empty start and end bounds`,
		},
		{
			name:    "too many commas",
			target:  "example.com/target",
			version: "v1.0.0,v2.0.0,v3.0.0",
			wantErr: `version "v1.0.0,v2.0.0,v3.0.0" must contain at most one comma`,
		},
		{
			name:    "invalid semver",
			target:  "example.com/target",
			version: "not-a-version",
			wantErr: `version "not-a-version" must be a valid semantic version`,
		},
		{
			name:    "inverted bounds",
			target:  "example.com/target",
			version: "v2.0.0,v1.0.0",
			wantErr: `version "v2.0.0,v1.0.0" must have a lower bound below the upper bound`,
		},
		{
			name:    "empty target",
			version: "v1.0.0,",
			wantErr: `version "v1.0.0," must use non-empty start and end bounds`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeModule(t, root, "module", "example.com/test")
			target := ""
			if test.target != "" {
				target = "  target: " + test.target + "\n"
			}
			writeRuleFile(
				t,
				root,
				"module/otelc.yaml",
				fmt.Sprintf("invalid:\n%s  version: %s\n", target, test.version),
			)

			_, err := Generate(root)
			require.ErrorContains(t, err, test.wantErr)
			require.ErrorContains(t, err, `validating version for rule "invalid" in file otelc.yaml`)
			require.ErrorContains(t, err, "loading rules for module example.com/test")
			require.ErrorContains(t, err, "generating manifest from")
		})
	}
}

func TestGenerateReportsInvalidRulesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module", "example.com/test")
	writeRuleFile(t, root, "module/otelc.yaml", `
z-invalid:
  target: example.com/z
  version: not-a-version
a-invalid:
  target: example.com/a
  version: v1.0.0,
`)

	for range 10 {
		_, err := Generate(root)
		require.ErrorContains(t, err, `validating version for rule "a-invalid" in file otelc.yaml`)
		require.ErrorContains(t, err, `version "v1.0.0," must use non-empty start and end bounds`)
	}
}

func TestGenerateValidatesTargets(t *testing.T) {
	t.Run("accepts valid targets", func(t *testing.T) {
		tests := []struct {
			name   string
			target string
		}{
			{name: "exact", target: "example.com/target"},
			{name: "glob", target: "example.com/service/**"},
			{name: "root", target: "$root"},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				root := t.TempDir()
				writeModule(t, root, "module", "example.com/test")
				writeRuleFile(
					t,
					root,
					"module/otelc.yaml",
					fmt.Sprintf("valid:\n  target: %s\n  version: v1.0.0\n", test.target),
				)

				got, err := Generate(root)
				require.NoError(t, err)
				require.Equal(t, Manifest{
					{ModulePath: "example.com/test", Target: test.target, VersionRange: "v1.0.0"},
				}, got)
			})
		}
	})

	t.Run("omits empty target", func(t *testing.T) {
		root := t.TempDir()
		writeModule(t, root, "module", "example.com/test")
		writeRuleFile(t, root, "module/otelc.yaml", `
empty:
  target: ""
  version: v1.0.0
`)

		got, err := Generate(root)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("rejects invalid targets", func(t *testing.T) {
		tests := []struct {
			name    string
			target  string
			wantErr string
		}{
			{
				name:    "malformed glob",
				target:  "example.com/[svc",
				wantErr: `target "example.com/[svc" is not a valid glob pattern`,
			},
			{
				name:    "root target with suffix",
				target:  "$root/**",
				wantErr: `target "$root/**" must be exactly "$root"`,
			},
			{
				name:    "root target as substring",
				target:  "example.com/$root/service",
				wantErr: `target "example.com/$root/service" must be exactly "$root"`,
			},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				root := t.TempDir()
				writeModule(t, root, "module", "example.com/test")
				writeRuleFile(
					t,
					root,
					"module/otelc.yaml",
					fmt.Sprintf("invalid:\n  target: %s\n  version: v1.0.0\n", test.target),
				)

				_, err := Generate(root)
				require.ErrorContains(t, err, test.wantErr)
				require.ErrorContains(t, err, `validating target for rule "invalid" in file otelc.yaml`)
				require.ErrorContains(t, err, "loading rules for module example.com/test")
				require.ErrorContains(t, err, "generating manifest from")
			})
		}
	})
}

func TestGenerateReportsInvalidTargetsDeterministically(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module", "example.com/test")
	writeRuleFile(t, root, "module/otelc.yaml", `
z-invalid:
  target: example.com/[z
a-invalid:
  target: example.com/[a
`)

	for range 10 {
		_, err := Generate(root)
		require.ErrorContains(t, err, `validating target for rule "a-invalid" in file otelc.yaml`)
		require.ErrorContains(t, err, `target "example.com/[a" is not a valid glob pattern`)
	}
}

func TestGenerateReportsInvalidVersionBeforeTarget(t *testing.T) {
	root := t.TempDir()
	writeModule(t, root, "module", "example.com/test")
	writeRuleFile(t, root, "module/otelc.yaml", `
invalid:
  target: example.com/[svc
  version: v1.0.0,
`)

	_, err := Generate(root)
	require.ErrorContains(t, err, `validating version for rule "invalid" in file otelc.yaml`)
	require.ErrorContains(t, err, `version "v1.0.0," must use non-empty start and end bounds`)
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

func TestLoadModuleEntriesReturnsNestedModuleStatErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permissions are not enforced consistently on Windows")
	}

	root := t.TempDir()
	moduleDir := filepath.Join(root, "module")
	blockedDir := filepath.Join(moduleDir, "blocked")
	require.NoError(t, os.MkdirAll(blockedDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(moduleDir, "go.mod"),
		[]byte("module example.com/module\n"),
		0o644,
	))

	require.NoError(t, os.Chmod(blockedDir, 0))
	t.Cleanup(func() {
		_ = os.Chmod(blockedDir, 0o755)
	})

	_, err := loadModuleEntries(moduleDir, "example.com/module")
	require.Error(t, err)
}

func TestLoadModuleEntriesRejectsEscapingRuleSymlink(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "module")
	require.NoError(t, os.Mkdir(moduleDir, 0o755))
	external := filepath.Join(root, "external.otelc.yaml")
	require.NoError(t, os.WriteFile(external, []byte("rule:\n  target: example.com/external\n"), 0o644))
	if err := os.Symlink(external, filepath.Join(moduleDir, "escaped.otelc.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := loadModuleEntries(moduleDir, "example.com/module")
	require.Error(t, err)
}

func TestLoad(t *testing.T) {
	got, err := load()
	require.NoError(t, err)
	require.NotEmpty(t, got)

	for _, entry := range got {
		assert.NotEmpty(t, entry.ModulePath)
		assert.NotEmpty(t, entry.Target)
	}
	assert.True(t, slices.IsSortedFunc(got, compareEntries))
	assert.Len(t, slices.Compact(slices.Clone(got)), len(got))
}

func TestEntryOmitsEmptyVersionRange(t *testing.T) {
	content, err := json.Marshal(Entry{ModulePath: "example.com/module", Target: "example.com/target"})
	require.NoError(t, err)
	assert.NotContains(t, string(content), "versionRange")
}

func compareEntries(a, b Entry) int {
	if a.ModulePath < b.ModulePath {
		return -1
	}
	if a.ModulePath > b.ModulePath {
		return 1
	}
	if a.Target < b.Target {
		return -1
	}
	if a.Target > b.Target {
		return 1
	}
	if a.VersionRange < b.VersionRange {
		return -1
	}
	if a.VersionRange > b.VersionRange {
		return 1
	}
	return 0
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
