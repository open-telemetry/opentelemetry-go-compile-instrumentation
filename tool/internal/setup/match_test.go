// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type mockInstRule struct {
	rule.InstBaseRule
}

func (r *mockInstRule) String() string {
	return r.Name
}

func TestNormalizeRule(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string]any
		expect    []map[string]any
		expectErr string
	}{
		{
			name: "flat format passthrough",
			input: map[string]any{
				"target": "net/http",
				"func":   "ServeHTTP",
				"before": "BeforeHook",
				"path":   "github.com/example/pkg",
			},
			expect: []map[string]any{{
				"target": "net/http",
				"func":   "ServeHTTP",
				"before": "BeforeHook",
				"path":   "github.com/example/pkg",
			}},
		},
		{
			name: "top-level target version with where selectors and where.file",
			input: map[string]any{
				"target":  "database/sql",
				"version": "v1.0.0,v2.0.0",
				"where": map[string]any{
					"func": "Open",
					"file": map[string]any{
						"has_func": "init",
					},
				},
				"do": []any{
					map[string]any{"inject_hooks": map[string]any{
						"before": "BeforeServeHTTP",
						"after":  "AfterServeHTTP",
						"path":   "github.com/example/pkg",
					}},
				},
			},
			expect: []map[string]any{{
				"target":  "database/sql",
				"version": "v1.0.0,v2.0.0",
				"func":    "Open",
				"before":  "BeforeServeHTTP",
				"after":   "AfterServeHTTP",
				"path":    "github.com/example/pkg",
				"where": map[string]any{
					"file": map[string]any{
						"has_func": "init",
					},
				},
			}},
		},
		{
			name: "multiple do items preserve declaration order",
			input: map[string]any{
				"target": "main",
				"where": map[string]any{
					"func": "Example",
				},
				"do": []any{
					map[string]any{"inject_hooks": map[string]any{
						"before": "BeforeHook",
						"path":   "example.com/hooks",
					}},
					map[string]any{"inject_code": map[string]any{
						"raw": "defer func(){}()",
					}},
				},
			},
			expect: []map[string]any{
				{
					"target": "main",
					"func":   "Example",
					"before": "BeforeHook",
					"path":   "example.com/hooks",
				},
				{
					"target": "main",
					"func":   "Example",
					"raw":    "defer func(){}()",
				},
			},
		},
		{
			name: "where one-of and not are preserved for later phases",
			input: map[string]any{
				"target": "main",
				"where": map[string]any{
					"func": "Open",
					"one-of": []any{
						map[string]any{"file": map[string]any{"has_func": "init"}},
						map[string]any{"not": map[string]any{"directive": "otelc:ignore"}},
					},
				},
				"do": []any{
					map[string]any{"inject_hooks": map[string]any{
						"before": "BeforeOpen",
						"path":   "example.com/hooks",
					}},
				},
			},
			expect: []map[string]any{{
				"target": "main",
				"func":   "Open",
				"before": "BeforeOpen",
				"path":   "example.com/hooks",
				"where": map[string]any{
					"one-of": []any{
						map[string]any{"file": map[string]any{"has_func": "init"}},
						map[string]any{"not": map[string]any{"directive": "otelc:ignore"}},
					},
				},
			}},
		},
		{
			name: "repeated modifier kinds are allowed",
			input: map[string]any{
				"target": "main",
				"where": map[string]any{
					"func": "Example",
				},
				"do": []any{
					map[string]any{"inject_hooks": map[string]any{
						"before": "BeforeOne",
						"path":   "example.com/hooks",
					}},
					map[string]any{"inject_hooks": map[string]any{
						"before": "BeforeTwo",
						"path":   "example.com/hooks",
					}},
				},
			},
			expect: []map[string]any{
				{
					"target": "main",
					"func":   "Example",
					"before": "BeforeOne",
					"path":   "example.com/hooks",
				},
				{
					"target": "main",
					"func":   "Example",
					"before": "BeforeTwo",
					"path":   "example.com/hooks",
				},
			},
		},
		{
			name: "do map form is sugar for one-element list",
			input: map[string]any{
				"target": "main",
				"where": map[string]any{
					"func": "Example",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeHook",
						"path":   "example.com/hooks",
					},
				},
			},
			expect: []map[string]any{{
				"target": "main",
				"func":   "Example",
				"before": "BeforeHook",
				"path":   "example.com/hooks",
			}},
		},
		{
			name: "do map form with multiple keys rejected",
			input: map[string]any{
				"target": "main",
				"where":  map[string]any{"func": "Example"},
				"do": map[string]any{
					"inject_hooks": map[string]any{"before": "BeforeHook"},
					"inject_code":  map[string]any{"raw": "_ = 0"},
				},
			},
			expectErr: "exactly one modifier key when written as a map",
		},
		{
			name: "target in where rejected",
			input: map[string]any{
				"target": "main",
				"where": map[string]any{
					"target": "net/http",
					"func":   "ServeHTTP",
				},
				"do": []any{
					map[string]any{"inject_hooks": map[string]any{
						"before": "BeforeHook",
						"path":   "example.com/hooks",
					}},
				},
			},
			expectErr: "target must be top-level",
		},
		{
			name: "missing do rejected",
			input: map[string]any{
				"target": "main",
				"where":  map[string]any{"func": "Fn"},
			},
			expectErr: "missing do",
		},
		{
			name: "empty do rejected",
			input: map[string]any{
				"target": "main",
				"where":  map[string]any{"func": "Fn"},
				"do":     []any{},
			},
			expectErr: "do must not be empty",
		},
		{
			name: "invalid do item with multiple keys rejected",
			input: map[string]any{
				"target": "main",
				"where":  map[string]any{"func": "Fn"},
				"do": []any{
					map[string]any{
						"inject_hooks": map[string]any{"before": "BeforeHook"},
						"inject_code":  map[string]any{"raw": "_ = 0"},
					},
				},
			},
			expectErr: "exactly one modifier key",
		},
		{
			name: "malformed where.file rejected",
			input: map[string]any{
				"target": "main",
				"where": map[string]any{
					"func": "Fn",
					"file": "not-a-map",
				},
				"do": []any{
					map[string]any{"inject_hooks": map[string]any{
						"before": "BeforeHook",
						"path":   "example.com/hooks",
					}},
				},
			},
			expectErr: "where.file must be a map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rule.Normalize(tt.input)
			if tt.expectErr != "" {
				require.ErrorContains(t, err, tt.expectErr)
				return
			}
			require.NoError(t, err)
			wantYAML, _ := yaml.Marshal(tt.expect)
			gotYAML, _ := yaml.Marshal(got)
			require.YAMLEq(t, string(wantYAML), string(gotYAML))
		})
	}
}

func TestMatchVersion(t *testing.T) {
	tests := []struct {
		name           string
		dependency     *Dependency
		ruleVersion    string
		expectedResult bool
	}{
		{
			name: "no version specified in rule - always matches",
			dependency: &Dependency{
				Version: "v1.5.0",
			},
			ruleVersion:    "",
			expectedResult: true,
		},
		{
			name: "version exactly at start of range",
			dependency: &Dependency{
				Version: "v1.0.0",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name: "version in middle of range",
			dependency: &Dependency{
				Version: "v1.5.0",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name: "version just before end of range",
			dependency: &Dependency{
				Version: "v1.9.9",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name: "version exactly at end of range - excluded",
			dependency: &Dependency{
				Version: "v2.0.0",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name: "version after end of range",
			dependency: &Dependency{
				Version: "v2.1.0",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name: "version before start of range",
			dependency: &Dependency{
				Version: "v0.9.0",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name: "pre-release version in range",
			dependency: &Dependency{
				Version: "v1.5.0-alpha",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name: "patch version in range",
			dependency: &Dependency{
				Version: "v1.5.3",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name: "major version jump",
			dependency: &Dependency{
				Version: "v3.0.0",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name: "zero major version",
			dependency: &Dependency{
				Version: "v0.5.0",
			},
			ruleVersion:    "v0.1.0,v1.0.0",
			expectedResult: true,
		},
		{
			name: "narrow version range",
			dependency: &Dependency{
				Version: "v1.2.3",
			},
			ruleVersion:    "v1.2.0,v1.3.0",
			expectedResult: true,
		},
		{
			name: "version with build metadata",
			dependency: &Dependency{
				Version: "v1.5.0+build123",
			},
			ruleVersion:    "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name: "minimal version only - good",
			dependency: &Dependency{
				Version: "v1.2.3",
			},
			ruleVersion:    "v1.2.3",
			expectedResult: true,
		},
		{
			name: "minimal version only - bad",
			dependency: &Dependency{
				Version: "v1.2.3",
			},
			ruleVersion:    "v1.2.4",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &mockInstRule{
				InstBaseRule: rule.InstBaseRule{
					Version: tt.ruleVersion,
				},
			}

			result := matchVersion(tt.dependency, rule)
			if result != tt.expectedResult {
				t.Errorf("matchVersion() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestCreateRuleFromFields(t *testing.T) {
	tests := []struct {
		name         string
		yamlContent  string
		ruleName     string
		expectError  bool
		expectedType string
	}{
		{
			name: "struct rule creation",
			yamlContent: `
struct: TestStruct
target: github.com/example/lib
`,
			ruleName:     "test-struct-rule",
			expectError:  false,
			expectedType: "*rule.InstStructRule",
		},
		{
			name: "func rule creation",
			yamlContent: `
func: TestFunc
target: github.com/example/lib
before: MyHook1Before
`,
			ruleName:     "test-func-rule",
			expectError:  false,
			expectedType: "*rule.InstFuncRule",
		},
		{
			name: "file rule creation",
			yamlContent: `
file: test.go
target: github.com/example/lib
`,
			ruleName:     "test-file-rule",
			expectError:  false,
			expectedType: "*rule.InstFileRule",
		},
		{
			name: "raw rule creation",
			yamlContent: `
raw: test
target: github.com/example/lib
`,
			ruleName:     "test-raw-rule",
			expectError:  false,
			expectedType: "*rule.InstRawRule",
		},
		{
			name: "rule with version",
			yamlContent: `
struct: TestStruct
target: github.com/example/lib
version: v1.0.0,v2.0.0
`,
			ruleName:     "test-versioned-rule",
			expectError:  false,
			expectedType: "*rule.InstStructRule",
		},
		{
			name: "directive rule creation",
			yamlContent: `
directive: "otelc:span"
target: github.com/example/lib
template: "_ = 0"
`,
			ruleName:     "test-directive-rule",
			expectError:  false,
			expectedType: "*rule.InstDirectiveRule",
		},
		{
			name: "directive rule missing field",
			yamlContent: `
directive: ""
target: github.com/example/lib
`,
			ruleName:    "test-invalid-directive-rule",
			expectError: true,
		},
		{
			name: "decl rule creation",
			yamlContent: `
target: github.com/example/lib
identifier: GlobalVar
value: "replaced"
`,
			ruleName:     "test-decl-rule",
			expectError:  false,
			expectedType: "*rule.InstDeclRule",
		},
		{
			name: "invalid yaml syntax",
			yamlContent: `
struct: [
target: github.com/example/lib
`,
			ruleName:    "test-invalid-rule",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCreateRuleFromFieldsCase(t, tt)
		})
	}
}

func testCreateRuleFromFieldsCase(t *testing.T, tt struct {
	name         string
	yamlContent  string
	ruleName     string
	expectError  bool
	expectedType string
},
) {
	var fields map[string]any
	err := yaml.Unmarshal([]byte(tt.yamlContent), &fields)
	if err != nil {
		if !tt.expectError {
			t.Fatalf("failed to parse test YAML: %v", err)
		}
		return // Expected YAML parsing to fail
	}

	createdRule, err := createRuleFromFields([]byte(tt.yamlContent), tt.ruleName, fields)

	if tt.expectError {
		if err == nil {
			t.Error("expected error but got none")
		}
		return
	}

	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}

	if createdRule == nil {
		return
	}

	validateCreatedRule(t, createdRule, tt.ruleName, fields)
}

func validateCreatedRule(t *testing.T, createdRule rule.InstRule, ruleName string, fields map[string]any) {
	if createdRule.GetName() != ruleName {
		t.Errorf("rule name = %v, want %v", createdRule.GetName(), ruleName)
	}

	if target, ok := fields["target"].(string); ok {
		if createdRule.GetTarget() != target {
			t.Errorf("rule target = %v, want %v", createdRule.GetTarget(), target)
		}
	}

	if version, ok := fields["version"].(string); ok {
		if createdRule.GetVersion() != version {
			t.Errorf("rule version = %v, want %v", createdRule.GetVersion(), version)
		}
	}
}

func writeCustomRules(t *testing.T, name, content string) string {
	path := filepath.Join(t.TempDir(), name)
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
	return path
}

func TestRuleFilesFromDir(t *testing.T) {
	content1 := `h1:
  target: main
  func: Example
  raw: "_ = 1"`
	content2 := `h2:
  target: main
  func: Example
  raw: "_ = 1"`

	// Manually make a temporary and sub temporary Directories
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub_dir")

	err := os.Mkdir(subDir, 0o755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "r1.otelc.yaml"), []byte(content1), 0o644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(subDir, "r2.otelc.yaml"), []byte(content2), 0o644)
	require.NoError(t, err)

	t.Setenv(util.EnvOtelcRules, "")

	sp := newTestSetupPhase()
	err = sp.extract()
	require.NoError(t, err)

	sp.ruleConfig = dir

	rules, err := sp.loadRules()
	require.NoError(t, err)
	require.Len(t, rules, 2)
}

func TestMultipleRuleFiles(t *testing.T) {
	content1 := `h1:
  target: main
  func: Example
  raw: "_ = 1"`
	content2 := `h2:
  target: main
  func: Example
  raw: "_ = 1"`

	p1 := writeCustomRules(t, "r1.yaml", content1)
	p2 := writeCustomRules(t, "r2.yaml", content2)

	t.Setenv(util.EnvOtelcRules, "")

	sp := newTestSetupPhase()
	err := sp.extract()
	require.NoError(t, err)

	sp.ruleConfig = p1 + "," + p2

	rules, err := sp.loadRules()
	require.NoError(t, err)
	require.Len(t, rules, 2)
	names := []string{
		rules[0].GetName(),
		rules[1].GetName(),
	}
	require.Contains(t, names, "h1")
	require.Contains(t, names, "h2")

	// Check for duplicate rule by name
	sp = newTestSetupPhase()
	err = sp.extract()
	require.NoError(t, err)

	sp.ruleConfig = p1 + "," + p1

	rules, err = sp.loadRules()
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "h1", rules[0].GetName())
}

func TestLoadDefaultRules(t *testing.T) {
	// Write custom rules to temporary files
	content1 := `h1:
  target: main
  func: Example
  raw: "_ = 1"`
	content2 := `h2:
  target: main
  func: Example
  raw: "_ = 1"`
	p1 := writeCustomRules(t, "r1.yaml", content1)
	p2 := writeCustomRules(t, "r2.yaml", content2)
	t.Setenv(util.EnvOtelcRules, p1)

	// Prepare setup phase and set custom rules via environment variable and flag
	sp := newTestSetupPhase()
	err := sp.extract()
	require.NoError(t, err)
	sp.ruleConfig = p2

	// Verify that the custom rule specified by environment variable has
	// higher priority than the custom rule specified by flag
	rules, err := sp.loadRules()
	require.NoError(t, err)
	require.NotEmpty(t, rules)
	require.Len(t, rules, 1)
	require.Equal(t, "h1", rules[0].GetName())

	// Verify that the custom rule specified by flag has higher priority than
	// default rules
	t.Setenv(util.EnvOtelcRules, "")
	rules, err = sp.loadRules()
	require.NoError(t, err)
	require.NotEmpty(t, rules)
	require.Len(t, rules, 1)
	require.Equal(t, "h2", rules[0].GetName())

	// Verify that the default rules are loaded
	t.Setenv(util.EnvOtelcRules, "")
	sp.ruleConfig = ""

	rules, err = sp.loadRules()
	require.NoError(t, err)
	require.NotEmpty(t, rules)
	require.Greater(t, len(rules), 1, "default rules should be more than 1")
}

func TestPreciseMatching_WhereFileFilter(t *testing.T) {
	matchFile := writeGoSource(t, "match.go", "package main\n\ntype Server struct{}\n\nfunc Handler() {}\n")
	noMatchFile := writeGoSource(t, "nomatch.go", "package main\n\nfunc Handler() {}\n")

	dep := &Dependency{
		ImportPath: "example.com/svc",
		Sources:    []string{matchFile, noMatchFile},
	}

	funcRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "test-where-file",
			Target: "example.com/svc",
			Where: &rule.WhereDef{
				File: &rule.FilterDef{HasStruct: "Server"},
			},
		},
		Func:   "Handler",
		Before: "BeforeHandler",
		Path:   "example.com/hooks",
	}

	sp := newTestSetupPhase()
	set := rule.NewInstRuleSet(dep.ImportPath)

	result, err := sp.preciseMatching(t.Context(), dep, []rule.InstRule{funcRule}, set)
	require.NoError(t, err)
	require.Len(t, result.FuncRules, 1)
	require.Contains(t, result.FuncRules, matchFile)
}

func TestPreciseMatching_WhereFileFilterBuildError(t *testing.T) {
	srcFile := writeGoSource(t, "src.go", "package main\n\nfunc Foo() {}\n")

	dep := &Dependency{
		ImportPath: "example.com/svc",
		Sources:    []string{srcFile},
	}

	badRule := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "bad-where-file",
			Target: "example.com/svc",
			Where: &rule.WhereDef{
				File: &rule.FilterDef{HasFunc: "Foo", HasStruct: "Bar"},
			},
		},
		Func: "Foo",
	}

	sp := newTestSetupPhase()
	set := rule.NewInstRuleSet(dep.ImportPath)

	_, err := sp.preciseMatching(t.Context(), dep, []rule.InstRule{badRule}, set)
	require.Error(t, err)
	require.ErrorContains(t, err, "where.file has multiple active predicates")
}

// Helper functions for constructing test data

func newTestSetupPhase() *SetupPhase {
	return &SetupPhase{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func newTestFuncRule(path, target string) *rule.InstFuncRule {
	return &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Target: target,
		},
		Path: path,
	}
}

func newTestRuleSet(modulePath string, funcRules ...*rule.InstFuncRule) *rule.InstRuleSet {
	rs := rule.NewInstRuleSet(modulePath)
	fakeFilePath := filepath.Join(os.TempDir(), "file.go")
	for _, fr := range funcRules {
		rs.AddFuncRule(fakeFilePath, fr)
	}
	return rs
}

func writeGoSource(t *testing.T, name, content string) string {
	path := filepath.Join(t.TempDir(), name)
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
	return path
}

func TestRunMatch_FileRuleOnlySetsPackageName(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "mypkg.go")
	err := os.WriteFile(srcFile, []byte("package mypkg\n"), 0o644)
	require.NoError(t, err)

	const importPath = "example.com/mypkg"

	yamlContent := []byte(`
file: hook.go
target: example.com/mypkg
`)
	fileRule, err := rule.NewInstFileRule(yamlContent, "test-file-rule")
	require.NoError(t, err)

	dep := &Dependency{
		ImportPath: importPath,
		Sources:    []string{srcFile},
		CgoFiles:   make(map[string]string),
	}

	rulesByTarget := map[string][]rule.InstRule{
		importPath: {fileRule},
	}

	sp := newTestSetupPhase()
	set, err := sp.runMatch(context.Background(), dep, rulesByTarget)
	require.NoError(t, err)
	require.NotNil(t, set)

	assert.Equal(t, "mypkg", set.PackageName)
	assert.False(t, set.IsEmpty(), "rule set must contain the file rule")
}

func TestRunMatch_EmptyRules(t *testing.T) {
	dep := &Dependency{
		ImportPath: "example.com/noop",
		Sources:    []string{},
		CgoFiles:   make(map[string]string),
	}

	sp := newTestSetupPhase()
	set, err := sp.runMatch(context.Background(), dep, map[string][]rule.InstRule{})
	require.NoError(t, err)
	require.NotNil(t, set)
	assert.True(t, set.IsEmpty())
}

func TestRunMatch_FileRuleInvalidSource(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "bad.go")
	err := os.WriteFile(srcFile, []byte("not valid go source {{{"), 0o644)
	require.NoError(t, err)

	const importPath = "example.com/mypkg"

	yamlContent := []byte(`
file: hook.go
target: example.com/mypkg
`)
	fileRule, err := rule.NewInstFileRule(yamlContent, "test-file-rule")
	require.NoError(t, err)

	dep := &Dependency{
		ImportPath: importPath,
		Sources:    []string{srcFile},
		CgoFiles:   make(map[string]string),
	}

	rulesByTarget := map[string][]rule.InstRule{
		importPath: {fileRule},
	}

	sp := newTestSetupPhase()
	_, err = sp.runMatch(context.Background(), dep, rulesByTarget)
	assert.Error(t, err, "should fail when source file cannot be parsed")
}

func TestRunMatch_FileRuleNoSources(t *testing.T) {
	const importPath = "example.com/mypkg"

	yamlContent := []byte(`
file: hook.go
target: example.com/mypkg
`)
	fileRule, err := rule.NewInstFileRule(yamlContent, "test-file-rule")
	require.NoError(t, err)

	dep := &Dependency{
		ImportPath: importPath,
		Sources:    []string{},
		CgoFiles:   make(map[string]string),
	}

	rulesByTarget := map[string][]rule.InstRule{
		importPath: {fileRule},
	}

	sp := newTestSetupPhase()
	set, err := sp.runMatch(context.Background(), dep, rulesByTarget)
	require.NoError(t, err)
	require.NotNil(t, set)

	assert.Empty(t, set.PackageName)
	assert.False(t, set.IsEmpty())
}
