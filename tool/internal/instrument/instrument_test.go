// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package instrument

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/golden"
)

const (
	matchedJSONFile = "matched.json"
	testdataPath    = "testdata"
	mainGoFile      = "main.go"
	globalsGoFile   = "otel.globals.go"
	mainPackageName = "main"
	mainModulePath  = "main"
	testBuildID     = "foo/bar"
	testOutputFile  = "_pkg_.a"
)

func TestInstrumentWithDifferentRuleTypes_Integration(t *testing.T) {
	tests := []struct {
		name       string
		setupRules func(string) *rule.InstRuleSet
		verify     func(*testing.T, *testContext)
	}{
		{
			name: "func rule only",
			setupRules: func(sourceFile string) *rule.InstRuleSet {
				return &rule.InstRuleSet{
					PackageName: mainPackageName,
					ModulePath:  mainModulePath,
					FuncRules: map[string][]*rule.InstFuncRule{
						sourceFile: {newFuncRule("hook_func", "Func1", "H1Before", "H1After")},
					},
				}
			},
			verify: func(t *testing.T, tc *testContext) {
				mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
				globalsPath := filepath.Join(tc.tempDir, globalsGoFile)
				assertGoldenFile(t, mainGoPath, "func_rule_only.main.go")
				assertGoldenFile(t, globalsPath, "shared.common.otel.globals.go")
			},
		},
		{
			name: "struct rule only",
			setupRules: func(sourceFile string) *rule.InstRuleSet {
				return &rule.InstRuleSet{
					PackageName: mainPackageName,
					ModulePath:  mainModulePath,
					StructRules: map[string][]*rule.InstStructRule{
						sourceFile: {newStructRule("add_new_field", "T", "NewField", "string")},
					},
				}
			},
			verify: func(t *testing.T, tc *testContext) {
				mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
				assertGoldenFile(t, mainGoPath, "struct_rule_only.main.go")
				assertGlobalsFileNotExists(t, tc.tempDir)
			},
		},
		{
			name: "raw rule only",
			setupRules: func(sourceFile string) *rule.InstRuleSet {
				return &rule.InstRuleSet{
					PackageName: mainPackageName,
					ModulePath:  mainModulePath,
					RawRules: map[string][]*rule.InstRawRule{
						sourceFile: {newRawRule("add_raw_code", "Func1", "_ = 123")},
					},
				}
			},
			verify: func(t *testing.T, tc *testContext) {
				mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
				globalsPath := filepath.Join(tc.tempDir, globalsGoFile)
				assertGoldenFile(t, mainGoPath, "raw_rule_only.main.go")
				assertGoldenFile(t, globalsPath, "raw_rule_only.otel.globals.go")
			},
		},
		{
			name: "file rule only",
			setupRules: func(sourceFile string) *rule.InstRuleSet {
				return &rule.InstRuleSet{
					PackageName: mainPackageName,
					ModulePath:  mainModulePath,
					FileRules:   []*rule.InstFileRule{newFileRule("add_new_file", "newfile.go")},
				}
			},
			verify: func(t *testing.T, tc *testContext) {
				newFile := filepath.Join(tc.tempDir, "otel.newfile.go")
				assertGoldenFile(t, newFile, "file_rule_only.otel.newfile.go")
				assertGlobalsFileNotExists(t, tc.tempDir)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := setupTest(t)
			sourceFile := setupTestFiles(t, tc.tempDir)
			ruleSet := tt.setupRules(sourceFile)
			err := tc.runInstrumentationWithRuleSet(t, ruleSet)
			require.NoError(t, err, "instrumentation should succeed")
			tt.verify(t, tc)
		})
	}
}

func TestInstrumentWithMethodReceiver_Integration(t *testing.T) {
	tc := setupTest(t)
	sourceFile := setupTestFiles(t, tc.tempDir)

	ruleSet := &rule.InstRuleSet{
		PackageName: mainPackageName,
		ModulePath:  mainModulePath,
		FuncRules: map[string][]*rule.InstFuncRule{
			sourceFile: {
				{
					InstBaseRule: newBaseRule("hook_method"),
					Path:         filepath.Join(".", testdataPath),
					Func:         "Func1",
					Recv:         "*T",
					Before:       "H3Before",
					After:        "H3After",
				},
			},
		},
	}

	err := tc.runInstrumentationWithRuleSet(t, ruleSet)
	require.NoError(t, err, "instrumentation should succeed")

	mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
	globalsPath := filepath.Join(tc.tempDir, globalsGoFile)
	assertGoldenFile(t, mainGoPath, "method_receiver.main.go")
	assertGoldenFile(t, globalsPath, "shared.common.otel.globals.go")
}

func TestInstrumentWithInvalidReceiver_Integration(t *testing.T) {
	tc := setupTest(t)
	sourceFile := setupTestFiles(t, tc.tempDir)

	ruleSet := &rule.InstRuleSet{
		PackageName: mainPackageName,
		ModulePath:  mainModulePath,
		FuncRules: map[string][]*rule.InstFuncRule{
			sourceFile: {
				{
					InstBaseRule: newBaseRule("hook_invalid_receiver"),
					Path:         filepath.Join(".", testdataPath),
					Func:         "Func1",
					Recv:         "*NonExistent", // Invalid receiver - doesn't match any method
					Before:       "H1Before",
					After:        "H1After",
				},
			},
		},
	}

	err := tc.runInstrumentationWithRuleSet(t, ruleSet)
	require.Error(t, err, "instrumentation should fail with invalid receiver")
	require.Contains(t, err.Error(), "can not find function", "error should indicate function not found")
}

func TestInstrumentWithBeforeOnly_Integration(t *testing.T) {
	tc := setupTest(t)
	sourceFile := setupTestFiles(t, tc.tempDir)

	ruleSet := &rule.InstRuleSet{
		PackageName: mainPackageName,
		ModulePath:  mainModulePath,
		FuncRules: map[string][]*rule.InstFuncRule{
			sourceFile: {
				{
					InstBaseRule: newBaseRule("hook_before_only"),
					Path:         filepath.Join(".", testdataPath),
					Func:         "Func1",
					Before:       "H1Before",
					After:        "",
				},
			},
		},
	}

	err := tc.runInstrumentationWithRuleSet(t, ruleSet)
	require.NoError(t, err, "instrumentation should succeed")

	mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
	globalsPath := filepath.Join(tc.tempDir, globalsGoFile)
	assertGoldenFile(t, mainGoPath, "before_only.main.go")
	assertGoldenFile(t, globalsPath, "shared.common.otel.globals.go")
}

func TestInstrumentWithAfterOnly_Integration(t *testing.T) {
	tc := setupTest(t)
	sourceFile := setupTestFiles(t, tc.tempDir)

	ruleSet := &rule.InstRuleSet{
		PackageName: mainPackageName,
		ModulePath:  mainModulePath,
		FuncRules: map[string][]*rule.InstFuncRule{
			sourceFile: {
				{
					InstBaseRule: newBaseRule("hook_after_only"),
					Path:         filepath.Join(".", testdataPath),
					Func:         "Func1",
					Before:       "",
					After:        "H1After",
				},
			},
		},
	}

	err := tc.runInstrumentationWithRuleSet(t, ruleSet)
	require.NoError(t, err, "instrumentation should succeed")

	mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
	globalsPath := filepath.Join(tc.tempDir, globalsGoFile)
	assertGoldenFile(t, mainGoPath, "after_only.main.go")
	assertGoldenFile(t, globalsPath, "shared.common.otel.globals.go")
}

func TestInstrumentWithMultipleFuncRules_Integration(t *testing.T) {
	tc := setupTest(t)
	sourceFile := setupTestFiles(t, tc.tempDir)

	ruleSet := &rule.InstRuleSet{
		PackageName: mainPackageName,
		ModulePath:  mainModulePath,
		FuncRules: map[string][]*rule.InstFuncRule{
			sourceFile: {
				newFuncRule("hook_func_1", "Func1", "H1Before", "H1After"),
				newFuncRule("hook_func_2", "Func1", "H2Before", "H2After"),
			},
		},
	}

	err := tc.runInstrumentationWithRuleSet(t, ruleSet)
	require.NoError(t, err, "instrumentation should succeed")

	mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
	globalsPath := filepath.Join(tc.tempDir, globalsGoFile)
	assertGoldenFile(t, mainGoPath, "multiple_func_rules.main.go")
	assertGoldenFile(t, globalsPath, "shared.common.otel.globals.go")
}

func TestInstrumentWithFuncAndRawRules_Integration(t *testing.T) {
	tc := setupTest(t)
	sourceFile := setupTestFiles(t, tc.tempDir)

	ruleSet := &rule.InstRuleSet{
		PackageName: mainPackageName,
		ModulePath:  mainModulePath,
		FuncRules: map[string][]*rule.InstFuncRule{
			sourceFile: {newFuncRule("hook_func", "Func1", "H1Before", "H1After")},
		},
		RawRules: map[string][]*rule.InstRawRule{
			sourceFile: {newRawRule("add_raw_code", "Func1", "_ = 456")},
		},
	}

	err := tc.runInstrumentationWithRuleSet(t, ruleSet)
	require.NoError(t, err, "instrumentation should succeed")

	mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
	globalsPath := filepath.Join(tc.tempDir, globalsGoFile)
	assertGoldenFile(t, mainGoPath, "func_and_raw_rules.main.go")
	assertGoldenFile(t, globalsPath, "shared.common.otel.globals.go")
}

func TestInstrumentWithMultipleStructFields_Integration(t *testing.T) {
	tc := setupTest(t)
	sourceFile := setupTestFiles(t, tc.tempDir)

	ruleSet := &rule.InstRuleSet{
		PackageName: mainPackageName,
		ModulePath:  mainModulePath,
		StructRules: map[string][]*rule.InstStructRule{
			sourceFile: {
				{
					InstBaseRule: newBaseRule("add_multiple_fields"),
					Struct:       "T",
					NewField: []*rule.InstStructField{
						{Name: "Field1", Type: "string"},
						{Name: "Field2", Type: "int"},
						{Name: "Field3", Type: "bool"},
					},
				},
			},
		},
	}

	err := tc.runInstrumentationWithRuleSet(t, ruleSet)
	require.NoError(t, err, "instrumentation should succeed")

	mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
	assertGoldenFile(t, mainGoPath, "multiple_struct_fields.main.go")
	assertGlobalsFileNotExists(t, tc.tempDir)
}

func TestInstrumentWithCombinedRules_Integration(t *testing.T) {
	tc := setupTest(t)
	sourceFile := setupTestFiles(t, tc.tempDir)

	ruleSet := &rule.InstRuleSet{
		PackageName: mainPackageName,
		ModulePath:  mainModulePath,
		FuncRules: map[string][]*rule.InstFuncRule{
			sourceFile: {newFuncRule("hook_func", "Func1", "H1Before", "H1After")},
		},
		StructRules: map[string][]*rule.InstStructRule{
			sourceFile: {newStructRule("add_field", "T", "NewField", "string")},
		},
		RawRules: map[string][]*rule.InstRawRule{
			sourceFile: {newRawRule("add_raw", "Func1", "_ = 789")},
		},
		FileRules: []*rule.InstFileRule{newFileRule("add_file", "newfile.go")},
	}

	err := tc.runInstrumentationWithRuleSet(t, ruleSet)
	require.NoError(t, err, "instrumentation should succeed")

	mainGoPath := filepath.Join(tc.tempDir, mainGoFile)
	globalsPath := filepath.Join(tc.tempDir, globalsGoFile)
	newFile := filepath.Join(tc.tempDir, "otel.newfile.go")
	assertGoldenFile(t, mainGoPath, "combined_rules.main.go")
	assertGoldenFile(t, globalsPath, "shared.common.otel.globals.go")
	assertGoldenFile(t, newFile, "combined_rules.otel.newfile.go")
}

// ============================================================================
// Test Helper Functions
// ============================================================================

// testContext holds common test setup
type testContext struct {
	ctx     context.Context
	tempDir string
	logger  *slog.Logger
}

// setupTest creates common test infrastructure
func setupTest(t *testing.T) *testContext {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	ctx := util.ContextWithLogger(t.Context(), logger)
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelWorkDir, tempDir)

	return &testContext{
		ctx:     ctx,
		tempDir: tempDir,
		logger:  logger,
	}
}

// runInstrumentationWithRuleSet executes instrumentation with the given rule set.
// The rule set should already have the correct source file paths set.
func (tc *testContext) runInstrumentationWithRuleSet(t *testing.T, ruleSet *rule.InstRuleSet) error {
	setupMatchedJSON(t, tc.tempDir, ruleSet)
	args := createCompileArgs(t, tc.tempDir)
	return Toolexec(tc.ctx, args)
}

// assertGoldenFile compares the actual file content against a golden file.
func assertGoldenFile(t *testing.T, actualPath, goldenName string) {
	require.FileExists(t, actualPath, "file should exist: %s", actualPath)

	content, err := os.ReadFile(actualPath)
	require.NoError(t, err, "failed to read file: %s", actualPath)

	golden.Assert(t, string(content), filepath.Join("golden", goldenName+".golden"))
}

// assertGlobalsFileNotExists verifies that otel.globals.go does not exist.
func assertGlobalsFileNotExists(t *testing.T, tempDir string) {
	globalsFile := filepath.Join(tempDir, globalsGoFile)
	require.NoFileExists(t, globalsFile, "%s should not exist", globalsGoFile)
}

// createCompileArgs creates compile command arguments for testing.
func createCompileArgs(t *testing.T, tempDir string) []string {
	cmd := exec.Command("go", "env", "GOTOOLDIR")
	output, err := cmd.Output()
	require.NoError(t, err, "failed to get GOTOOLDIR")
	goToolDir := strings.TrimSpace(string(output))
	require.NotEmpty(t, goToolDir, "GOTOOLDIR should not be empty")

	goToolCompilePath := filepath.Join(goToolDir, "compile")
	require.FileExists(t, goToolCompilePath, "compile should exist: %s", goToolCompilePath)

	return []string{
		goToolCompilePath,
		"-o", filepath.Join(tempDir, testOutputFile),
		"-p", mainPackageName,
		"-complete",
		"-buildid", testBuildID,
		"-pack",
		filepath.Join(tempDir, mainGoFile),
	}
}

// setupTestFiles creates the test environment with source files.
func setupTestFiles(t *testing.T, tempDir string) string {
	sourceFile := filepath.Join(tempDir, mainGoFile)
	err := os.MkdirAll(filepath.Dir(sourceFile), 0o755)
	require.NoError(t, err, "failed to create directory")
	err = util.CopyFile(filepath.Join(testdataPath, "source.go"), sourceFile)
	require.NoError(t, err, "failed to copy source file")
	return sourceFile
}

// setupMatchedJSON creates the matched.json file with the given rule set.
func setupMatchedJSON(t *testing.T, tempDir string, ruleSet *rule.InstRuleSet) {
	matchedJSON, err := json.Marshal([]*rule.InstRuleSet{ruleSet})
	require.NoError(t, err, "failed to marshal rule set")
	matchedFile := filepath.Join(tempDir, util.BuildTempDir, matchedJSONFile)
	err = os.MkdirAll(filepath.Dir(matchedFile), 0o755)
	require.NoError(t, err, "failed to create build temp directory")
	err = util.WriteFile(matchedFile, string(matchedJSON))
	require.NoError(t, err, "failed to write matched.json")
}

// ============================================================================
// Rule Builder Helpers
// ============================================================================

// newFuncRule creates a function instrumentation rule.
//
//nolint:unparam // funcName parameter kept for flexibility in future tests
func newFuncRule(name, funcName, before, after string) *rule.InstFuncRule {
	return &rule.InstFuncRule{
		InstBaseRule: newBaseRule(name),
		Path:         filepath.Join(".", testdataPath),
		Func:         funcName,
		Before:       before,
		After:        after,
	}
}

// newStructRule creates a struct instrumentation rule.
func newStructRule(name, structName, fieldName, fieldType string) *rule.InstStructRule {
	return &rule.InstStructRule{
		InstBaseRule: newBaseRule(name),
		Struct:       structName,
		NewField: []*rule.InstStructField{
			{
				Name: fieldName,
				Type: fieldType,
			},
		},
	}
}

// newRawRule creates a raw code instrumentation rule.
func newRawRule(name, funcName, raw string) *rule.InstRawRule {
	return &rule.InstRawRule{
		InstBaseRule: newBaseRule(name),
		Func:         funcName,
		Raw:          raw,
	}
}

// newFileRule creates a file instrumentation rule.
func newFileRule(name, fileName string) *rule.InstFileRule {
	return &rule.InstFileRule{
		InstBaseRule: newBaseRule(name),
		File:         fileName,
		Path:         filepath.Join(".", testdataPath),
	}
}

// newBaseRule creates a base rule with common fields.
func newBaseRule(name string) rule.InstBaseRule {
	return rule.InstBaseRule{
		Name:   name,
		Target: mainPackageName,
	}
}
