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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/ast"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"gotest.tools/v3/golden"
)

// helperPkg holds a compiled helper package for use in golden tests.
type helperPkg struct {
	importPath string
	archive    string
}

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
	t.Setenv(util.EnvOtelcWorkDir, tempDir)
	ctx := util.ContextWithLogger(
		t.Context(),
		slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})),
	)

	sourceFile := filepath.Join(tempDir, mainGoFileName)
	// Each test case must provide its own source.go in its golden directory.
	testSpecificSource := filepath.Join(testdataDir, goldenDir, testName, sourceFileName)
	require.NoError(t, util.CopyFile(testSpecificSource, sourceFile),
		"missing source.go for test %q at %s", testName, testSpecificSource)

	ruleSet := loadRulesYAML(t, testName, sourceFile)
	writeMatchedJSON(ruleSet)

	testcaseDir := filepath.Join(testdataDir, goldenDir, testName)
	helpers := buildTestcaseHelpers(ctx, t, testcaseDir)

	args := compileArgs(tempDir, sourceFile, helpers)
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

	// Parse the source AST once and reuse it for every rule's where.file gating
	// below. The gating is per-rule, but the tree is shared, so N rules do not
	// trigger N reparses of the same file.
	sourceTree, parseErr := ast.ParseFileFast(sourceFile)
	require.NoError(t, parseErr)

	ruleSet := &rule.InstRuleSet{
		PackageName:    mainPackage,
		ModulePath:     mainPackage,
		FuncRules:      make(map[string][]*rule.InstFuncRule),
		StructRules:    make(map[string][]*rule.InstStructRule),
		RawRules:       make(map[string][]*rule.InstRawRule),
		CallRules:      make(map[string][]*rule.InstCallRule),
		DirectiveRules: make(map[string][]*rule.InstDirectiveRule),
		DeclRules:      make(map[string][]*rule.InstDeclRule),
		FileRules:      make([]*rule.InstFileRule, 0),
	}

	// Sort rule names to ensure deterministic order in tests
	ruleNames := make([]string, 0, len(rawRules))
	for name := range rawRules {
		ruleNames = append(ruleNames, name)
	}
	slices.Sort(ruleNames)

	for _, name := range ruleNames {
		propsList, normErr := rule.Normalize(rawRules[name])
		require.NoError(t, normErr)
		for _, props := range propsList {
			props["name"] = name
			ruleData, _ := yaml.Marshal(props)

			// The golden harness has no setup phase, so the where.file filter
			// that setup.preciseMatching would evaluate is applied inline here.
			// A rule whose file predicate does not match the source is skipped,
			// exactly as it would be gated out during matching.
			if !whereFileMatches(t, ruleData, sourceTree) {
				continue
			}

			// Mirror the setup-phase target-glob gate: a glob target only
			// applies when it matches the package import path. Exact targets
			// (the common case) always pass through. This lets golden fixtures
			// prove glob match vs no-match end to end.
			if !targetMatches(props, mainPackage) {
				continue
			}

			switch {
			case props["struct"] != nil:
				r, _ := rule.NewInstStructRule(ruleData, name)
				ruleSet.StructRules[sourceFile] = append(ruleSet.StructRules[sourceFile], r)
			case props["file"] != nil:
				r, _ := rule.NewInstFileRule(ruleData, name)
				ruleSet.FileRules = append(ruleSet.FileRules, r)
			case props["directive"] != nil:
				r, _ := rule.NewInstDirectiveRule(ruleData, name)
				ruleSet.DirectiveRules[sourceFile] = append(ruleSet.DirectiveRules[sourceFile], r)
			case props["raw"] != nil:
				r, _ := rule.NewInstRawRule(ruleData, name)
				ruleSet.RawRules[sourceFile] = append(ruleSet.RawRules[sourceFile], r)
			case props["func"] != nil:
				r, _ := rule.NewInstFuncRule(ruleData, name)
				ruleSet.FuncRules[sourceFile] = append(ruleSet.FuncRules[sourceFile], r)
			case props["function_call"] != nil:
				r, _ := rule.NewInstCallRule(ruleData, name)
				ruleSet.CallRules[sourceFile] = append(ruleSet.CallRules[sourceFile], r)
			case props["identifier"] != nil:
				r, _ := rule.NewInstDeclRule(ruleData, name)
				ruleSet.DeclRules[sourceFile] = append(ruleSet.DeclRules[sourceFile], r)
			}
		}
	}

	return ruleSet
}

// whereFileMatches evaluates the rule's where.file predicate against the
// already-parsed source tree, mirroring the gating that setup.preciseMatching
// performs. It returns true when there is no file predicate. The golden harness
// builds the matched rule set by hand (no setup phase), so this keeps fixtures
// honest: a rule whose file filter does not match is gated out and produces no
// instrumentation. The caller parses the tree once and shares it across rules.
func whereFileMatches(t *testing.T, ruleData []byte, tree *dst.File) bool {
	t.Helper()

	var probe struct {
		Where *rule.WhereDef `yaml:"where"`
	}
	require.NoError(t, yaml.Unmarshal(ruleData, &probe))
	if probe.Where == nil || probe.Where.File == nil {
		return true
	}

	return fileFilterMatches(t, probe.Where.File, tree)
}

// fileFilterMatches reports whether a where.file predicate matches the parsed
// source. It covers the predicates exercised by golden fixtures: all-of and
// one-of composition plus has_func / has_struct leaves. Filter compilation and
// match semantics are unit-tested in tool/internal/setup; this is a lightweight
// stand-in for the golden harness only.
//
// Any predicate this evaluator does not model is rejected with t.Fatalf rather
// than silently treated as a match: setup.buildFile errors on such predicates,
// so silently matching here would let a fixture certify instrumentation output
// that production could never produce.
func fileFilterMatches(t *testing.T, def *rule.FilterDef, tree *dst.File) bool {
	t.Helper()
	// Mirror setup.buildFile's exclusivity rule: a combinator owns the node, so
	// combining it with a sibling leaf or another combinator is a build-time
	// error in production. Reject it loudly here too, so an invalid fixture
	// cannot pass the golden harness when production would refuse the rule.
	combinators := 0
	for _, present := range []bool{def.AllOf != nil, def.OneOf != nil, def.Not != nil} {
		if present {
			combinators++
		}
	}
	leaf := def.HasFunc != "" || def.HasRecv != "" || def.HasStruct != "" || def.HasDirective != ""
	if combinators > 1 || (combinators == 1 && leaf) {
		t.Fatalf("golden fixture combines a where.file combinator with sibling "+
			"predicates (%+v); setup.buildFile rejects this at build time", def)
	}
	// Mirror setup.buildFile's presence semantics: a non-nil combinator slice
	// (including an explicit empty one-of: [] / all-of: []) is present and owns
	// the node. An empty one-of matches nothing (vacuous false); an empty all-of
	// matches vacuously true — the loop is skipped in each case.
	if def.OneOf != nil {
		for i := range def.OneOf {
			if fileFilterMatches(t, &def.OneOf[i], tree) {
				return true
			}
		}
		return false
	}
	if def.AllOf != nil {
		for i := range def.AllOf {
			if !fileFilterMatches(t, &def.AllOf[i], tree) {
				return false
			}
		}
		return true
	}
	// not is unary: it negates its single inner predicate (mirrors setup.Not).
	if def.Not != nil {
		return !fileFilterMatches(t, def.Not, tree)
	}
	switch {
	case def.HasFunc != "":
		_, ok, _ := ast.FindFuncDecl(tree, def)
		return ok
	case def.HasStruct != "":
		return ast.FindStructDecl(tree, def.HasStruct) != nil
	default:
		t.Fatalf("golden fixture uses a where.file predicate the harness cannot "+
			"evaluate (%+v); extend fileFilterMatches to mirror setup.buildFile", def)
		return false
	}
}

// targetMatches reports whether a rule's target selects importPath, mirroring
// setup-phase package selection: a glob target matches via MatchGlobTarget, an
// exact target matches only on equality. A missing, non-string, or empty target
// never matches, so an invalid fixture fails the golden test instead of silently
// being applied.
func targetMatches(props map[string]any, importPath string) bool {
	target, ok := props["target"].(string)
	if !ok || strings.TrimSpace(target) == "" {
		return false
	}
	if rule.IsGlobTarget(target) {
		return rule.MatchGlobTarget(target, importPath)
	}
	return target == importPath
}

func writeMatchedJSON(ruleSet *rule.InstRuleSet) {
	matchedJSON, _ := json.Marshal([]*rule.InstRuleSet{ruleSet})
	matchedFile := util.GetMatchedRuleFile()
	os.MkdirAll(filepath.Dir(matchedFile), 0o755)
	util.WriteFile(matchedFile, string(matchedJSON))
}

func compileArgs(tempDir, sourceFile string, helpers []helperPkg) []string {
	output, _ := exec.Command("go", "env", "GOTOOLDIR").Output()

	// Create importcfg file for the test
	importCfgPath := filepath.Join(tempDir, "importcfg")
	createImportCfg(importCfgPath, helpers)

	return []string{
		filepath.Join(strings.TrimSpace(string(output)), "compile"),
		"-o", filepath.Join(tempDir, compiledOutput),
		"-p", mainPackage,
		"-complete",
		"-buildid", buildID,
		"-importcfg", importCfgPath,
		"-pack",
		sourceFile,
	}
}

// createImportCfg creates an importcfg file with standard library packages
// and any additional helper packages built for the testcase.
func createImportCfg(path string, helpers []helperPkg) {
	// Get standard library package locations
	// We'll use go list to populate common packages
	ctx := context.Background()

	// Start with an empty config
	cfg := struct {
		PackageFile map[string]string
	}{
		PackageFile: make(map[string]string),
	}

	// Resolve common standard library packages that might be needed
	commonPkgs := []string{"fmt", "unsafe", "runtime", "strings", "io"}
	for _, pkg := range commonPkgs {
		cmd := exec.CommandContext(ctx, "go", "list", "-export", "-json", pkg)
		output, err := cmd.Output()
		if err != nil {
			continue // Skip if package not found
		}

		var info struct {
			ImportPath string `json:"ImportPath"`
			Export     string `json:"Export"`
		}
		if err2 := json.Unmarshal(output, &info); err2 == nil && info.Export != "" {
			cfg.PackageFile[info.ImportPath] = info.Export
		}
	}

	// Register testcase-local helper packages
	for _, h := range helpers {
		cfg.PackageFile[h.importPath] = h.archive
	}

	// Write the importcfg file
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()

	for importPath, archive := range cfg.PackageFile {
		fmt.Fprintf(f, "packagefile %s=%s\n", importPath, archive)
	}
}

// buildTestcaseHelpers discovers Go helper packages under <testcaseDir>/helpers/,
// compiles each one via "go list -export -json" and returns the resulting
// (importPath, archivePath) pairs so they can be added to the importcfg.
func buildTestcaseHelpers(ctx context.Context, t *testing.T, testcaseDir string) []helperPkg {
	helpersDir := filepath.Join(testcaseDir, "helpers")
	entries, readErr := os.ReadDir(helpersDir)
	if os.IsNotExist(readErr) {
		return nil
	}
	require.NoError(t, readErr, "reading helpers dir %s", helpersDir)

	var out []helperPkg
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkgPath := "./" + filepath.ToSlash(filepath.Join(helpersDir, e.Name()))

		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, "go", "list", "-export", "-json", pkgPath)
		cmd.Stderr = &stderr
		listOut, listErr := cmd.Output()
		require.NoError(t, listErr, "go list -export -json %s: %s", pkgPath, stderr.String())

		var info struct {
			ImportPath string `json:"ImportPath"`
			Export     string `json:"Export"`
		}
		require.NoError(t, json.Unmarshal(listOut, &info))

		out = append(out, helperPkg{importPath: info.ImportPath, archive: info.Export})
	}
	return out
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
			name: "decl rules only",
			ruleSet: &rule.InstRuleSet{
				FuncRules:   make(map[string][]*rule.InstFuncRule),
				StructRules: make(map[string][]*rule.InstStructRule),
				RawRules:    make(map[string][]*rule.InstRawRule),
				DeclRules: map[string][]*rule.InstDeclRule{
					"file1.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "decl1"}, Identifier: "GlobalVar"},
					},
				},
			},
			expectedFiles: []string{"file1.go"},
			validate: func(t *testing.T, grouped map[string][]rule.InstRule) {
				assert.Len(t, grouped["file1.go"], 1)
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
		{
			name: "call rules only",
			ruleSet: &rule.InstRuleSet{
				FuncRules:   make(map[string][]*rule.InstFuncRule),
				StructRules: make(map[string][]*rule.InstStructRule),
				RawRules:    make(map[string][]*rule.InstRawRule),
				CallRules: map[string][]*rule.InstCallRule{
					"file1.go": {
						{InstBaseRule: rule.InstBaseRule{Name: "call1"}},
					},
				},
			},
			expectedFiles: []string{"file1.go"},
			validate: func(t *testing.T, grouped map[string][]rule.InstRule) {
				assert.Len(t, grouped["file1.go"], 1)
			},
		},
		{
			name: "directive rules included in grouping",
			ruleSet: &rule.InstRuleSet{
				FuncRules:   make(map[string][]*rule.InstFuncRule),
				StructRules: make(map[string][]*rule.InstStructRule),
				RawRules:    make(map[string][]*rule.InstRawRule),
				CallRules:   make(map[string][]*rule.InstCallRule),
				DirectiveRules: map[string][]*rule.InstDirectiveRule{
					"file1.go": {
						{
							InstBaseRule: rule.InstBaseRule{Name: "directive1"},
							Directive:    "otelc:span",
							Template:     "_ = 0",
						},
					},
				},
			},
			expectedFiles: []string{"file1.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grouped := groupRules("", tt.ruleSet)

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
