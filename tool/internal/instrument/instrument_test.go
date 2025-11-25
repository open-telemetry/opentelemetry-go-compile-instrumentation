// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

// Package instrument tests verify that the instrumentation process generates
// the expected output by comparing against golden files.
//
// To update golden files after intentional changes:
//
//		go test -update ./tool/internal/instrument/...
//	 or
//		make test-unit/update-golden

package instrument

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"gotest.tools/v3/golden"
)

const (
	testdataDir        = "testdata"
	goldenDir          = "golden"
	sourceFileName     = "source.go"
	rulesFileName      = "rules.yml"
	mainGoFileName     = "main.go"
	mainPackage        = "main"
	buildID            = "foo/bar"
	compiledOutput     = "_pkg_.a"
	goldenExt          = ".golden"
	invalidReceiver    = "invalid-receiver"
	invalidReceiverMsg = "can not find function"
)

func TestInstrumentation_Integration(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(testdataDir, goldenDir))
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			runTest(t, entry.Name())
		})
	}
}

func runTest(t *testing.T, testName string) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelWorkDir, tempDir)
	ctx := util.ContextWithLogger(
		t.Context(),
		slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})),
	)

	sourceFile := filepath.Join(tempDir, mainGoFileName)
	util.CopyFile(filepath.Join(testdataDir, sourceFileName), sourceFile)

	ruleSet := loadRulesYAML(t, testName, sourceFile)
	writeMatchedJSON(ruleSet)

	args := compileArgs(tempDir, sourceFile)
	err := Toolexec(ctx, args)

	if testName == invalidReceiver {
		require.Error(t, err)
		require.Contains(t, err.Error(), invalidReceiverMsg)
		return
	}

	require.NoError(t, err)
	verifyGoldenFiles(t, tempDir, testName)
}

func loadRulesYAML(t *testing.T, testName, sourceFile string) *rule.InstRuleSet {
	data, err := os.ReadFile(filepath.Join(testdataDir, goldenDir, testName, rulesFileName))
	require.NoError(t, err)

	var rawRules map[string]map[string]any
	yaml.Unmarshal(data, &rawRules)

	ruleSet := &rule.InstRuleSet{
		PackageName: mainPackage,
		ModulePath:  mainPackage,
		FuncRules:   make(map[string][]*rule.InstFuncRule),
		StructRules: make(map[string][]*rule.InstStructRule),
		RawRules:    make(map[string][]*rule.InstRawRule),
		FileRules:   make([]*rule.InstFileRule, 0),
	}

	// Sort rule names to ensure deterministic order in tests
	ruleNames := make([]string, 0, len(rawRules))
	for name := range rawRules {
		ruleNames = append(ruleNames, name)
	}
	sort.Strings(ruleNames)

	for _, name := range ruleNames {
		props := rawRules[name]
		props["name"] = name
		ruleData, _ := yaml.Marshal(props)

		switch {
		case props["struct"] != nil:
			r, _ := rule.NewInstStructRule(ruleData, name)
			ruleSet.StructRules[sourceFile] = append(ruleSet.StructRules[sourceFile], r)
		case props["file"] != nil:
			r, _ := rule.NewInstFileRule(ruleData, name)
			ruleSet.FileRules = append(ruleSet.FileRules, r)
		case props["raw"] != nil:
			r, _ := rule.NewInstRawRule(ruleData, name)
			ruleSet.RawRules[sourceFile] = append(ruleSet.RawRules[sourceFile], r)
		case props["func"] != nil:
			r, _ := rule.NewInstFuncRule(ruleData, name)
			ruleSet.FuncRules[sourceFile] = append(ruleSet.FuncRules[sourceFile], r)
		}
	}

	return ruleSet
}

func writeMatchedJSON(ruleSet *rule.InstRuleSet) {
	matchedJSON, _ := json.Marshal([]*rule.InstRuleSet{ruleSet})
	matchedFile := util.GetMatchedRuleFile()
	os.MkdirAll(filepath.Dir(matchedFile), 0o755)
	util.WriteFile(matchedFile, string(matchedJSON))
}

func compileArgs(tempDir, sourceFile string) []string {
	output, _ := exec.Command("go", "env", "GOTOOLDIR").Output()
	return []string{
		filepath.Join(strings.TrimSpace(string(output)), "compile"),
		"-o", filepath.Join(tempDir, compiledOutput),
		"-p", mainPackage,
		"-complete",
		"-buildid", buildID,
		"-pack",
		sourceFile,
	}
}

func verifyGoldenFiles(t *testing.T, tempDir, testName string) {
	entries, _ := os.ReadDir(filepath.Join(testdataDir, goldenDir, testName))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), goldenExt) {
			continue
		}
		actualFile := actualFileFromGolden(t, entry.Name())
		actual, _ := os.ReadFile(filepath.Join(tempDir, actualFile))
		golden.Assert(t, string(actual), filepath.Join(goldenDir, testName, entry.Name()))
	}
}

func actualFileFromGolden(t *testing.T, goldenName string) string {
	// Golden files are named: <prefix>.<actual_file_name>.golden
	// Example: func_rule_only.main.go.golden -> main.go
	nameWithoutExt := strings.TrimSuffix(goldenName, goldenExt)
	parts := strings.SplitN(nameWithoutExt, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("invalid golden file name format: %s (expected: <prefix>.<filename>.golden)", goldenName)
	}
	return parts[1]
}

func TestGroupRules(t *testing.T) {
	tests := []struct {
		name          string
		ruleSet       *rule.InstRuleSet
		expectedFiles []string
		validate      func(*testing.T, map[string][]rule.InstRule)
	}{
		{
			name: "empty ruleset",
			ruleSet: &rule.InstRuleSet{
				FuncRules:   make(map[string][]*rule.InstFuncRule),
				StructRules: make(map[string][]*rule.InstStructRule),
				RawRules:    make(map[string][]*rule.InstRawRule),
			},
			expectedFiles: []string{},
		},
		{
			name: "func rules only",
			ruleSet: &rule.InstRuleSet{
				FuncRules: map[string][]*rule.InstFuncRule{
					"file1.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "rule1"}},
						{InstBaseRule: rule.InstBaseRule{Name: "rule2"}},
					},
				},
				StructRules: make(map[string][]*rule.InstStructRule),
				RawRules:    make(map[string][]*rule.InstRawRule),
			},
			expectedFiles: []string{"file1.go"},
			validate: func(t *testing.T, grouped map[string][]rule.InstRule) {
				assert.Len(t, grouped["file1.go"], 2)
			},
		},
		{
			name: "struct rules only",
			ruleSet: &rule.InstRuleSet{
				FuncRules: make(map[string][]*rule.InstFuncRule),
				StructRules: map[string][]*rule.InstStructRule{
					"file2.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "struct1"}},
					},
				},
				RawRules: make(map[string][]*rule.InstRawRule),
			},
			expectedFiles: []string{"file2.go"},
			validate: func(t *testing.T, grouped map[string][]rule.InstRule) {
				assert.Len(t, grouped["file2.go"], 1)
			},
		},
		{
			name: "raw rules only",
			ruleSet: &rule.InstRuleSet{
				FuncRules:   make(map[string][]*rule.InstFuncRule),
				StructRules: make(map[string][]*rule.InstStructRule),
				RawRules: map[string][]*rule.InstRawRule{
					"file3.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "raw1"}},
					},
				},
			},
			expectedFiles: []string{"file3.go"},
			validate: func(t *testing.T, grouped map[string][]rule.InstRule) {
				assert.Len(t, grouped["file3.go"], 1)
			},
		},
		{
			name: "mixed rules across multiple files",
			ruleSet: &rule.InstRuleSet{
				FuncRules: map[string][]*rule.InstFuncRule{
					"file1.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "func1"}},
					},
					"file2.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "func2"}},
					},
				},
				StructRules: map[string][]*rule.InstStructRule{
					"file1.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "struct1"}},
					},
				},
				RawRules: map[string][]*rule.InstRawRule{
					"file2.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "raw1"}},
					},
				},
			},
			expectedFiles: []string{"file1.go", "file2.go"},
			validate: func(t *testing.T, grouped map[string][]rule.InstRule) {
				assert.Len(t, grouped["file1.go"], 2) // func1 + struct1
				assert.Len(t, grouped["file2.go"], 2) // func2 + raw1
			},
		},
		{
			name: "multiple rules of same type in same file",
			ruleSet: &rule.InstRuleSet{
				FuncRules: map[string][]*rule.InstFuncRule{
					"file1.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "func1"}},
						{InstBaseRule: rule.InstBaseRule{Name: "func2"}},
						{InstBaseRule: rule.InstBaseRule{Name: "func3"}},
					},
				},
				StructRules: make(map[string][]*rule.InstStructRule),
				RawRules:    make(map[string][]*rule.InstRawRule),
			},
			expectedFiles: []string{"file1.go"},
			validate: func(t *testing.T, grouped map[string][]rule.InstRule) {
				assert.Len(t, grouped["file1.go"], 3)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grouped := groupRules(tt.ruleSet)

			// Check expected files are present
			for _, file := range tt.expectedFiles {
				_, found := grouped[file]
				assert.True(t, found, "expected file %s not found in grouped rules", file)
			}

			// Check no unexpected files
			assert.Len(t, grouped, len(tt.expectedFiles))

			if tt.validate != nil {
				tt.validate(t, grouped)
			}
		})
	}
}

func TestFindActualFile(t *testing.T) {
	tests := []struct {
		name        string
		ruleFile    string
		compileArgs []string
		expectError bool
		expectedAbs string
	}{
		{
			name:     "exact match",
			ruleFile: "/tmp/project/main.go",
			compileArgs: []string{
				"compile",
				"-o", "output.a",
				"/tmp/project/main.go",
			},
			expectError: false,
			expectedAbs: "/tmp/project/main.go",
		},
		{
			name:     "basename match",
			ruleFile: "/cache/module@v1.0.0/file.go",
			compileArgs: []string{
				"compile",
				"-o", "output.a",
				"/cache/module@v1.2.0/file.go", // Different version
			},
			expectError: false,
			expectedAbs: "/cache/module@v1.2.0/file.go",
		},
		{
			name:     "file not found",
			ruleFile: "/tmp/project/missing.go",
			compileArgs: []string{
				"compile",
				"-o", "output.a",
				"/tmp/project/main.go",
			},
			expectError: true,
		},
		{
			name:     "non-go files ignored",
			ruleFile: "/tmp/project/main.go",
			compileArgs: []string{
				"compile",
				"-o", "output.a",
				"-p", "main",
				"/tmp/project/main.go",
			},
			expectError: false,
			expectedAbs: "/tmp/project/main.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := &InstrumentPhase{
				logger:      slog.Default(),
				compileArgs: tt.compileArgs,
			}

			actualFile, err := ip.findActualFile(tt.ruleFile)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "cannot find file")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedAbs, actualFile)
		})
	}
}

func TestFindActualFile_MultipleBasenameMatches(t *testing.T) {
	// When multiple files have same basename, should prefer exact match
	tempDir := t.TempDir()
	file1 := filepath.Join(tempDir, "dir1", "file.go")
	file2 := filepath.Join(tempDir, "dir2", "file.go")

	// Create the files
	require.NoError(t, os.MkdirAll(filepath.Dir(file1), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(file2), 0o755))
	require.NoError(t, os.WriteFile(file1, []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(file2, []byte("package main"), 0o644))

	ip := &InstrumentPhase{
		logger: slog.Default(),
		compileArgs: []string{
			"compile",
			"-o", "output.a",
			file1,
			file2,
		},
	}

	// Request file1 - should get exact match
	actualFile, err := ip.findActualFile(file1)
	require.NoError(t, err)
	assert.Equal(t, file1, actualFile)

	// Request file2 - should get exact match
	actualFile, err = ip.findActualFile(file2)
	require.NoError(t, err)
	assert.Equal(t, file2, actualFile)
}
