// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package instrument

import (
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
	"gopkg.in/yaml.v3"
	"gotest.tools/v3/golden"
)

func TestInstrumentation_Integration(t *testing.T) {
	entries, _ := os.ReadDir("testdata/golden")
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
	ctx := util.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	sourceFile := filepath.Join(tempDir, "main.go")
	util.CopyFile("testdata/source.go", sourceFile)

	ruleSet := loadRulesYAML(testName, sourceFile)
	writeMatchedJSON(tempDir, ruleSet)

	args := compileArgs(tempDir, sourceFile)
	err := Toolexec(ctx, args)

	if testName == "invalid-receiver" {
		require.Error(t, err)
		require.Contains(t, err.Error(), "can not find function")
		return
	}

	require.NoError(t, err)
	verifyGoldenFiles(t, tempDir, testName)
}

func loadRulesYAML(testName, sourceFile string) *rule.InstRuleSet {
	data, _ := os.ReadFile(filepath.Join("testdata/golden", testName, "rules.yml"))

	var rawRules map[string]map[string]interface{}
	yaml.Unmarshal(data, &rawRules)

	ruleSet := &rule.InstRuleSet{
		PackageName: "main",
		ModulePath:  "main",
		FuncRules:   make(map[string][]*rule.InstFuncRule),
		StructRules: make(map[string][]*rule.InstStructRule),
		RawRules:    make(map[string][]*rule.InstRawRule),
		FileRules:   make([]*rule.InstFileRule, 0),
	}

	for name, props := range rawRules {
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

func writeMatchedJSON(tempDir string, ruleSet *rule.InstRuleSet) {
	matchedJSON, _ := json.Marshal([]*rule.InstRuleSet{ruleSet})
	matchedFile := filepath.Join(tempDir, util.BuildTempDir, "matched.json")
	os.MkdirAll(filepath.Dir(matchedFile), 0o755)
	util.WriteFile(matchedFile, string(matchedJSON))
}

func compileArgs(tempDir, sourceFile string) []string {
	output, _ := exec.Command("go", "env", "GOTOOLDIR").Output()
	return []string{
		filepath.Join(strings.TrimSpace(string(output)), "compile"),
		"-o", filepath.Join(tempDir, "_pkg_.a"),
		"-p", "main",
		"-complete",
		"-buildid", "foo/bar",
		"-pack",
		sourceFile,
	}
}

func verifyGoldenFiles(t *testing.T, tempDir, testName string) {
	entries, _ := os.ReadDir(filepath.Join("testdata/golden", testName))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".golden") {
			continue
		}
		actualFile := actualFileFromGolden(entry.Name())
		if actualFile == "" {
			continue
		}
		actual, _ := os.ReadFile(filepath.Join(tempDir, actualFile))
		golden.Assert(t, string(actual), filepath.Join("golden", testName, entry.Name()))
	}
}

func actualFileFromGolden(goldenName string) string {
	parts := strings.SplitN(strings.TrimSuffix(goldenName, ".golden"), ".", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}
