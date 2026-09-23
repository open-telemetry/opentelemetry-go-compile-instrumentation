// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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

	deps, err := injectedDeps(t.Context(), nil, known, t.TempDir(), nil)
	require.NoError(t, err)
	assert.Empty(t, deps)
}

// TestMatchInjectedDepsDegradesWhenLoadFails covers a closure that cannot be
// resolved. Matching injected dependencies is an improvement on the build plan,
// not a precondition for building, so failing to resolve them must leave the
// build running on the plan otelc already has.
func TestMatchInjectedDepsDegradesWhenLoadFails(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	set := rule.NewInstRuleSet("example.com/svc")
	set.AddFuncRule("/src/a.go", funcRuleWithPath("fn", "example.com/hooks"))

	extra, err := sp.matchInjectedDeps(
		t.Context(), []*rule.InstRuleSet{set}, nil, nil, "/nonexistent-dir-xyz", nil,
	)
	require.NoError(t, err, "an unresolvable closure must not fail an otherwise fine build")
	assert.Empty(t, extra)
}

func TestBuildFlagsForLoad(t *testing.T) {
	tests := map[string]struct {
		args []string
		want []string
	}{
		"nothing to carry":     {args: []string{"-o", "/tmp/x", "."}, want: []string{}},
		"joined tags":          {args: []string{"-tags=a,b", "."}, want: []string{"-tags=a,b"}},
		"separated tags":       {args: []string{"-tags", "a,b", "."}, want: []string{"-tags", "a,b"}},
		"valueless flags kept": {args: []string{"-race", "-a", "."}, want: []string{"-race"}},
		"mod and modfile": {
			args: []string{"-mod=mod", "-modfile", "go.alt.mod", "."},
			want: []string{"-mod=mod", "-modfile", "go.alt.mod"},
		},
		"value is not mistaken for a flag": {
			args: []string{"-modfile", "-weird.mod", "-tags=x"},
			want: []string{"-modfile", "-weird.mod", "-tags=x"},
		},
		"test binary args ignored": {
			args: []string{"-tags=x", "-args", "-tags=no"},
			want: []string{"-tags=x"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, buildFlagsForLoad(tc.args))
		})
	}
}

// TestRunInjectionPassesReportsNonConvergence covers the bound being reached.
// Truncating quietly would reintroduce the very failure this whole pass exists
// to remove: a rule that is never matched, on a green build.
func TestRunInjectionPassesReportsNonConvergence(t *testing.T) {
	n := 0
	pass := injectionPass{
		// Always reports a package never seen before, so the loop can never settle.
		load: func(_ context.Context, _ []string, _ map[string]bool) ([]*Dependency, error) {
			n++
			return []*Dependency{{ImportPath: fmt.Sprintf("example.com/pkg%d", n)}}, nil
		},
		match: func(_ context.Context, _ []*Dependency) ([]*rule.InstRuleSet, error) {
			set := rule.NewInstRuleSet("example.com/svc")
			set.AddFuncRule("/src/a.go", funcRuleWithPath("fn", "example.com/hooks"))
			return []*rule.InstRuleSet{set}, nil
		},
	}

	extra, converged, err := runInjectionPasses(t.Context(), nil, map[string]bool{}, 3, pass)
	require.NoError(t, err)
	assert.False(t, converged, "running out of passes must be reported, not passed over")
	assert.Len(t, extra, 3, "every pass that ran still contributes its rule sets")
	assert.Equal(t, 3, n, "the bound must cap the number of passes")
}

func TestRunInjectionPassesConverges(t *testing.T) {
	calls := 0
	pass := injectionPass{
		load: func(_ context.Context, _ []string, _ map[string]bool) ([]*Dependency, error) {
			calls++
			if calls > 1 {
				return nil, nil // nothing new the second time round
			}
			return []*Dependency{{ImportPath: "example.com/pkg"}}, nil
		},
		match: func(_ context.Context, _ []*Dependency) ([]*rule.InstRuleSet, error) {
			set := rule.NewInstRuleSet("example.com/svc")
			set.AddFuncRule("/src/a.go", funcRuleWithPath("fn", "example.com/hooks"))
			return []*rule.InstRuleSet{set}, nil
		},
	}

	extra, converged, err := runInjectionPasses(t.Context(), nil, map[string]bool{}, maxInjectionPasses, pass)
	require.NoError(t, err)
	assert.True(t, converged)
	assert.Len(t, extra, 1)
}
