// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"go/token"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/dave/dst"
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
			name: "function_call rule creation",
			yamlContent: `
function_call: "net/http.Get"
target: github.com/example/lib
template: "{{ . }}"
`,
			ruleName:     "test-call-rule",
			expectError:  false,
			expectedType: "*rule.InstCallRule",
		},
		{
			name: "value_declaration rule creation",
			yamlContent: `
value_declaration: "bool"
target: github.com/example/lib
assign_value: "true"
`,
			ruleName:     "test-value-decl-rule",
			expectError:  false,
			expectedType: "*rule.InstValueDeclRule",
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

// newMatchOneRuleFixture returns shared setup for matchOneRule tests.
func newMatchOneRuleFixture() (*SetupPhase, string, *rule.InstRuleSet, *Dependency) {
	sp := &SetupPhase{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	fakeFile := filepath.Join(os.TempDir(), "fake_file.go")
	set := rule.NewInstRuleSet("example.com/pkg")
	dep := &Dependency{ImportPath: "example.com/pkg"}
	return sp, fakeFile, set, dep
}

func TestMatchOneRule_ValueDeclRule(t *testing.T) {
	sp, fakeFile, set, dep := newMatchOneRuleFixture()
	tree := &dst.File{Name: &dst.Ident{Name: "main"}}

	r := &rule.InstValueDeclRule{
		InstBaseRule:     rule.InstBaseRule{Name: "replace-bool", Target: "example.com/pkg"},
		ValueDeclaration: "bool",
		AssignValue:      "true",
		TypeIdent:        "bool",
	}
	sp.matchOneRule(tree, fakeFile, r, set, dep)

	assert.Len(t, set.ValueDeclRules[fakeFile], 1)
	assert.Equal(t, r, set.ValueDeclRules[fakeFile][0])
}

func TestMatchOneRule_CallRule(t *testing.T) {
	// Call rules are added unconditionally to all source files.
	sp, fakeFile, set, dep := newMatchOneRuleFixture()
	tree := &dst.File{Name: &dst.Ident{Name: "main"}}

	r := &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{Name: "my-call", Target: "example.com/pkg"},
		ImportPath:   "net/http",
		FuncName:     "Get",
	}
	sp.matchOneRule(tree, fakeFile, r, set, dep)

	assert.Len(t, set.CallRules[fakeFile], 1)
}

func TestMatchOneRule_FuncRule_Match(t *testing.T) {
	sp, fakeFile, set, dep := newMatchOneRuleFixture()
	tree := &dst.File{
		Name: &dst.Ident{Name: "main"},
		Decls: []dst.Decl{
			&dst.FuncDecl{Name: &dst.Ident{Name: "MyFunc"}, Type: &dst.FuncType{}},
		},
	}
	r := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "my-func", Target: "example.com/pkg"},
		Func:         "MyFunc",
	}
	sp.matchOneRule(tree, fakeFile, r, set, dep)

	assert.Len(t, set.FuncRules[fakeFile], 1)
}

func TestMatchOneRule_FuncRule_NoMatch(t *testing.T) {
	sp, fakeFile, set, dep := newMatchOneRuleFixture()
	tree := &dst.File{Name: &dst.Ident{Name: "main"}}

	r := &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: "my-func", Target: "example.com/pkg"},
		Func:         "MissingFunc",
	}
	sp.matchOneRule(tree, fakeFile, r, set, dep)

	assert.Empty(t, set.FuncRules)
}

func TestMatchOneRule_DeclRule_Match(t *testing.T) {
	sp, fakeFile, set, dep := newMatchOneRuleFixture()
	tree := &dst.File{
		Name: &dst.Ident{Name: "main"},
		Decls: []dst.Decl{
			&dst.GenDecl{
				Tok: token.VAR,
				Specs: []dst.Spec{
					&dst.ValueSpec{Names: []*dst.Ident{{Name: "GlobalVar"}}},
				},
			},
		},
	}
	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "my-decl", Target: "example.com/pkg"},
		Identifier:   "GlobalVar",
		Kind:         "var",
	}
	sp.matchOneRule(tree, fakeFile, r, set, dep)

	assert.Len(t, set.DeclRules[fakeFile], 1)
}

func TestMatchOneRule_DeclRule_NoMatch(t *testing.T) {
	sp, fakeFile, set, dep := newMatchOneRuleFixture()
	tree := &dst.File{Name: &dst.Ident{Name: "main"}}

	r := &rule.InstDeclRule{
		InstBaseRule: rule.InstBaseRule{Name: "my-decl", Target: "example.com/pkg"},
		Identifier:   "MissingVar",
		Kind:         "var",
	}
	sp.matchOneRule(tree, fakeFile, r, set, dep)

	assert.Empty(t, set.DeclRules)
}

func TestMatchOneRule_FileRule_Skipped(t *testing.T) {
	// File rules are pre-processed by runMatch; matchOneRule skips them.
	sp, fakeFile, set, dep := newMatchOneRuleFixture()
	tree := &dst.File{Name: &dst.Ident{Name: "main"}}

	r := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: "my-file", Target: "example.com/pkg"},
	}
	sp.matchOneRule(tree, fakeFile, r, set, dep)

	assert.Empty(t, set.FileRules)
}

func newTestRuleSet(modulePath string, funcRules ...*rule.InstFuncRule) *rule.InstRuleSet {
	rs := rule.NewInstRuleSet(modulePath)
	fakeFilePath := filepath.Join(os.TempDir(), "file.go")
	for _, fr := range funcRules {
		rs.AddFuncRule(fakeFilePath, fr)
	}
	return rs
}
