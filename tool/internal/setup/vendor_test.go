// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// mustVendorMode returns the vendor mode of a build in workDir with args. The
// test fails if vendorMode returns an error.
func mustVendorMode(ctx context.Context, t *testing.T, workDir string, args []string) bool {
	t.Helper()
	vendored, err := vendorMode(ctx, workDir, subcmdBuild, args)
	require.NoError(t, err)
	return vendored
}

// writeVendoredGoMod writes go.mod with goDirective (no go directive when it
// is empty) and an empty vendor/modules.txt in dir.
func writeVendoredGoMod(t *testing.T, dir, goDirective string) {
	t.Helper()
	goMod := "module m\n"
	if goDirective != "" {
		goMod += "\ngo " + goDirective + "\n"
	}
	mustWriteFile(t, filepath.Join(dir, goModFileName), goMod)
	mustWriteFile(t, filepath.Join(dir, vendorDirName, modulesTxtFileName), "")
}

func TestVendoredModuleRoot(t *testing.T) {
	t.Setenv("GOWORK", "")
	writeVendoredModule := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		writeVendoredGoMod(t, root, "1.25.0")
		return root
	}

	t.Run("vendor present at module root", func(t *testing.T) {
		root := writeVendoredModule(t)
		assert.Equal(t, root, vendoredModuleRoot(t.Context(), root))
	})
	t.Run("vendor found from subdirectory", func(t *testing.T) {
		root := writeVendoredModule(t)
		sub := filepath.Join(root, "cmd")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		assert.Equal(t, root, vendoredModuleRoot(t.Context(), sub))
	})
	t.Run("module without vendor", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
		assert.Empty(t, vendoredModuleRoot(t.Context(), root))
	})
	t.Run("no module", func(t *testing.T) {
		assert.Empty(t, vendoredModuleRoot(t.Context(), t.TempDir()))
	})
	t.Run("vendor present but in a workspace", func(t *testing.T) {
		root := writeVendoredModule(t)
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "go.work"),
			[]byte("go 1.25.0\n\nuse .\n"),
			0o644,
		))
		// In workspace mode the go command uses the vendor directory of the
		// workspace, not of the module, so a leftover vendor/modules.txt in the
		// module must not make this report true.
		assert.Empty(t, vendoredModuleRoot(t.Context(), root))
	})
}

// TestVendorModeOldGoVersion: the go command uses vendor/ by default only
// when the go directive of go.mod is 1.14 or higher. otelc does the same, and
// it does not support an explicit -mod=vendor for a lower version.
func TestVendorModeOldGoVersion(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")

	for _, goDirective := range []string{"1.13", ""} {
		root := t.TempDir()
		writeVendoredGoMod(t, root, goDirective)

		assert.False(t, mustVendorMode(t.Context(), t, root, nil),
			"go %q without -mod: the go command ignores vendor/", goDirective)
		assert.False(t, mustVendorMode(t.Context(), t, root, []string{"-mod=mod"}), goDirective)

		for _, setFlag := range []func() []string{
			func() []string { return []string{"-mod=vendor", "."} },
			func() []string { t.Setenv("GOFLAGS", "-mod=vendor"); return []string{"."} },
		} {
			args := setFlag()
			_, err := vendorMode(t.Context(), root, subcmdBuild, args)
			require.Error(t, err, "go %q with -mod=vendor", goDirective)
			assert.Contains(t, err.Error(), "otelc needs go 1.14 or higher in "+filepath.Join(root, goModFileName))
			assert.Contains(t, err.Error(), `run "go mod edit -go=1.14 && go mod vendor"`)
			t.Setenv("GOFLAGS", "")
		}
	}

	root := t.TempDir()
	writeVendoredGoMod(t, root, "1.14")
	assert.True(t, mustVendorMode(t.Context(), t, root, nil), "go 1.14 uses vendor/ by default")
	assert.True(t, mustVendorMode(t.Context(), t, root, []string{"-mod=vendor"}))
}

// TestContextWithBuildModeDirFlag: the vendor mode is the one of the -C
// directory of the build, not the one of the otelc work directory.
func TestContextWithBuildModeDirFlag(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	parent := t.TempDir()
	vendored := filepath.Join(parent, "vendored")
	plain := filepath.Join(parent, "plain")
	writeVendoredGoMod(t, vendored, "1.25.0")
	mustWriteFile(t, filepath.Join(plain, goModFileName), "module plain\n\ngo 1.25.0\n")

	t.Chdir(vendored)
	t.Setenv(util.EnvOtelcWorkDir, vendored)
	ctx, err := contextWithBuildMode(t.Context(), subcmdBuild, []string{"-C", "../plain", "."})
	require.NoError(t, err)
	got, ok := vendorModeFromContext(ctx)
	require.True(t, ok)
	assert.False(t, got, "-C ../plain: the plain module does not vendor")

	ctx, err = contextWithBuildMode(t.Context(), subcmdBuild, []string{"."})
	require.NoError(t, err)
	got, _ = vendorModeFromContext(ctx)
	assert.True(t, got, "no -C: the vendored module")

	t.Chdir(plain)
	t.Setenv(util.EnvOtelcWorkDir, plain)
	ctx, err = contextWithBuildMode(t.Context(), subcmdBuild, []string{"-C=../vendored", "."})
	require.NoError(t, err)
	got, _ = vendorModeFromContext(ctx)
	assert.True(t, got, "-C=../vendored: the vendored module")

	// The go command accepts the flag with two dashes too.
	ctx, err = contextWithBuildMode(t.Context(), subcmdBuild, []string{"--C=../vendored", "."})
	require.NoError(t, err)
	got, _ = vendorModeFromContext(ctx)
	assert.True(t, got, "--C=../vendored: the vendored module")
	ctx, err = contextWithBuildMode(t.Context(), subcmdBuild, []string{"--C", "../vendored", "."})
	require.NoError(t, err)
	got, _ = vendorModeFromContext(ctx)
	assert.True(t, got, "--C ../vendored: the vendored module")

	ctx = contextWithVendorMode(t.Context(), false)
	same, err := contextWithBuildMode(ctx, subcmdBuild, []string{"-C=../vendored", "."})
	require.NoError(t, err)
	assert.Equal(t, ctx, same, "a mode in the context is kept")
}

// TestModBuildFlags: the -mod flag of the build (in args or in GOFLAGS) goes
// to the package loads that otelc does after it changed go.mod.
func TestModBuildFlags(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	root := t.TempDir()
	writeVendoredGoMod(t, root, "1.25.0")
	t.Chdir(root)
	t.Setenv(util.EnvOtelcWorkDir, root)

	assert.Nil(t, modBuildFlags(t.Context()), "no mode in the context")
	assert.Nil(t, modBuildFlags(contextWithVendorMode(t.Context(), true)))

	ctx, err := contextWithBuildMode(t.Context(), subcmdBuild, []string{"."})
	require.NoError(t, err)
	assert.Nil(t, modBuildFlags(ctx), "no -mod flag")

	ctx, err = contextWithBuildMode(t.Context(), subcmdBuild, []string{"-mod=mod", "."})
	require.NoError(t, err)
	vendored, _ := vendorModeFromContext(ctx)
	assert.False(t, vendored)
	assert.Equal(t, []string{"-mod=mod"}, modBuildFlags(ctx))

	ctx, err = contextWithBuildMode(t.Context(), subcmdBuild, []string{"-mod", "readonly", "."})
	require.NoError(t, err)
	assert.Equal(t, []string{"-mod=readonly"}, modBuildFlags(ctx))

	t.Setenv("GOFLAGS", "-mod=mod")
	ctx, err = contextWithBuildMode(t.Context(), subcmdBuild, []string{"."})
	require.NoError(t, err)
	assert.Equal(t, []string{"-mod=mod"}, modBuildFlags(ctx))
}

// inconsistentVendoredApp returns a vendored app whose go.mod requires
// example.com/extra, which vendor/modules.txt does not have. This is the
// state after syncDeps changed go.mod for a -mod=mod build: in vendor mode,
// the go command reports "inconsistent vendoring".
func inconsistentVendoredApp(t *testing.T) string {
	t.Helper()
	appDir := newVendorSyncApp(t)
	goMod := readAppFile(t, appDir, goModFileName)
	mustWriteFile(t, filepath.Join(appDir, goModFileName), goMod+"\nrequire example.com/extra v1.0.0\n")
	return appDir
}

// modModeContext returns the context of a -mod=mod build in the current
// directory.
func modModeContext(t *testing.T) context.Context {
	t.Helper()
	ctx, err := contextWithBuildMode(t.Context(), subcmdBuild, []string{"-mod=mod", "."})
	require.NoError(t, err)
	return ctx
}

func TestResolveRulePathsUsesModFlag(t *testing.T) {
	appDir := inconsistentVendoredApp(t)
	newMatched := func() []*rule.InstRuleSet {
		rs := rule.NewInstRuleSet("example.com/app")
		rs.AddFuncRule(filepath.Join(appDir, "main.go"), &rule.InstFuncRule{Path: "example.com/extra"})
		return []*rule.InstRuleSet{rs}
	}

	matched := newMatched()
	err := resolveRulePaths(contextWithVendorMode(t.Context(), false), matched, []string{appDir})
	require.Error(t, err, "without -mod=mod the load uses vendor/ and fails")

	matched = newMatched()
	require.NoError(t, resolveRulePaths(modModeContext(t), matched, []string{appDir}))
	got := matched[0].AllFuncRules()[0].ResolvedPath
	assert.NotEmpty(t, got)
	assert.False(t, isInVendorDir(got), "the package comes from the module cache: %s", got)
}

func TestResolveInstrumentationConfigsUsesModFlag(t *testing.T) {
	appDir := inconsistentVendoredApp(t)

	_, err := resolveInstrumentationConfigs(contextWithVendorMode(t.Context(), false), appDir,
		[]string{"example.com/extra"})
	require.ErrorContains(t, err, "inconsistent vendoring", "without -mod=mod the load uses vendor/ and fails")

	cfgs, err := resolveInstrumentationConfigs(modModeContext(t), appDir, []string{"example.com/extra"})
	require.NoError(t, err)
	require.Contains(t, cfgs, "example.com/extra")
	cfg := cfgs["example.com/extra"]
	require.NotNil(t, cfg)
	// example.com/extra has no rule file, so it is not an instrumentation
	// package. But the package load works: the module directory is known.
	assert.ErrorIs(t, cfg.Error, errNotInstrumentation)
}

func TestRuleSetImports(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.go")
	rs := rule.NewInstRuleSet("example.com/app")
	base := func(path string) rule.InstBaseRule {
		return rule.InstBaseRule{Imports: map[string]string{"x": path, "_": ""}}
	}
	rs.AddFuncRule(file, &rule.InstFuncRule{InstBaseRule: base("example.com/func/imp"), Path: "example.com/func"})
	rs.AddFileRule(&rule.InstFileRule{InstBaseRule: base("example.com/file/imp"), Path: "example.com/file"})
	rs.AddRawRule(file, &rule.InstRawRule{InstBaseRule: base("example.com/raw")})
	rs.AddStructRule(file, &rule.InstStructRule{InstBaseRule: base("example.com/struct")})
	rs.AddCallRule(file, &rule.InstCallRule{InstBaseRule: base("example.com/call")})
	rs.AddLitRule(file, &rule.InstLitRule{InstBaseRule: base("example.com/lit")})
	rs.AddDirectiveRule(file, &rule.InstDirectiveRule{InstBaseRule: base("example.com/directive")})
	rs.AddDeclRule(file, &rule.InstDeclRule{InstBaseRule: base("fmt")})

	assert.Equal(t, map[string]bool{
		"example.com/func": true, "example.com/func/imp": true,
		"example.com/file": true, "example.com/file/imp": true,
		"example.com/raw": true, "example.com/struct": true, "example.com/call": true,
		"example.com/lit": true, "example.com/directive": true, "fmt": true,
	}, (&setupPhase{}).ruleSetImports([]*rule.InstRuleSet{rs}))
	assert.Empty(t, (&setupPhase{}).ruleSetImports(nil))
}

// TestVendorSyncRuleSets: the packages of the matched rules reach vendorSync
// for each module, after go.mod and go.sum are tracked.
func TestVendorSyncRuleSets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(util.EnvOtelcWorkDir, dir)
	mustWriteFile(t, filepath.Join(dir, goModFileName), "module example.com/app\n\ngo 1.25.0\n")

	type call struct {
		imports   map[string]bool
		moduleDir string
		tracked   bool
	}
	var calls []call
	sm := newStateManager()
	orig := vendorSyncHook
	t.Cleanup(func() { vendorSyncHook = orig })
	vendorSyncHook = func(_ context.Context, imports map[string]bool, moduleDir string) error {
		_, tracked := sm.files[filepath.Join(dir, goModFileName)]
		calls = append(calls, call{imports: imports, moduleDir: moduleDir, tracked: tracked})
		return nil
	}

	ctx := contextWithStateManager(t.Context(), sm)
	require.NoError(t, (&setupPhase{}).vendorSyncRuleSets(ctx, nil, []string{dir}))
	assert.Empty(t, calls, "no rules: no vendorSync")

	rs := rule.NewInstRuleSet("example.com/app")
	rs.AddFuncRule(filepath.Join(dir, "main.go"), &rule.InstFuncRule{Path: "example.com/hook"})
	require.NoError(t, (&setupPhase{}).vendorSyncRuleSets(ctx, []*rule.InstRuleSet{rs}, []string{dir, dir}))
	require.Len(t, calls, 1, "one call for each module")
	assert.Equal(t, map[string]bool{"example.com/hook": true}, calls[0].imports)
	assert.Equal(t, dir, calls[0].moduleDir)
	assert.True(t, calls[0].tracked, "go.mod is tracked before vendorSync")

	require.Error(t, (&setupPhase{}).vendorSyncRuleSets(t.Context(), []*rule.InstRuleSet{rs}, []string{dir}),
		"no state manager")
}

// TestSetupVendorSyncsCustomRuleHooks: with custom rules (OTELC_RULES), the
// hook packages are only required in go.mod, not in vendor/. Setup adds them
// to vendor/ for the build.
func TestSetupVendorSyncsCustomRuleHooks(t *testing.T) {
	appDir := newVendorSyncApp(t)
	goMod := readAppFile(t, appDir, goModFileName)
	mustWriteFile(t, filepath.Join(appDir, goModFileName), goMod+"\nrequire example.com/extra v1.0.0\n")
	// "go mod vendor" lists a required module that no package imports, but
	// it copies no files of it.
	modulesTxt := readAppFile(t, appDir, filepath.Join(vendorDirName, modulesTxtFileName))
	mustWriteFile(t, filepath.Join(appDir, vendorDirName, modulesTxtFileName),
		modulesTxt+"# example.com/extra v1.0.0\n## explicit; go 1.21\n")
	rules := filepath.Join(t.TempDir(), "rules.yaml")
	mustWriteFile(t, rules, `add_extra:
  target: main
  func: main
  raw: "_ = 1"
  imports:
    _: example.com/extra
`)
	t.Setenv(util.EnvOtelcRules, rules)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	require.NoError(t, Setup(t.Context(), newParsedCommand(t, "setup", ".")))

	assert.Contains(t, readAppFile(t, appDir, filepath.Join(vendorDirName, modulesTxtFileName)),
		"# example.com/extra v1.0.0\n## explicit; go 1.21\nexample.com/extra\n")
	assert.FileExists(t, filepath.Join(appDir, vendorDirName, "example.com", "extra", "extra.go"))
}

// TestSetupVendorSyncsCustomRuleFileImports: with custom rules in vendor mode,
// an add_file source file (a //go:build ignore file, which "go mod vendor"
// does not see) can import a package that is not in vendor/. Setup must add
// this package to vendor/ after it resolves the path of the rule, because
// only then can it read the imports of the file.
func TestSetupVendorSyncsCustomRuleFileImports(t *testing.T) {
	appDir := newVendorSyncApp(t)
	goMod := readAppFile(t, appDir, goModFileName)
	mustWriteFile(t, filepath.Join(appDir, goModFileName), goMod+"\nrequire example.com/extra v1.0.0\n")
	modulesTxt := readAppFile(t, appDir, filepath.Join(vendorDirName, modulesTxtFileName))
	mustWriteFile(t, filepath.Join(appDir, vendorDirName, modulesTxtFileName),
		modulesTxt+"# example.com/extra v1.0.0\n## explicit; go 1.21\n")
	mustWriteFile(t, filepath.Join(appDir, "hooks", "doc.go"), "package hooks\n")
	mustWriteFile(t, filepath.Join(appDir, "hooks", "added.go"),
		"//go:build ignore\n\npackage hooks\n\nimport _ \"example.com/extra\"\n")
	rules := filepath.Join(t.TempDir(), "rules.yaml")
	mustWriteFile(t, rules, `add_hooks_file:
  target: main
  do:
    - add_file:
        file: added.go
        path: example.com/app/hooks
`)
	t.Setenv(util.EnvOtelcRules, rules)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	require.NoError(t, Setup(t.Context(), newParsedCommand(t, "setup", ".")))

	assert.Contains(t, readAppFile(t, appDir, filepath.Join(vendorDirName, modulesTxtFileName)),
		"# example.com/extra v1.0.0\n## explicit; go 1.21\nexample.com/extra\n")
	assert.FileExists(t, filepath.Join(appDir, vendorDirName, "example.com", "extra", "extra.go"))
}

// TestSetupVendorSyncsCustomRuleHookChain: with custom rules in vendor mode,
// a hook package can bring a new hook package into the build. Rule A matches
// the app and uses hook chaina. Rule B matches chaina and uses hook chainb.
// Rule C matches chainleaf, which only chainb imports. Setup must add each
// new hook package to vendor/ before it loads it, else rule C does not match.
func TestSetupVendorSyncsCustomRuleHookChain(t *testing.T) {
	appDir := newVendorSyncApp(t)
	mods := append(testProxyModules(),
		proxyModule{path: "example.com/chaina", version: "v1.0.0", files: map[string]string{
			goModFileName: "module example.com/chaina\n\ngo 1.21\n",
			"chaina.go":   "package chaina\n\nfunc A() {}\n",
		}},
		proxyModule{path: "example.com/chainb", version: "v1.0.0", files: map[string]string{
			goModFileName: "module example.com/chainb\n\ngo 1.21\n\nrequire example.com/chainleaf v1.0.0\n",
			"chainb.go":   "package chainb\n\nimport _ \"example.com/chainleaf\"\n",
		}},
		proxyModule{path: "example.com/chainleaf", version: "v1.0.0", files: map[string]string{
			goModFileName: "module example.com/chainleaf\n\ngo 1.21\n",
			"leaf.go":     "package chainleaf\n\nfunc Leaf() {}\n",
		}},
	)
	t.Setenv("GOPROXY", newTestProxy(t, mods...))

	mustWriteFile(t, filepath.Join(appDir, goModFileName), readAppFile(t, appDir, goModFileName)+
		"\nrequire (\n\texample.com/chaina v1.0.0\n\texample.com/chainb v1.0.0\n\texample.com/chainleaf v1.0.0\n)\n")
	modulesTxt := readAppFile(t, appDir, filepath.Join(vendorDirName, modulesTxtFileName))
	mustWriteFile(t, filepath.Join(appDir, vendorDirName, modulesTxtFileName), modulesTxt+
		"# example.com/chaina v1.0.0\n## explicit; go 1.21\n"+
		"# example.com/chainb v1.0.0\n## explicit; go 1.21\n"+
		"# example.com/chainleaf v1.0.0\n## explicit; go 1.21\n")
	rules := filepath.Join(t.TempDir(), "rules.yaml")
	mustWriteFile(t, rules, `rule_a:
  target: main
  func: main
  before: BeforeMain
  path: example.com/chaina
rule_b:
  target: example.com/chaina
  func: A
  before: BeforeA
  path: example.com/chainb
rule_c:
  target: example.com/chainleaf
  func: Leaf
  before: BeforeLeaf
  path: example.com/chaina
`)
	t.Setenv(util.EnvOtelcRules, rules)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	require.NoError(t, Setup(t.Context(), newParsedCommand(t, "setup", ".")))

	assert.FileExists(t, filepath.Join(appDir, vendorDirName, "example.com", "chainb", "chainb.go"))
	matched, err := os.ReadFile(util.GetMatchedRuleFile())
	require.NoError(t, err)
	assert.Contains(t, string(matched), "rule_b")
	assert.Contains(t, string(matched), "rule_c", "the rule on the dependency of the last hook matches")
}

func TestEnvWithModFlag(t *testing.T) {
	t.Setenv("GOFLAGS", "-trimpath '-mod=vendor'")
	env := []string{"A=1"}
	assert.Equal(t, env, envWithModFlag(env, ""), "no -mod flag: no change")
	assert.Equal(t, []string{"A=1", "GOFLAGS=-trimpath -mod=mod"}, envWithModFlag(env, "mod"))

	t.Setenv("GOFLAGS", "")
	assert.Equal(t, []string{"A=1", "GOFLAGS=-mod=readonly"}, envWithModFlag(env, "readonly"))
}

// TestVendorModeErrors: a directory that does not exist is not vendored. A
// go.mod that otelc cannot read, and a -mod=vendor build of a go.mod that is
// too old, are errors.
func TestVendorModeErrors(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")

	assert.Empty(t, vendoredModuleRoot(t.Context(), filepath.Join(t.TempDir(), "absent")),
		"the go command cannot run in a directory that does not exist")

	_, err := vendorGoVersionSupported(t.TempDir())
	require.Error(t, err, "no go.mod")

	root := t.TempDir()
	writeVendoredGoMod(t, root, "1.13")
	_, err = contextWithBuildMode(t.Context(), subcmdBuild, []string{"-C", root, "-mod=vendor"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "otelc needs go 1.14 or higher")
}

// TestBuildWorkDirDefaultSubcommand: without a subcommand, the arguments are
// the ones of go build. Without -C, the directory is the otelc work
// directory.
func TestBuildWorkDirDefaultSubcommand(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	dir, err := buildWorkDir("", nil)
	require.NoError(t, err)
	assert.Equal(t, util.GetOtelcWorkDir(), dir)

	other := t.TempDir()
	dir, err = buildWorkDir("", []string{"-C", other, "."})
	require.NoError(t, err)
	assert.Equal(t, other, dir)
}

// TestVendorSyncRuleSetsErrors: each failure stops vendorSyncRuleSets with
// an error.
func TestVendorSyncRuleSetsErrors(t *testing.T) {
	newModule := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		t.Chdir(dir)
		t.Setenv(util.EnvOtelcWorkDir, dir)
		mustWriteFile(t, filepath.Join(dir, goModFileName), "module example.com/app\n\ngo 1.25.0\n")
		return dir
	}
	rs := rule.NewInstRuleSet("example.com/app")
	rs.AddFuncRule(filepath.Join(t.TempDir(), "main.go"), &rule.InstFuncRule{Path: "example.com/hook"})
	matched := []*rule.InstRuleSet{rs}

	t.Run("vendorSync fails", func(t *testing.T) {
		dir := newModule(t)
		orig := vendorSyncHook
		t.Cleanup(func() { vendorSyncHook = orig })
		vendorSyncHook = func(context.Context, map[string]bool, string) error {
			return os.ErrPermission
		}
		ctx := contextWithStateManager(t.Context(), newStateManager())
		require.ErrorIs(t, (&setupPhase{}).vendorSyncRuleSets(ctx, matched, []string{dir}), os.ErrPermission)
	})

	t.Run("the go command is not found", func(t *testing.T) {
		dir := newModule(t)
		t.Setenv("PATH", "")
		ctx := contextWithStateManager(t.Context(), newStateManager())
		err := (&setupPhase{}).vendorSyncRuleSets(ctx, matched, []string{dir})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GOWORK")
	})

	t.Run("the state cannot be saved", func(t *testing.T) {
		skipWithoutPermissions(t)
		dir := newModule(t)
		require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
		makeReadOnly(t, util.GetBuildTempDir())
		called := false
		orig := vendorSyncHook
		t.Cleanup(func() { vendorSyncHook = orig })
		vendorSyncHook = func(context.Context, map[string]bool, string) error {
			called = true
			return nil
		}
		ctx := contextWithStateManager(t.Context(), newStateManager())
		err := (&setupPhase{}).vendorSyncRuleSets(ctx, matched, []string{dir})
		require.ErrorIs(t, err, os.ErrPermission)
		// The error comes from the snapshot in the state directory, not
		// from an earlier step (for example the go env call).
		assert.Contains(t, err.Error(), util.GetBuildTemp(stateDir))
		assert.False(t, called, "vendorSync does not run when the state is not saved")
	})
}
