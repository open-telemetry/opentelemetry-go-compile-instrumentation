// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
)

func funcRuleWithPath(name, path string) *rule.InstFuncRule {
	return &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{Name: name, Target: rule.NewTarget("example.com/svc")},
		Func:         "Handler",
		Before:       "BeforeHandler",
		Path:         path,
	}
}

func fileRuleWithPath(name, path string) *rule.InstFileRule {
	return &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: name, Target: rule.NewTarget("example.com/svc")},
		Path:         path,
	}
}

// fakeSrcFile returns an OS-appropriate absolute path for a rule's source
// file. AddFuncRule only asserts the path is absolute and uses it as a map
// key; the file is never created or read.
func fakeSrcFile(name string) string {
	return filepath.Join(os.TempDir(), name)
}

func TestHookPackagePaths(t *testing.T) {
	t.Run("no rules", func(t *testing.T) {
		assert.Empty(t, hookPackagePaths(nil))
	})

	t.Run("collects func and file rule paths", func(t *testing.T) {
		set := rule.NewInstRuleSet("example.com/svc")
		set.AddFuncRule(fakeSrcFile("a.go"), funcRuleWithPath("fn", "example.com/hooks"))
		set.AddFileRule(fileRuleWithPath("file", "example.com/filehooks"))

		assert.ElementsMatch(t,
			[]string{"example.com/hooks", "example.com/filehooks"},
			hookPackagePaths([]*rule.InstRuleSet{set}),
		)
	})

	t.Run("deduplicates across rules and sets", func(t *testing.T) {
		first := rule.NewInstRuleSet("example.com/one")
		first.AddFuncRule(fakeSrcFile("a.go"), funcRuleWithPath("fn1", "example.com/hooks"))
		first.AddFuncRule(fakeSrcFile("b.go"), funcRuleWithPath("fn2", "example.com/hooks"))

		second := rule.NewInstRuleSet("example.com/two")
		second.AddFuncRule(fakeSrcFile("c.go"), funcRuleWithPath("fn3", "example.com/hooks"))

		assert.Equal(t,
			[]string{"example.com/hooks"},
			hookPackagePaths([]*rule.InstRuleSet{first, second}),
		)
	})

	t.Run("skips rules with no path", func(t *testing.T) {
		set := rule.NewInstRuleSet("example.com/svc")
		set.AddFuncRule(fakeSrcFile("a.go"), funcRuleWithPath("fn", ""))

		assert.Empty(t, hookPackagePaths([]*rule.InstRuleSet{set}))
	})
}

// TestInjectedDepsSkipsKnownPackages covers the empty-input guard: no hook
// paths means no load is attempted at all.
func TestInjectedDepsSkipsKnownPackages(t *testing.T) {
	known := map[string]bool{"example.com/hooks": true}

	deps, err := injectedDeps(t.Context(), nil, known, nil)
	require.NoError(t, err)
	assert.Empty(t, deps)
}

// TestInjectedDepsSkipsAlreadyKnownDependency loads a real hook package whose
// own path is already in known, and asserts it is left out of the result
// while its not-yet-known dependency is still resolved and returned. This is
// the load-then-filter path the nil-input case above cannot exercise.
func TestInjectedDepsSkipsAlreadyKnownDependency(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/injecttest\n\ngo 1.24\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "hooks.go"),
		"package hooks\n\nimport _ \"example.com/injecttest/internal\"\n")
	mustWriteFile(t, filepath.Join(dir, "internal", "internal.go"), "package internal\n")

	known := map[string]bool{"example.com/injecttest/hooks": true}
	deps, err := injectedDeps(
		t.Context(), []string{"example.com/injecttest/hooks"}, known, []string{"-C", dir},
	)
	require.NoError(t, err)

	require.Len(t, deps, 1)
	assert.Equal(t, "example.com/injecttest/internal", deps[0].ImportPath)
	assert.True(t, known["example.com/injecttest/internal"], "the resolved dependency must be marked known")
}

// TestInjectedDepsReturnsErrorOnPackageLoadFailure covers a packages.Load call
// that returns a nil top-level error while still reporting a failure on one
// of the loaded packages: walking past that would match rules against a
// closure that is silently missing part of what the hook actually imports.
func TestInjectedDepsReturnsErrorOnPackageLoadFailure(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/injecttest\n\ngo 1.24\n")
	mustWriteFile(t, filepath.Join(dir, "broken", "broken.go"),
		"package broken\n\nimport _ \"example.com/injecttest/missing\"\n")

	_, err := injectedDeps(
		t.Context(), []string{"example.com/injecttest/broken"}, map[string]bool{}, []string{"-C", dir},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "example.com/injecttest/missing")
}

// TestMatchInjectedDepsDegradesWhenLoadFails covers a closure that cannot be
// resolved. Matching injected dependencies is an improvement on the build plan,
// not a precondition for building, so failing to resolve them must leave the
// build running on the plan otelc already has.
func TestMatchInjectedDepsDegradesWhenLoadFails(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	missingDir := filepath.Join(t.TempDir(), "missing")

	set := rule.NewInstRuleSet("example.com/svc")
	set.AddFuncRule(fakeSrcFile("a.go"), funcRuleWithPath("fn", "example.com/hooks"))

	extra, err := sp.matchInjectedDeps(
		t.Context(), []*rule.InstRuleSet{set}, nil, nil, []string{"-C", missingDir},
	)
	require.NoError(t, err, "an unresolvable closure must not fail an otherwise fine build")
	assert.Empty(t, extra)
}

// Build-flag extraction itself is covered by the existing TestExtractBuildFlags
// in setup_test.go; matchInjectedDeps now reuses that function directly.

func TestRunInjectionPassesFollowsLongChain(t *testing.T) {
	const chainLength = 6
	n := 0
	pass := injectionPass{
		load: func(_ context.Context, _ []string, _ map[string]bool) ([]*Dependency, error) {
			n++
			if n > chainLength {
				return nil, nil
			}
			return []*Dependency{{ImportPath: fmt.Sprintf("example.com/pkg%d", n)}}, nil
		},
		match: func(_ context.Context, _ []*Dependency) ([]*rule.InstRuleSet, error) {
			set := rule.NewInstRuleSet("example.com/svc")
			set.AddFuncRule(
				fakeSrcFile(fmt.Sprintf("%d.go", n)),
				funcRuleWithPath("fn", fmt.Sprintf("example.com/hooks/%d", n)),
			)
			return []*rule.InstRuleSet{set}, nil
		},
	}

	extra, err := runInjectionPasses(t.Context(), nil, map[string]bool{}, pass)
	require.NoError(t, err)
	assert.Len(t, extra, chainLength)
	assert.Equal(t, chainLength+1, n, "the pass after the last match must detect convergence")
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
			set.AddFuncRule(fakeSrcFile("a.go"), funcRuleWithPath("fn", "example.com/hooks"))
			return []*rule.InstRuleSet{set}, nil
		},
	}

	extra, err := runInjectionPasses(t.Context(), nil, map[string]bool{}, pass)
	require.NoError(t, err)
	assert.Len(t, extra, 1)
}
