// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func funcRuleWithPath(name, path string) *rule.InstFuncRule {
	return &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: name, Target: "example.com/svc"},
		Func:         "Handler",
		Before:       "BeforeHandler",
		Path:         path,
	}
}

func fileRuleWithPath(name, path string) *rule.InstFileRule {
	return &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: name, Target: "example.com/svc"},
		Path:         path,
	}
}

func TestHookPackagePaths(t *testing.T) {
	t.Run("no rules", func(t *testing.T) {
		assert.Empty(t, hookPackagePaths(nil))
	})

	t.Run("collects func and file rule paths", func(t *testing.T) {
		set := rule.NewInstRuleSet("example.com/svc")
		set.AddFuncRule("/src/a.go", funcRuleWithPath("fn", "example.com/hooks"))
		set.AddFileRule(fileRuleWithPath("file", "example.com/filehooks"))

		assert.ElementsMatch(t,
			[]string{"example.com/hooks", "example.com/filehooks"},
			hookPackagePaths([]*rule.InstRuleSet{set}),
		)
	})

	t.Run("deduplicates across rules and sets", func(t *testing.T) {
		first := rule.NewInstRuleSet("example.com/one")
		first.AddFuncRule("/src/a.go", funcRuleWithPath("fn1", "example.com/hooks"))
		first.AddFuncRule("/src/b.go", funcRuleWithPath("fn2", "example.com/hooks"))

		second := rule.NewInstRuleSet("example.com/two")
		second.AddFuncRule("/src/c.go", funcRuleWithPath("fn3", "example.com/hooks"))

		assert.Equal(t,
			[]string{"example.com/hooks"},
			hookPackagePaths([]*rule.InstRuleSet{first, second}),
		)
	})

	t.Run("skips rules with no path", func(t *testing.T) {
		set := rule.NewInstRuleSet("example.com/svc")
		set.AddFuncRule("/src/a.go", funcRuleWithPath("fn", ""))

		assert.Empty(t, hookPackagePaths([]*rule.InstRuleSet{set}))
	})
}

// TestInjectedDepsSkipsKnownPackages pins the filtering half of injectedDeps
// without loading any package: a hook path that is already in the build plan
// contributes nothing, so no second matching pass is triggered for it.
func TestInjectedDepsSkipsKnownPackages(t *testing.T) {
	known := map[string]bool{"example.com/hooks": true}

	deps, err := injectedDeps(t.Context(), nil, known, t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, deps)
}
