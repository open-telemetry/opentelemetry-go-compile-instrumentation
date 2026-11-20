// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}

	t.Run("no rules", func(t *testing.T) {
		assert.Empty(t, sp.hookPackagePaths(nil))
	})

	t.Run("collects func and file rule paths", func(t *testing.T) {
		set := rule.NewInstRuleSet("example.com/svc")
		set.AddFuncRule(fakeSrcFile("a.go"), funcRuleWithPath("fn", "example.com/hooks"))
		set.AddFileRule(fileRuleWithPath("file", "example.com/filehooks"))

		assert.ElementsMatch(t,
			[]string{"example.com/hooks", "example.com/filehooks"},
			sp.hookPackagePaths([]*rule.InstRuleSet{set}),
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
			sp.hookPackagePaths([]*rule.InstRuleSet{first, second}),
		)
	})

	t.Run("skips rules with no path", func(t *testing.T) {
		set := rule.NewInstRuleSet("example.com/svc")
		set.AddFuncRule(fakeSrcFile("a.go"), funcRuleWithPath("fn", ""))

		assert.Empty(t, sp.hookPackagePaths([]*rule.InstRuleSet{set}))
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

// TestMatchInjectedDepsRespectsBuildFlags covers the buildFlags parameter
// setupLocked now passes straight through as sp.buildFlags instead of
// recomputing it. A hook package that only imports a second package under a
// build tag must bring that second package into the matched closure when the
// tag is active, and must not when it is absent — proving the flags given to
// matchInjectedDeps actually reach the packages it loads, not just that the
// call compiles.
func TestMatchInjectedDepsRespectsBuildFlags(t *testing.T) {
	t.Setenv("OTELC_RULES", "")

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/injecttest2\n\ngo 1.25.0\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "hooks.go"), "package hooks\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "extra.go"),
		"//go:build extra\n\npackage hooks\n\nimport _ \"example.com/injecttest2/extradep\"\n")
	mustWriteFile(t, filepath.Join(dir, "extradep", "extradep.go"),
		"package extradep\n\nfunc ExtraFn() {}\n")

	ruleFile := filepath.Join(dir, "rules.yaml")
	mustWriteFile(t, ruleFile, `extraRule:
  target: example.com/injecttest2/extradep
  func: ExtraFn
  raw: "_ = 1"
`)

	newSetupPhase := func() *setupPhase {
		return &setupPhase{logger: slog.New(slog.DiscardHandler), ruleConfig: ruleFile}
	}
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddFuncRule(fakeSrcFile("a.go"), funcRuleWithPath("fn", "example.com/injecttest2/hooks"))

	t.Run("tag absent, extradep is never imported and must not match", func(t *testing.T) {
		sp := newSetupPhase()
		idx, err := sp.loadRuleIndex(t.Context(), nil)
		require.NoError(t, err)
		extra, err := sp.matchInjectedDeps(
			t.Context(), []*rule.InstRuleSet{set}, nil, idx, []string{"-C", dir},
		)
		require.NoError(t, err)
		assert.Empty(t, extra)
	})

	t.Run("tag active, extradep enters the closure and its rule matches", func(t *testing.T) {
		sp := newSetupPhase()
		idx, err := sp.loadRuleIndex(t.Context(), nil)
		require.NoError(t, err)
		extra, err := sp.matchInjectedDeps(
			t.Context(), []*rule.InstRuleSet{set}, nil, idx, []string{"-C", dir, "-tags", "extra"},
		)
		require.NoError(t, err)
		require.Len(t, extra, 1)
		assert.Equal(t, "example.com/injecttest2/extradep", extra[0].ModulePath)
	})
}

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

	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	extra, err := sp.runInjectionPasses(t.Context(), nil, map[string]bool{}, pass)
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

	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	extra, err := sp.runInjectionPasses(t.Context(), nil, map[string]bool{}, pass)
	require.NoError(t, err)
	assert.Len(t, extra, 1)
}

func TestMatchInjectedDeps_ReusesRuleIndexAcrossPasses(t *testing.T) {
	for _, source := range []string{"custom", "tool files"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("OTELC_RULES", "")

			dir := t.TempDir()
			mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/repro\n\ngo 1.25.0\n")
			mustWriteFile(t, filepath.Join(dir, "app", "app.go"), "package app\n\nfunc Run() {}\n")
			mustWriteFile(t, filepath.Join(dir, "hook1", "hook1.go"),
				"package hook1\n\nimport _ \"example.com/repro/dep1\"\n\nfunc Hook1() {}\n")
			mustWriteFile(t, filepath.Join(dir, "dep1", "dep1.go"), "package dep1\n\nfunc Work1() {}\n")
			mustWriteFile(t, filepath.Join(dir, "hook2", "hook2.go"),
				"package hook2\n\nimport _ \"example.com/repro/dep2\"\n\nfunc Hook2() {}\n")
			mustWriteFile(t, filepath.Join(dir, "dep2", "dep2.go"), "package dep2\n\nfunc Work2() {}\n")
			mustWriteFile(t, filepath.Join(dir, "hook3", "hook3.go"), "package hook3\n\nfunc Hook3() {}\n")

			ruleFile := filepath.Join(dir, "rules.yaml")
			moduleDirs := []string{dir}
			if source == "tool files" {
				instrumentationDir := filepath.Join(dir, "instrumentation")
				nestedDir := filepath.Join(dir, "nested")
				writeInstrumentationModule(t, dir, "example.com/repro", false, map[string]string{
					"example.com/instrumentation": instrumentationDir,
				})
				writeInstrumentationModule(
					t,
					instrumentationDir,
					"example.com/instrumentation",
					false,
					map[string]string{
						"example.com/nested": nestedDir,
					},
				)
				writeInstrumentationModule(t, nestedDir, "example.com/nested", false, nil)
				ruleFile = filepath.Join(nestedDir, "rules.otelc.yaml")
			}
			mustWriteFile(t, ruleFile, `version: v1.0.0

ruleApp:
  target: example.com/repro/app
  func: Run
  before: Hook1
  path: example.com/repro/hook1

ruleDep1:
  target: example.com/repro/dep1
  func: Work1
  before: Hook2
  path: example.com/repro/hook2

ruleDep2:
  target: example.com/repro/dep2
  func: Work2
  before: Hook3
  path: example.com/repro/hook3
`)

			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			sp := &setupPhase{
				logger:     logger,
				ruleConfig: ruleFile,
			}

			if source == "tool files" {
				sp.ruleConfig = ""
			}

			appDep := &Dependency{
				ImportPath: "example.com/repro/app",
				Sources:    []string{filepath.Join(dir, "app", "app.go")},
				CgoFiles:   map[string]string{},
			}

			idx, err := sp.loadRuleIndex(t.Context(), moduleDirs)
			require.NoError(t, err)
			require.NoError(t, os.Remove(ruleFile))
			matched, err := sp.matchDeps(t.Context(), []*Dependency{appDep}, idx)
			require.NoError(t, err)
			require.Len(t, matched, 1)

			injected, err := sp.matchInjectedDeps(t.Context(), matched, []*Dependency{appDep}, idx, []string{"-C", dir})
			require.NoError(t, err)
			require.Len(t, injected, 2)

			matched = append(matched, injected...)
			paths := make([]string, 0, len(matched))
			for _, set := range matched {
				paths = append(paths, set.ModulePath)
			}
			assert.ElementsMatch(t, []string{
				"example.com/repro/app", "example.com/repro/dep1", "example.com/repro/dep2",
			}, paths)

			logOutput := logBuf.String()
			assert.Equal(t, 1, strings.Count(logOutput, "rules source:"), "rules must only be loaded once")
			assert.Equal(t, 1, strings.Count(logOutput, "Found available rules"), "rules must only be indexed once")
		})
	}
}

func TestRunInjectionPasses_Errors(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}

	t.Run("load error", func(t *testing.T) {
		pass := injectionPass{
			load: func(_ context.Context, _ []string, _ map[string]bool) ([]*Dependency, error) {
				return nil, assert.AnError
			},
		}
		_, err := sp.runInjectionPasses(t.Context(), nil, map[string]bool{}, pass)
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("match error", func(t *testing.T) {
		pass := injectionPass{
			load: func(_ context.Context, _ []string, _ map[string]bool) ([]*Dependency, error) {
				return []*Dependency{{ImportPath: "example.com/dep"}}, nil
			},
			match: func(_ context.Context, _ []*Dependency) ([]*rule.InstRuleSet, error) {
				return nil, assert.AnError
			},
		}
		_, err := sp.runInjectionPasses(t.Context(), nil, map[string]bool{}, pass)
		require.ErrorIs(t, err, assert.AnError)
	})
}

func TestFileRuleSourceImports(t *testing.T) {
	dir := t.TempDir()
	sourceCode := `//go:build ignore

package hooks

import (
	"context"
	_ "unsafe"
	_ "C"
	extra "example.com/extra"
	. "example.com/dot"
	_ "example.com/blank"
)
`
	sourceFile := "injected.go"
	mustWriteFile(t, filepath.Join(dir, sourceFile), sourceCode)

	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: "add-hook", Target: rule.NewTarget("main")},
		File:         sourceFile,
		Path:         "example.com/hooks",
		ResolvedPath: dir,
	}

	t.Run("extracts and sorts unique imports excluding C and unsafe", func(t *testing.T) {
		imports, err := sp.fileRuleSourceImports(fileRule)
		require.NoError(t, err)
		assert.Equal(t, []string{"context", "example.com/blank", "example.com/dot", "example.com/extra"}, imports)
	})

	t.Run("returns cached results on subsequent calls", func(t *testing.T) {
		imports, err := sp.fileRuleSourceImports(fileRule)
		require.NoError(t, err)
		assert.Equal(t, []string{"context", "example.com/blank", "example.com/dot", "example.com/extra"}, imports)

		// Delete the source file on disk; subsequent call must serve from cache without re-reading.
		require.NoError(t, os.Remove(filepath.Join(dir, sourceFile)))
		cached, err := sp.fileRuleSourceImports(fileRule)
		require.NoError(t, err)
		assert.Equal(t, imports, cached)
	})

	t.Run("inapplicable build constraints returns nil", func(t *testing.T) {
		inapplicable := "inapplicable.go"
		mustWriteFile(
			t,
			filepath.Join(dir, inapplicable),
			"//go:build ignore && non_matching_xyz\n\npackage hooks\nimport \"example.com/skip\"\n",
		)
		r := &rule.InstFileRule{
			InstBaseRule: rule.InstBaseRule{Name: "inapplicable", Target: rule.NewTarget("main")},
			File:         inapplicable,
			Path:         "example.com/hooks",
			ResolvedPath: dir,
		}
		imports, err := sp.fileRuleSourceImports(r)
		require.NoError(t, err)
		assert.Nil(t, imports)
	})

	t.Run("missing file returns error", func(t *testing.T) {
		r := &rule.InstFileRule{
			InstBaseRule: rule.InstBaseRule{Name: "missing", Target: rule.NewTarget("main")},
			File:         "does_not_exist.go",
			Path:         "example.com/hooks",
			ResolvedPath: dir,
		}
		_, err := sp.fileRuleSourceImports(r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading source file")
		assert.Contains(t, err.Error(), "does_not_exist.go")
	})

	t.Run("malformed source returns error", func(t *testing.T) {
		malformed := "malformed.go"
		mustWriteFile(t, filepath.Join(dir, malformed), "//go:build ignore\n\npackage hooks\nimport \"unclosed\n")
		r := &rule.InstFileRule{
			InstBaseRule: rule.InstBaseRule{Name: "malformed", Target: rule.NewTarget("main")},
			File:         malformed,
			Path:         "example.com/hooks",
			ResolvedPath: dir,
		}
		_, err := sp.fileRuleSourceImports(r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing imports for rule")
	})

	t.Run("invalid build constraints returns error", func(t *testing.T) {
		invalidBC := "invalid_bc.go"
		mustWriteFile(
			t,
			filepath.Join(dir, invalidBC),
			"//go:build (invalid &&\n\npackage hooks\nimport \"example.com/skip\"\n",
		)
		r := &rule.InstFileRule{
			InstBaseRule: rule.InstBaseRule{Name: "invalid-bc", Target: rule.NewTarget("main")},
			File:         invalidBC,
			Path:         "example.com/hooks",
			ResolvedPath: dir,
		}
		_, err := sp.fileRuleSourceImports(r)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "matching build constraints for rule")
	})

	t.Run("empty import path is skipped", func(t *testing.T) {
		emptyImp := "empty_imp.go"
		mustWriteFile(t, filepath.Join(dir, emptyImp), "//go:build ignore\n\npackage hooks\nimport \"\"\n")
		r := &rule.InstFileRule{
			InstBaseRule: rule.InstBaseRule{Name: "empty-imp", Target: rule.NewTarget("main")},
			File:         emptyImp,
			Path:         "example.com/hooks",
			ResolvedPath: dir,
		}
		imports, err := sp.fileRuleSourceImports(r)
		require.NoError(t, err)
		assert.Empty(t, imports)
	})

	t.Run("empty path or file returns nil", func(t *testing.T) {
		imports, err := sp.fileRuleSourceImports(&rule.InstFileRule{})
		require.NoError(t, err)
		assert.Nil(t, imports)
	})

	t.Run("nil rule returns nil", func(t *testing.T) {
		imports, err := sp.fileRuleSourceImports(nil)
		require.NoError(t, err)
		assert.Nil(t, imports)
	})
}

func TestHookPackagePaths_FileRuleSourceImports(t *testing.T) {
	dir := t.TempDir()
	sourceFile := "injected.go"
	mustWriteFile(
		t,
		filepath.Join(dir, sourceFile),
		"//go:build ignore\n\npackage hooks\nimport \"example.com/extra\"\n",
	)

	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: "file", Target: rule.NewTarget("example.com/svc")},
		Path:         "example.com/filehooks",
		File:         sourceFile,
		ResolvedPath: dir,
	}
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddFileRule(fileRule)

	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	paths := sp.hookPackagePaths([]*rule.InstRuleSet{set})
	assert.ElementsMatch(t, []string{"example.com/filehooks", "example.com/extra"}, paths)

	t.Run("source import error is logged and skipped", func(t *testing.T) {
		badRule := &rule.InstFileRule{
			InstBaseRule: rule.InstBaseRule{Name: "bad-file", Target: rule.NewTarget("example.com/svc")},
			Path:         "example.com/filehooks",
			File:         "does_not_exist.go",
			ResolvedPath: dir,
		}
		badSet := rule.NewInstRuleSet("example.com/svc")
		badSet.AddFileRule(badRule)

		badPaths := sp.hookPackagePaths([]*rule.InstRuleSet{badSet})
		assert.ElementsMatch(t, []string{"example.com/filehooks"}, badPaths)
	})
}

func TestMatchInjectedDeps_AddFileSourceImports(t *testing.T) {
	t.Setenv("OTELC_RULES", "")

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/injecttest3\n\ngo 1.25.0\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "hooks.go"), "package hooks\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "added.go"),
		"//go:build ignore\n\npackage hooks\n\nimport _ \"example.com/injecttest3/injecteddep\"\n")
	mustWriteFile(t, filepath.Join(dir, "injecteddep", "injecteddep.go"),
		"package injecteddep\n\nfunc InjectedFn() {}\n")

	ruleFile := filepath.Join(dir, "rules.yaml")
	mustWriteFile(t, ruleFile, `injectedRule:
  target: example.com/injecttest3/injecteddep
  func: InjectedFn
  raw: "_ = 1"
`)

	sp := &setupPhase{logger: slog.New(slog.DiscardHandler), ruleConfig: ruleFile}
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: "add-file", Target: rule.NewTarget("example.com/svc")},
		Path:         "example.com/injecttest3/hooks",
		File:         "added.go",
		ResolvedPath: filepath.Join(dir, "hooks"),
	}
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddFileRule(fileRule)

	idx, err := sp.loadRuleIndex(t.Context(), nil)
	require.NoError(t, err)

	extra, err := sp.matchInjectedDeps(
		t.Context(), []*rule.InstRuleSet{set}, nil, idx, []string{"-C", dir},
	)
	require.NoError(t, err)
	require.Len(t, extra, 1)
	assert.Equal(t, "example.com/injecttest3/injecteddep", extra[0].ModulePath)
}

func TestMatchInjectedDeps_MatchError(t *testing.T) {
	t.Setenv("OTELC_RULES", "")

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/injecttest4\n\ngo 1.25.0\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "hooks.go"), "package hooks\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "added.go"),
		"//go:build ignore\n\npackage hooks\n\nimport _ \"example.com/injecttest4/injecteddep\"\n")
	mustWriteFile(t, filepath.Join(dir, "injecteddep", "injecteddep.go"),
		"package injecteddep\n\nfunc InjectedFn( // unclosed parenthesis syntax error\n")

	ruleFile := filepath.Join(dir, "rules.yaml")
	mustWriteFile(t, ruleFile, `injectedRule:
  target: example.com/injecttest4/injecteddep
  func: InjectedFn
  raw: "_ = 1"
`)

	sp := &setupPhase{logger: slog.New(slog.DiscardHandler), ruleConfig: ruleFile}
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: "add-file", Target: rule.NewTarget("example.com/svc")},
		Path:         "example.com/injecttest4/hooks",
		File:         "added.go",
		ResolvedPath: filepath.Join(dir, "hooks"),
	}
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddFileRule(fileRule)

	idx, err := sp.loadRuleIndex(t.Context(), nil)
	require.NoError(t, err)

	_, err = sp.matchInjectedDeps(
		t.Context(), []*rule.InstRuleSet{set}, nil, idx, []string{"-C", dir},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matching injected dependencies to hook rules")
}

func TestMatchInjectedDeps_ResolveRulePathsError(t *testing.T) {
	t.Setenv("OTELC_RULES", "")

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/injecttest5\n\ngo 1.25.0\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "hooks.go"), "package hooks\n")
	mustWriteFile(t, filepath.Join(dir, "hooks", "added.go"),
		"//go:build ignore\n\npackage hooks\n\nimport _ \"example.com/injecttest5/injecteddep\"\n")
	mustWriteFile(t, filepath.Join(dir, "injecteddep", "injecteddep.go"),
		"package injecteddep\n\nfunc InjectedFn() {}\n")

	ruleFile := filepath.Join(dir, "rules.yaml")
	mustWriteFile(t, ruleFile, `injectedRule:
  target: example.com/injecttest5/injecteddep
  func: InjectedFn
  path: example.com/unresolvable/nonexistent
  after: OtelAfter
`)

	sp := &setupPhase{logger: slog.New(slog.DiscardHandler), ruleConfig: ruleFile}
	fileRule := &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{Name: "add-file", Target: rule.NewTarget("example.com/svc")},
		Path:         "example.com/injecttest5/hooks",
		File:         "added.go",
		ResolvedPath: filepath.Join(dir, "hooks"),
	}
	set := rule.NewInstRuleSet("example.com/svc")
	set.AddFileRule(fileRule)

	idx, err := sp.loadRuleIndex(t.Context(), nil)
	require.NoError(t, err)

	_, err = sp.matchInjectedDeps(
		t.Context(), []*rule.InstRuleSet{set}, nil, idx, []string{"-C", dir},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving injected rule paths")
}
