// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"
	"path/filepath"
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
		file2rules := make(map[string][]rule.InstRule)
		cgoMap := map[string]string{"pkg.cgo1.go": "cgo1.go"}
		addIgnoredCallFilesToMap([]string{"pkg.cgo1.go"}, file2rules, cgoMap, "/build/work")

		_, exists := file2rules["/build/work/cgo1.go"]
		assert.True(t, exists, "cgo-mapped file must be keyed by its remapped path")
		_, unmapped := file2rules["pkg.cgo1.go"]
		assert.False(t, unmapped)
	})
}
