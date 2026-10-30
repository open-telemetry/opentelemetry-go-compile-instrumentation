// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// callRuleWithImports returns a call rule whose JSON unmarshals cleanly; the
// selector must parse, so it names a real package function shape.
func callRuleWithImports(imports map[string]string) *rule.InstCallRule {
	return &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{
			Name: "call-rule", Imports: imports,
		},
		FunctionCall: "example.com/b.New",
	}
}

// writeMatchedRules stores matched.json the way setup does, so the link phase
// can read the rule-added imports back.
func writeMatchedRules(t *testing.T, sets []*rule.InstRuleSet) {
	t.Helper()
	content, err := json.Marshal(sets)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), content, 0o600))
}

// linkArgs returns a minimal link command with importcfg at cfgPath.
func linkArgs(cfgPath string) []string {
	return []string{"link", "-o", "out.test", "-buildid=test", "-importcfg", cfgPath}
}

// TestInterceptLinkRecoversRuleAddedImport covers the warm-GOCACHE shape: no
// compile ran, so no per-process tracking file exists, and the link must
// still receive the import a rule adds, recovered from matched.json.
func TestInterceptLinkRecoversRuleAddedImportFromMatchedRules(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{
					InstBaseRule: rule.InstBaseRule{
						Name:    "rule",
						Target:  rule.NewTarget("example.com/app/dep"),
						Imports: map[string]string{"extra": "fmt"},
					},
				}},
			},
		},
	})

	cfgPath := filepath.Join(workDir, "importcfg.link")
	require.NoError(t, os.WriteFile(cfgPath, []byte("packagefile context=/unused/context.a\n"), 0o644))

	_, err := interceptLink(t.Context(), linkArgs(cfgPath))
	require.NoError(t, err)

	content, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "packagefile fmt=")
}

// TestInterceptLinkSkipsImportsTheLinkAlreadyHas checks that a link importcfg
// listing every rule-added import is left untouched, and that a build with no
// matched rules changes nothing.
func TestInterceptLinkLeavesCompleteImportcfgUnchanged(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{
					InstBaseRule: rule.InstBaseRule{
						Name:    "rule",
						Target:  rule.NewTarget("example.com/app/dep"),
						Imports: map[string]string{"extra": "fmt"},
					},
				}},
			},
		},
	})

	cfgPath := filepath.Join(workDir, "importcfg.link")
	original := "packagefile fmt=/unused/fmt.a\npackagefile context=/unused/context.a\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(original), 0o644))

	_, err := interceptLink(t.Context(), linkArgs(cfgPath))
	require.NoError(t, err)

	content, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, original, string(content))

	// Without matched.json there is nothing to recover either.
	require.NoError(t, os.Remove(util.GetMatchedRuleFile()))
	_, err = interceptLink(t.Context(), linkArgs(cfgPath))
	require.NoError(t, err)
	content, err = os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, original, string(content))
}

// TestRuleAddedImportsCollectsEveryRuleKind checks that the link-side
// candidate set covers the imports of every rule kind and of add_file rule
// sources, and skips the built-in pseudo-packages.
func TestRuleAddedImportsCollectsEveryRuleKind(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	sourceDir := filepath.Join(workDir, "hooksrc")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	sourceFile := filepath.Join(sourceDir, "added.go")
	require.NoError(t, os.WriteFile(sourceFile, []byte(
		"//go:build ignore\n\npackage hooks\n\nimport (\n\t\"fmt\"\n\t\"example.com/hooks\"\n)\n",
	), 0o644))

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{InstBaseRule: rule.InstBaseRule{
					Name: "func-rule", Imports: map[string]string{"a": "example.com/a"},
				}}},
			},
			CallRules: map[string][]*rule.InstCallRule{
				"dep.go": {callRuleWithImports(map[string]string{"b": "example.com/b"})},
			},
			RawRules: map[string][]*rule.InstRawRule{
				"dep.go": {{InstBaseRule: rule.InstBaseRule{
					Name: "raw-rule", Imports: map[string]string{"c": "unsafe"},
				}}},
			},
			FileRules: []*rule.InstFileRule{{
				InstBaseRule: rule.InstBaseRule{Name: "file-rule"},
				File:         "added.go",
				Path:         "example.com/app/hooks",
				ResolvedPath: sourceDir,
			}},
		},
	})

	paths := ruleAddedImports(t.Context())
	assert.Equal(t, map[string]struct{}{
		"example.com/a":     {},
		"example.com/b":     {},
		"unsafe":            {},
		"fmt":               {},
		"example.com/hooks": {},
	}, paths)
}

// TestResolveRuleAddedImportsSkipsUnresolvableImports checks that a candidate
// the rules declare but that cannot load does not fail the link, while a
// resolvable one still lands in the import config.
func TestResolveRuleAddedImportsSkipsUnresolvableImports(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	linkConfig := imports.ImportConfig{
		PackageFile: map[string]string{"context": "/unused/context.a"},
	}
	updated, err := resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.False(t, updated)

	writeMatchedRules(t, []*rule.InstRuleSet{
		{
			PackageName: "dep",
			ModulePath:  "example.com/app/dep",
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{InstBaseRule: rule.InstBaseRule{
					Name: "rule",
					Imports: map[string]string{
						"std":          "fmt",
						"unresolvable": "example.invalid/nonexistent/pkg",
					},
				}}},
			},
		},
	})

	updated, err = resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Contains(t, linkConfig.PackageFile, "fmt")
	assert.NotContains(t, linkConfig.PackageFile, "example.invalid/nonexistent/pkg")
}
