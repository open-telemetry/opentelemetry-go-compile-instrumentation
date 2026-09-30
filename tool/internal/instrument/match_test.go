// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"
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
		other.AddIgnoredCallFile("/abs/other.go")
		allSet := []*rule.InstRuleSet{
			rule.NewInstRuleSet("example.com/mine"),
			other,
		}
		assert.True(t, anyRuleSetHasIgnoredCallFiles(allSet))
	})
}
