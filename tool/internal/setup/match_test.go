// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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
		name   string
		input  map[string]any
		expect map[string]any
	}{
		{
			name: "flat format passthrough",
			input: map[string]any{
				"target": "net/http",
				"func":   "ServeHTTP",
				"before": "BeforeHook",
				"path":   "github.com/example/pkg",
			},
			expect: map[string]any{
				"target": "net/http",
				"func":   "ServeHTTP",
				"before": "BeforeHook",
				"path":   "github.com/example/pkg",
			},
		},
		{
			name: "inject_hooks: func+recv from where, before/after/path from do",
			input: map[string]any{
				"where": map[string]any{
					"target": "net/http",
					"func":   "ServeHTTP",
					"recv":   "serverHandler",
				},
				"do": map[string]any{
					"inject_hooks": map[string]any{
						"before": "BeforeServeHTTP",
						"after":  "AfterServeHTTP",
						"path":   "github.com/example/pkg",
					},
				},
			},
			expect: map[string]any{
				"target": "net/http",
				"func":   "ServeHTTP",
				"recv":   "serverHandler",
				"before": "BeforeServeHTTP",
				"after":  "AfterServeHTTP",
				"path":   "github.com/example/pkg",
			},
		},
		{
			name: "inject_code: func from where, raw from do",
			input: map[string]any{
				"where": map[string]any{
					"target": "runtime",
					"func":   "newproc1",
				},
				"do": map[string]any{
					"inject_code": map[string]any{
						"raw": "defer func(){}()",
					},
				},
			},
			expect: map[string]any{
				"target": "runtime",
				"func":   "newproc1",
				"raw":    "defer func(){}()",
			},
		},
		{
			name: "add_struct_fields: struct from where, new_field from do",
			input: map[string]any{
				"where": map[string]any{
					"target": "runtime",
					"struct": "g",
				},
				"do": map[string]any{
					"add_struct_fields": map[string]any{
						"new_field": []any{
							map[string]any{"name": "otel_ctx", "type": "interface{}"},
						},
					},
				},
			},
			expect: map[string]any{
				"target": "runtime",
				"struct": "g",
				"new_field": []any{
					map[string]any{"name": "otel_ctx", "type": "interface{}"},
				},
			},
		},
		{
			name: "add_file: target only in where, file+path from do",
			input: map[string]any{
				"where": map[string]any{
					"target": "runtime",
				},
				"do": map[string]any{
					"add_file": map[string]any{
						"file": "runtime_gls.go",
						"path": "github.com/example/pkg",
					},
				},
			},
			expect: map[string]any{
				"target": "runtime",
				"file":   "runtime_gls.go",
				"path":   "github.com/example/pkg",
			},
		},
		{
			name: "wrap_call: function_call from where, template from do",
			input: map[string]any{
				"where": map[string]any{
					"target":        "main",
					"function_call": "unsafe.Sizeof",
				},
				"do": map[string]any{
					"wrap_call": map[string]any{
						"template": "Wrapper({{ . }})",
					},
				},
			},
			expect: map[string]any{
				"target":        "main",
				"function_call": "unsafe.Sizeof",
				"template":      "Wrapper({{ . }})",
			},
		},
		{
			name: "expand_directive: directive from where, template from do",
			input: map[string]any{
				"where": map[string]any{
					"target":    "main",
					"directive": "otelc:span",
				},
				"do": map[string]any{
					"expand_directive": map[string]any{
						"template": `defer otelc.End()`,
					},
				},
			},
			expect: map[string]any{
				"target":    "main",
				"directive": "otelc:span",
				"template":  `defer otelc.End()`,
			},
		},
		{
			name: "assign_value: kind+identifier from where, value from do",
			input: map[string]any{
				"where": map[string]any{
					"target":     "main",
					"kind":       "var",
					"identifier": "GlobalVar",
				},
				"do": map[string]any{
					"assign_value": map[string]any{
						"value": `"replaced"`,
					},
				},
			},
			expect: map[string]any{
				"target":     "main",
				"kind":       "var",
				"identifier": "GlobalVar",
				"value":      `"replaced"`,
			},
		},
		{
			name: "imports stays at top level",
			input: map[string]any{
				"where": map[string]any{
					"target": "main",
					"func":   "Fn",
				},
				"do": map[string]any{
					"inject_code": map[string]any{
						"raw": `fmt.Println("x")`,
					},
				},
				"imports": map[string]any{"fmt": "fmt"},
			},
			expect: map[string]any{
				"target":  "main",
				"func":    "Fn",
				"raw":     `fmt.Println("x")`,
				"imports": map[string]any{"fmt": "fmt"},
			},
		},
		{
			name: "version from where is preserved",
			input: map[string]any{
				"where": map[string]any{
					"target":  "golang.org/x/time/rate",
					"version": "v0.14.0,v0.15.0",
					"func":    "Every",
				},
				"do": map[string]any{
					"inject_code": map[string]any{
						"raw": `fmt.Println("x")`,
					},
				},
			},
			expect: map[string]any{
				"target":  "golang.org/x/time/rate",
				"version": "v0.14.0,v0.15.0",
				"func":    "Every",
				"raw":     `fmt.Println("x")`,
			},
		},
		{
			name: "only where block (no do)",
			input: map[string]any{
				"where": map[string]any{
					"target": "main",
					"func":   "Fn",
				},
			},
			expect: map[string]any{
				"target": "main",
				"func":   "Fn",
			},
		},
		{
			name: "only do block (no where)",
			input: map[string]any{
				"do": map[string]any{
					"inject_code": map[string]any{
						"raw": "_ = 0",
					},
				},
			},
			expect: map[string]any{
				"raw": "_ = 0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRule(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("normalizeRule() len = %d, want %d; got %v", len(got), len(tt.expect), got)
				return
			}
			for k, wantVal := range tt.expect {
				gotVal, exists := got[k]
				if !exists {
					t.Errorf("normalizeRule() missing key %q", k)
					continue
				}
				// Use yaml round-trip for deep equality of nested maps/slices.
				wantYAML, _ := yaml.Marshal(wantVal)
				gotYAML, _ := yaml.Marshal(gotVal)
				if string(wantYAML) != string(gotYAML) {
					t.Errorf("normalizeRule()[%q] = %v, want %v", k, gotVal, wantVal)
				}
			}
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
