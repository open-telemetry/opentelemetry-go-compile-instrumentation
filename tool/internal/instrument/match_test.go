// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestLoadMissingMatchedRules(t *testing.T) {
	// Point the work dir at an empty directory: matched.json does not exist,
	// which is what a bare -toolexec build sees when setup never ran.
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	ip := &instrumentPhase{logger: slog.Default()}
	_, err := ip.load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "otelc setup")
}

func TestMatchPicksRuleSetOfThisPackage(t *testing.T) {
	// Every main package of a build compiles with -p main.
	dir := t.TempDir()
	mainA := filepath.Join(dir, "a", "main.go")
	mainB := filepath.Join(dir, "b", "main.go")
	argsA := []string{"compile", "-o", "_pkg_.a", "-p", "main", "-buildid", "x", mainA}
	argsB := []string{"compile", "-o", "_pkg_.a", "-p", "main", "-buildid", "x", mainB}
	ip := &instrumentPhase{logger: slog.Default(), workDir: dir}

	setA := rule.NewInstRuleSet("main")
	setA.AddFuncRule(mainA, &rule.InstFuncRule{Func: "Hello"})
	setB := rule.NewInstRuleSet("main")
	setB.AddFuncRule(mainB, &rule.InstFuncRule{Func: "Hello"})

	assert.Same(t, setA, ip.match([]*rule.InstRuleSet{setA}, argsA))
	assert.Nil(t, ip.match([]*rule.InstRuleSet{setA}, argsB),
		"rules matched in a/main.go must not be applied to package b")
	assert.Same(t, setB, ip.match([]*rule.InstRuleSet{setA, setB}, argsB))

	// A set that names no file, such as one with only file rules, applies to
	// any package with the import path.
	fileRulesOnly := rule.NewInstRuleSet("main")
	assert.Same(t, fileRulesOnly, ip.match([]*rule.InstRuleSet{fileRulesOnly}, argsB))
}

func TestMatchPicksRuleSetOfSameCompileKind(t *testing.T) {
	// go test compiles example.com/lib with lib_test.go for its own test
	// binary, and without it for the test binaries of packages importing it.
	normalArgs := []string{"compile", "-o", "_pkg_.a", "-p", "example.com/lib", "-buildid", "x", "lib.go"}
	testArgs := append(slices.Clone(normalArgs), "lib_test.go")
	ip := &instrumentPhase{logger: slog.Default()}

	t.Run("a normal build set is not applied to the test build", func(t *testing.T) {
		normal := rule.NewInstRuleSet("example.com/lib")
		allSet := []*rule.InstRuleSet{normal}
		assert.Same(t, normal, ip.match(allSet, normalArgs))
		assert.Nil(t, ip.match(allSet, testArgs))
	})
	t.Run("each build gets its own set", func(t *testing.T) {
		normal := rule.NewInstRuleSet("example.com/lib")
		test := rule.NewInstRuleSet("example.com/lib")
		test.IsTest = true
		allSet := []*rule.InstRuleSet{test, normal}
		assert.Same(t, normal, ip.match(allSet, normalArgs))
		assert.Same(t, test, ip.match(allSet, testArgs))
	})
}

func TestAnyRuleSetHasIgnoredCallFiles(t *testing.T) {
	t.Run("empty set", func(t *testing.T) {
		assert.False(t, anyRuleSetHasIgnoredCallFiles(nil))
	})
	t.Run("no rule set has one", func(t *testing.T) {
		allSet := []*rule.InstRuleSet{
			rule.NewInstRuleSet("example.com/a"),
			rule.NewInstRuleSet("example.com/b"),
		}
		assert.False(t, anyRuleSetHasIgnoredCallFiles(allSet))
	})
	t.Run("one rule set has one, not necessarily the caller's own", func(t *testing.T) {
		other := rule.NewInstRuleSet("example.com/other")
		other.AddIgnoredCallFile(filepath.Join(t.TempDir(), "other.go"))
		allSet := []*rule.InstRuleSet{
			rule.NewInstRuleSet("example.com/mine"),
			other,
		}
		assert.True(t, anyRuleSetHasIgnoredCallFiles(allSet))
	})
}

func TestAddIgnoredCallFilesToMap(t *testing.T) {
	t.Run("adds a nil entry for a file with no rule of its own", func(t *testing.T) {
		file2rules := make(map[string][]rule.InstRule)
		addIgnoredCallFilesToMap([]string{"only-ignore.go"}, file2rules, nil, "")

		rules, exists := file2rules["only-ignore.go"]
		require.True(t, exists)
		assert.Nil(t, rules)
	})

	t.Run("leaves an existing entry untouched", func(t *testing.T) {
		existing := []rule.InstRule{&rule.InstFuncRule{InstBaseRule: rule.InstBaseRule{Name: "r"}}}
		file2rules := map[string][]rule.InstRule{"shared.go": existing}
		addIgnoredCallFilesToMap([]string{"shared.go"}, file2rules, nil, "")

		assert.Len(t, file2rules["shared.go"], 1)
	})

	t.Run("remaps a cgo file through workDir", func(t *testing.T) {
		workDir := t.TempDir()
		file2rules := make(map[string][]rule.InstRule)
		cgoMap := map[string]string{"pkg.cgo1.go": "cgo1.go"}
		addIgnoredCallFilesToMap([]string{"pkg.cgo1.go"}, file2rules, cgoMap, workDir)

		_, exists := file2rules[filepath.Join(workDir, "cgo1.go")]
		assert.True(t, exists, "cgo-mapped file must be keyed by its remapped path")
		_, unmapped := file2rules["pkg.cgo1.go"]
		assert.False(t, unmapped)
	})
}

func TestLoadImportNames_MissingFileIsNotAnError(t *testing.T) {
	// import_names.json only improves a guess. An older setup run
	// without the file must still succeed.
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	names, err := loadImportNames()
	require.NoError(t, err)
	assert.Empty(t, names)
}

func TestLoadImportNames_ValidFile(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(
		util.GetImportNamesFile(),
		[]byte(`{"github.com/redis/go-redis/v9":"redis"}`),
		0o644,
	))

	names, err := loadImportNames()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"github.com/redis/go-redis/v9": "redis"}, names)
}

func TestLoadImportNames_MalformedFile(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetImportNamesFile(), []byte("not json"), 0o644))

	_, err := loadImportNames()
	require.Error(t, err)
}

func TestLoadImportNames_ReadErrorOtherThanMissing(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetImportNamesFile(), 0o755))

	_, err := loadImportNames()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}
