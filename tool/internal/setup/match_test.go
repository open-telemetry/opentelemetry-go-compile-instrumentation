// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
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
