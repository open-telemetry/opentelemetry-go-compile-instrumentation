// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	modzip "golang.org/x/mod/zip"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestFindVendorConflicts(t *testing.T) {
	versions := map[string]string{"example.com/dep": "v1.0.0", "example.com/userlib": "v1.0.0"}
	fixed := map[string]bool{"example.com/dep": true, "example.com/userlib": true}
	hook := util.OtelcInstRoot + "/example"

	tests := []struct {
		name           string
		graph          map[string]map[string]string
		pkgs           []listedPackage
		fixed          map[string]bool
		wantRequired   string
		wantRequiredBy string
	}{
		{
			name:  "otelc module that imports the module needs a higher version",
			graph: map[string]map[string]string{hook + "@v0.0.0": {"example.com/dep": "v1.2.0"}},
			pkgs: []listedPackage{
				{importPath: hook, module: hook + "@v0.0.0", imports: []string{"example.com/dep"}},
				{importPath: "example.com/dep", module: "example.com/dep@v1.2.0"},
			},
			wantRequired:   "v1.2.0",
			wantRequiredBy: hook,
		},
		{
			name:  "other module (a dependency of the hook code) needs a higher version",
			graph: map[string]map[string]string{"example.com/sdk@v1.0.0": {"example.com/dep": "v1.2.0"}},
			pkgs: []listedPackage{
				{importPath: "example.com/sdk", module: "example.com/sdk@v1.0.0", imports: []string{"example.com/dep"}},
				{importPath: "example.com/dep", module: "example.com/dep@v1.2.0"},
			},
			wantRequired:   "v1.2.0",
			wantRequiredBy: "example.com/sdk",
		},
		{
			name: "requirement without import is ignored",
			graph: map[string]map[string]string{
				hook + "@v0.0.0": {"example.com/dep": "v1.2.0"},
			},
			pkgs: []listedPackage{
				{importPath: hook, module: hook + "@v0.0.0", imports: []string{"fmt"}},
				{importPath: "example.com/dep", module: "example.com/dep@v1.2.0"},
			},
		},
		{
			name:  "vendored module of the user as requiring module is ignored",
			graph: map[string]map[string]string{"example.com/userlib@v1.0.0": {"example.com/dep": "v1.2.0"}},
			pkgs: []listedPackage{
				{
					importPath: "example.com/userlib", module: "example.com/userlib@v1.0.0",
					imports: []string{"example.com/dep"},
				},
				{importPath: "example.com/dep", module: "example.com/dep@v1.2.0"},
			},
		},
		{
			name:  "updated vendored module as requiring module counts",
			graph: map[string]map[string]string{"example.com/userlib@v1.0.0": {"example.com/dep": "v1.2.0"}},
			pkgs: []listedPackage{
				{
					importPath: "example.com/userlib", module: "example.com/userlib@v1.0.0",
					imports: []string{"example.com/dep"},
				},
				{importPath: "example.com/dep", module: "example.com/dep@v1.2.0"},
			},
			fixed:          map[string]bool{"example.com/dep": true},
			wantRequired:   "v1.2.0",
			wantRequiredBy: "example.com/userlib",
		},
		{
			name:  "same version is not a conflict",
			graph: map[string]map[string]string{hook + "@v0.0.0": {"example.com/dep": "v1.0.0"}},
			pkgs: []listedPackage{
				{importPath: hook, module: hook + "@v0.0.0", imports: []string{"example.com/dep"}},
				{importPath: "example.com/dep", module: "example.com/dep@v1.0.0"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fixed
			if tt.fixed != nil {
				f = tt.fixed
			}
			conflicts := findVendorConflicts(versions, f, tt.graph, tt.pkgs)
			if tt.wantRequired == "" {
				assert.Empty(t, conflicts)
				return
			}
			require.Len(t, conflicts, 1)
			conflict := conflicts[0]
			assert.Equal(t, "example.com/dep", conflict.Module)
			assert.Equal(t, "v1.0.0", conflict.VendoredVersion)
			assert.Equal(t, tt.wantRequired, conflict.RequiredVersion)
			assert.Equal(t, tt.wantRequiredBy, conflict.RequiredBy)

			// A build fails with this error. It tells the fix.
			err := conflictsError(conflicts)
			var target *vendorConflictError
			require.ErrorAs(t, err, &target)
			assert.Contains(t, err.Error(), tt.wantRequiredBy)
			assert.Contains(t, err.Error(), `go get example.com/dep@v1.2.0`)
			assert.Contains(t, err.Error(), `otelc vendor`)
		})
	}
}

func TestConflictsErrorOnePerModule(t *testing.T) {
	err := conflictsError([]*vendorConflictError{
		{Module: "example.com/a", VendoredVersion: "v1.0.0", RequiredVersion: "v1.3.0", RequiredBy: "x"},
		{Module: "example.com/a", VendoredVersion: "v1.0.0", RequiredVersion: "v1.2.0", RequiredBy: "y"},
		{Module: "example.com/b", VendoredVersion: "v0.1.0", RequiredVersion: "v0.2.0", RequiredBy: "z"},
	})
	msg := err.Error()
	assert.Contains(t, msg, "example.com/a at version v1.3.0")
	assert.NotContains(t, msg, "v1.2.0", "only the highest required version of a module is reported")
	assert.Contains(t, msg, "example.com/b at version v0.2.0")
}

func TestVendorScratchUpdate(t *testing.T) {
	user := mustParseModulesTxt(t, `# example.com/dep v1.0.0
## explicit
example.com/dep
example.com/dep/sub
# example.com/replaced v1.0.0 => ../replaced
## explicit
example.com/replaced
`)
	s := &vendorScratch{
		versions: map[string]string{"example.com/dep": "v1.0.0", "example.com/replaced": "v1.0.0"},
		userMods: map[string]bool{"example.com/dep": true, "example.com/replaced": true},
		pinned:   map[string]bool{"example.com/dep": true},
		updated:  map[string]bool{},
		extra:    map[string]bool{},
	}
	require.NoError(t, s.update([]*vendorConflictError{
		{Module: "example.com/dep", VendoredVersion: "v1.0.0", RequiredVersion: "v1.2.0"},
	}, user))
	assert.Equal(t, "v1.2.0", s.versions["example.com/dep"])
	assert.True(t, s.updated["example.com/dep"])
	assert.False(t, s.pinned["example.com/dep"], "an updated module is not pinned")
	assert.Equal(t, map[string]bool{"example.com/dep": true, "example.com/dep/sub": true}, s.extra,
		"the vendored packages of an updated module stay in vendor/")
	assert.Equal(t, map[string]bool{"example.com/replaced": true}, s.fixedMods())

	err := s.update([]*vendorConflictError{
		{Module: "example.com/replaced", VendoredVersion: "v1.0.0", RequiredVersion: "v1.1.0"},
	}, user)
	var conflict *vendorConflictError
	require.ErrorAs(t, err, &conflict, "a module that go.mod replaces is not updated")
	assert.Contains(t, err.Error(), "replaces it")
}

func TestParseScratchOutputs(t *testing.T) {
	graph := parseModGraph([]byte("main example.com/a@v1.0.0\nexample.com/a@v1.0.0 example.com/b@v1.2.0\n"))
	assert.Equal(t, map[string]map[string]string{
		"main":                 {"example.com/a": "v1.0.0"},
		"example.com/a@v1.0.0": {"example.com/b": "v1.2.0"},
	}, graph)

	pkgs := parseListedPackages([]byte("example.com/a\texample.com/a@v1.0.0\tfmt example.com/b\t\n" +
		"example.com/c\t\t\terror\nmalformed line\n"))
	assert.Equal(t, []listedPackage{
		{importPath: "example.com/a", module: "example.com/a@v1.0.0", imports: []string{"fmt", "example.com/b"}},
		{importPath: "example.com/c", imports: []string{}, failed: true},
	}, pkgs)
}

func TestCopiedModulesWithMissingPackages(t *testing.T) {
	out := []byte(`go: finding module for package golang.org/x/sys/windows/registry
go: example imports
	golang.org/x/sys/windows/registry: module golang.org/x/sys@latest found (v0.49.0, replaced by ./deps/0027), ` +
		`but does not contain package golang.org/x/sys/windows/registry
	google.golang.org/protobuf/types/known/timestamppb: no required module provides package ` +
		`google.golang.org/protobuf/types/known/timestamppb; to add it:
`)
	failed := missingPackagesInOutput(out)
	assert.Equal(t, []string{
		"golang.org/x/sys/windows/registry",
		"google.golang.org/protobuf/types/known/timestamppb",
	}, failed)

	copies := map[string]string{
		"golang.org/x/sys":           "./deps/0001",
		"google.golang.org/protobuf": "./deps/0002",
		"example.com/m":              "./deps/0003",
		"example.com/m/nested":       "./deps/0004",
	}
	failed = append(failed, "example.com/m/nested/pkg", "example.com/unknown")
	assert.Equal(t, []string{"example.com/m/nested", "golang.org/x/sys", "google.golang.org/protobuf"},
		copiedModulesWithMissingPackages(failed, copies, nil),
		"the owner is the longest module path, and unknown packages are ignored")
	assert.Equal(t, []string{"example.com/m/nested", "golang.org/x/sys"},
		copiedModulesWithMissingPackages(failed, copies, map[string]bool{"google.golang.org/protobuf": true}),
		"a pinned module is not reported again")
}

func TestAtVendoredVersions(t *testing.T) {
	user := mustParseModulesTxt(t, "# golang.org/x/sys v0.41.0\n## explicit; go 1.24\ngolang.org/x/sys/unix\n")
	injected := mustParseModulesTxt(t, `# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
# golang.org/x/sys v0.47.0 => golang.org/x/sys v0.41.0
## explicit; go 1.24
golang.org/x/sys/unix
golang.org/x/sys/windows/registry
# golang.org/x/sys => golang.org/x/sys v0.41.0
`)
	got := atVendoredVersions(injected, user, map[string]bool{"golang.org/x/sys": true})

	merged, added, err := mergeVendorModules(user, got)
	require.NoError(t, err)
	assert.Equal(t, []string{"go.opentelemetry.io/otel", "golang.org/x/sys/windows/registry"}, added)
	assert.Contains(t, string(merged.Format()),
		"# golang.org/x/sys v0.41.0\n## explicit; go 1.24\ngolang.org/x/sys/unix\ngolang.org/x/sys/windows/registry\n")
	assert.NotContains(t, string(merged.Format()), "=> golang.org/x/sys")
	// The input does not change.
	v, _ := injected.Module("golang.org/x/sys")
	assert.Equal(t, "v0.47.0", v)
}

func TestRelativeReplacePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "app")
	assert.Equal(t, "./.otelc-build/pkg", relativeReplacePath(root, filepath.Join(root, ".otelc-build", "pkg")))
	assert.Equal(t, "../.otelc-build/pkg",
		relativeReplacePath(root, filepath.Join(filepath.Dir(root), ".otelc-build", "pkg")))
}

func TestUpdateGoModForVendor(t *testing.T) {
	moduleDir := t.TempDir()
	buildDir := filepath.Join(moduleDir, ".otelc-build")
	mf, err := modfile.Parse("go.mod", []byte(`module example.com/app

go 1.25.0

require example.com/dep v1.0.0

replace example.com/userreplaced => ../somewhere
`), nil)
	require.NoError(t, err)

	user := mustParseModulesTxt(t, "# example.com/dep v1.0.0\n## explicit; go 1.22\nexample.com/dep\n")
	merged := mustParseModulesTxt(t, `# example.com/dep v1.0.0
## explicit; go 1.22
example.com/dep
# example.com/userreplaced v1.1.0 => /abs/elsewhere
## explicit; go 1.22
example.com/userreplaced
# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
# go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000 => `+filepath.Join(buildDir, "pkg")+`
## explicit; go 1.26
go.opentelemetry.io/otelc/pkg/hook
`)

	require.NoError(t, updateGoModForVendor(mf, user, merged, moduleDir))

	data, err := mf.Format()
	require.NoError(t, err)
	gomod := string(data)
	assert.Contains(t, gomod, "go 1.26", "the go version is the highest of the added modules")
	assert.Contains(t, gomod, "go.opentelemetry.io/otel v1.46.0 // indirect")
	assert.Contains(t, gomod, "go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000 // indirect")
	assert.Contains(t, gomod,
		"replace go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000 => ./.otelc-build/pkg")
	assert.NotContains(t, gomod, "/abs/elsewhere", "a replace of the user is not changed")

	out := string(merged.Format())
	assert.Contains(t, out,
		"# go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000 => ./.otelc-build/pkg\n",
		"modules.txt has the same replacement text as go.mod")
	assert.Contains(t, out, "# example.com/userreplaced v1.1.0 => ../somewhere\n",
		"modules.txt has the replacement of the user")
}

func TestPlanVendorCopies(t *testing.T) {
	scratch := t.TempDir()
	userVendor := filepath.Join(t.TempDir(), vendorDirName)
	mustWriteFile(t, filepath.Join(scratch, "example.com", "m", "LICENSE"), "license")
	mustWriteFile(t, filepath.Join(scratch, "example.com", "m", "sub", "a.go"), "package sub")
	mustWriteFile(t, filepath.Join(scratch, "example.com", "m", "sub", "deep", "b.go"), "package deep")
	mustWriteFile(t, filepath.Join(scratch, "example.com", "m", "sub", "keep.go"), "package sub // new")
	// The user already has this file: it is never replaced.
	mustWriteFile(t, filepath.Join(userVendor, "example.com", "m", "sub", "keep.go"), "package sub // user")

	merged := mustParseModulesTxt(t, "# example.com/m v1.0.0\n## explicit\nexample.com/m/sub\nexample.com/m/sub/deep\n")
	copies, dirs, err := planVendorCopies(merged, []string{"example.com/m/sub", "example.com/m/sub/deep"},
		vendorCopyRoots{scratchVendor: scratch, moduleDir: filepath.Dir(userVendor)})
	require.NoError(t, err)

	dsts := make([]string, 0, len(copies))
	for _, c := range copies {
		rel, relErr := filepath.Rel(userVendor, c.dst)
		require.NoError(t, relErr)
		dsts = append(dsts, filepath.ToSlash(rel))
	}
	assert.Equal(t, []string{"example.com/m/LICENSE", "example.com/m/sub/a.go", "example.com/m/sub/deep/b.go"}, dsts)
	assert.Equal(t, []string{filepath.Join(userVendor, "example.com", "m", "sub", "deep")}, dirs,
		"only the directories that do not exist yet are made")
}

func TestVendorModeFlags(t *testing.T) {
	t.Setenv("GOFLAGS", "-trimpath -mod=readonly")
	assert.Equal(t, "readonly", lastModFlag("build", nil))
	assert.Equal(t, "vendor", lastModFlag("build", []string{"-mod=vendor", "."}), "a CLI flag wins over GOFLAGS")
	assert.Equal(t, "mod", lastModFlag("build", []string{"-mod", "mod", "."}))

	t.Setenv("GOFLAGS", "")
	assert.Empty(t, lastModFlag("build", []string{"."}))

	assert.Equal(t, "-trimpath -tags='a b'",
		goflagsWithoutMod("-mod=vendor -trimpath '-mod=readonly' -tags='a b' --mod=mod"))

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
	mustWriteFile(t, filepath.Join(root, "vendor", "modules.txt"), "")
	t.Setenv("GOWORK", "off")
	assert.True(t, mustVendorMode(t.Context(), t, root, nil))
	assert.False(t, mustVendorMode(t.Context(), t, root, []string{"-mod=mod"}))
	assert.False(t, mustVendorMode(contextWithVendorMode(t.Context(), false), t, root, nil),
		"the mode in the context wins")
}

func TestFillVendoredVersions(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "vendor", "modules.txt"),
		"# example.com/m v1.2.3\n## explicit\nexample.com/m/pkg\n")
	src := filepath.Join(root, "vendor", "example.com", "m", "pkg", "a.go")
	deps := []*Dependency{
		{ImportPath: "example.com/m/pkg", Sources: []string{src}},
		{ImportPath: "example.com/other", Sources: []string{filepath.Join(root, "other", "a.go")}},
		{ImportPath: "example.com/cached", Version: "v9.0.0", Sources: []string{src}},
	}
	fillVendoredVersions(t.Context(), deps)
	assert.Equal(t, "v1.2.3", deps[0].Version)
	assert.Empty(t, deps[1].Version)
	assert.Equal(t, "v9.0.0", deps[2].Version, "a known version is kept")
}

func TestCollectRulePackages(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	initDir := filepath.Join(workDir, ".otelc-build", unzippedInstDir, "init")
	mustWriteFile(t, filepath.Join(initDir, "init.go"),
		"//go:build ignore\n\npackage main\n\nimport _ \""+util.OtelcPkgRoot+"/runtime\"\n")
	ruleFile := filepath.Join(workDir, "rules.yaml")
	// Each rule type can have imports. Only the function rules and the file
	// rules name a hook package.
	mustWriteFile(t, ruleFile, `version: "v1.0.0"
hook_rule:
  target: example.com/lib
  where:
    func: Run
  do:
    - inject_hooks:
        before: Before
        path: "example.com/hooks"
  imports:
    hookimp: "example.com/hookimp"
raw_rule:
  target: example.com/lib
  where:
    func: Run
  do:
    - inject_code:
        raw: "_ = extra.V"
  imports:
    ctx: "context"
    extra: "example.com/extra"
file_rule:
  target: main
  do:
    - add_file:
        file: init.go
        path: "`+util.OtelcInstRoot+`/init"
struct_rule:
  target: example.com/lib
  struct: Server
  new_field:
    - name: Span
      type: trace.Span
  imports:
    trace: "example.com/structimp"
call_rule:
  target: example.com/lib
  function_call: net/http.Get
  replace: "callimp.Get({{ . }})"
  imports:
    callimp: "example.com/callimp"
lit_rule:
  target: example.com/lib
  struct_literal: example.com/lib.Client
  field:
    - name: Transport
      value: litimp.T
  imports:
    litimp: "example.com/litimp"
directive_rule:
  target: example.com/lib
  directive: otelc:span
  template: "_ = dirimp.V"
  imports:
    dirimp: "example.com/dirimp"
decl_rule:
  target: example.com/lib
  identifier: DefaultClient
  replace: declimp.Client
  imports:
    declimp: "example.com/declimp"
`)
	set := map[string]bool{}
	require.NoError(t, collectRulePackages(ruleFile, set, &hookSources{user: &modulesTxt{}}))
	assert.Equal(t, map[string]bool{
		"example.com/hooks":            true,
		"example.com/hookimp":          true,
		"context":                      true,
		"example.com/extra":            true,
		util.OtelcInstRoot + "/init":   true,
		util.OtelcPkgRoot + "/runtime": true,
		"example.com/structimp":        true,
		"example.com/callimp":          true,
		"example.com/litimp":           true,
		"example.com/dirimp":           true,
		"example.com/declimp":          true,
	}, set)

	// The typed loader rejects a rule that the instrumentation cannot use.
	mustWriteFile(t, ruleFile, "bad_rule:\n  target: example.com/lib\n  imports:\n    x: example.com/x\n")
	err := collectRulePackages(ruleFile, map[string]bool{}, &hookSources{user: &modulesTxt{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no recognised selector")
	mustWriteFile(t, ruleFile, "a: [")
	require.Error(t, collectRulePackages(ruleFile, map[string]bool{}, &hookSources{user: &modulesTxt{}}))
	require.Error(t, collectRulePackages(filepath.Join(workDir, "absent.yaml"), map[string]bool{},
		&hookSources{user: &modulesTxt{}}))
}

func TestEmbeddedSourceDir(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	instDir := filepath.Join(workDir, ".otelc-build", unzippedInstDir, "net", "http")
	require.NoError(t, os.MkdirAll(instDir, 0o755))

	dir, ok := embeddedSourceDir(util.OtelcInstRoot + "/net/http")
	assert.True(t, ok)
	assert.Equal(t, instDir, dir)
	_, ok = embeddedSourceDir(util.OtelcInstRoot + "/absent")
	assert.False(t, ok)
	_, ok = embeddedSourceDir("example.com/x")
	assert.False(t, ok)
	_, ok = embeddedSourceDir(util.OtelcInstRoot + "/net/../net/http")
	assert.False(t, ok, "an import path with .. is not valid")

	assert.True(t, isInVendorDir(filepath.Join(workDir, "vendor", "example.com", "x")))
	assert.False(t, isInVendorDir(instDir))
}

// TestVendorSyncNothingToAdd checks that vendorSync changes nothing when
// vendor/ has all the packages of the hook code already (for example after
// `otelc vendor`). Then it needs no network and no module cache.
func TestVendorSyncNothingToAdd(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, extractOtelcBundle())

	imports := map[string]bool{util.OtelcInstRoot + "/net/http/server": true}
	scratchImports, err := vendorScratchImports(imports, &hookSources{moduleDir: workDir, user: &modulesTxt{}})
	require.NoError(t, err)
	needed := scratchImports.packages
	require.Contains(t, needed, util.OtelcInstRoot+"/net/http/server")

	// The otelc packages are in an otelc module with the fingerprint of this
	// bundle. The other packages are each in their own module.
	var modulesTxt strings.Builder
	modulesTxt.WriteString("# " + util.OtelcRoot + " v0.0.0-00010101000000-000000000000\n" +
		"## explicit; " + annotationBundlePrefix + bundleFingerprint() + "\n")
	var others []string
	for _, p := range needed {
		if isOtelcModule(p) {
			modulesTxt.WriteString(p + "\n")
		} else {
			others = append(others, p)
		}
	}
	for _, p := range others {
		modulesTxt.WriteString("# " + p + " v1.0.0\n## explicit\n" + p + "\n")
	}
	goMod := "module example.com/app\n\ngo 1.25.0\n"
	mustWriteFile(t, filepath.Join(workDir, "go.mod"), goMod)
	mustWriteFile(t, filepath.Join(workDir, "vendor", "modules.txt"), modulesTxt.String())

	require.NoError(t, vendorSync(t.Context(), imports, workDir))

	data, err := os.ReadFile(filepath.Join(workDir, "go.mod"))
	require.NoError(t, err)
	assert.Equal(t, goMod, string(data))
	data, err = os.ReadFile(filepath.Join(workDir, "vendor", "modules.txt"))
	require.NoError(t, err)
	assert.Equal(t, modulesTxt.String(), string(data))
	assert.NoDirExists(t, util.GetBuildTemp(vendorScratchDir), "no scratch module is made")
}

func TestStaleOtelcModules(t *testing.T) {
	fp := bundleFingerprint()
	pkgMod := util.OtelcPkgRoot
	user := mustParseModulesTxt(t, "# "+pkgMod+" v0.0.0-00010101000000-000000000000 => ./.otelc-build/pkg\n"+
		"## explicit; go 1.25; "+annotationBundlePrefix+"0123456789abcdef\n"+
		pkgMod+"/hook\n"+
		"# "+util.OtelcInstRoot+" v0.0.0-00010101000000-000000000000 => ./.otelc-build/instrumentation\n"+
		"## explicit; go 1.25; "+annotationBundlePrefix+fp+"\n"+
		util.OtelcInstRoot+"/shared\n"+
		"# example.com/dep v1.0.0\n## explicit\nexample.com/dep\n")
	needed := []string{pkgMod + "/hook", util.OtelcInstRoot + "/shared", "example.com/dep"}

	assert.Equal(t, map[string]bool{pkgMod: true}, staleOtelcModules(user, needed, nil, fp),
		"an otelc module from a different bundle is stale")
	assert.Equal(t, map[string]bool{pkgMod: true}, staleOtelcModules(user, needed[1:], nil, fp),
		"an otelc module that otelc added from a different bundle is stale, also when the hook code "+
			"uses it only through its own imports (for example pkg/runtime)")

	userVendored := mustParseModulesTxt(t, "# "+pkgMod+" v0.1.0\n## explicit; go 1.25\n"+pkgMod+"/hook\n")
	assert.Empty(t, staleOtelcModules(userVendored, []string{"example.com/dep"}, nil, fp),
		"an otelc module of the user that the hook code does not need is kept")
	assert.Equal(t, map[string]bool{pkgMod: true}, staleOtelcModules(userVendored, []string{pkgMod + "/hook"}, nil, fp),
		"an otelc module of the user that the hook code needs is replaced by the bundle")

	// pkg/runtime of the user has no annotation, and the hook code uses it
	// only through the imports of an instrumentation package. It is a module
	// of the bundle, so it is replaced.
	runtimeMod := util.OtelcPkgRoot + "/runtime"
	withRuntime := mustParseModulesTxt(t, "# "+runtimeMod+" v0.1.0\n## explicit; go 1.25\n"+runtimeMod+"\n"+
		"# "+util.OtelcRoot+" v0.1.0\n## explicit; go 1.25\n"+util.OtelcRoot+"/tool/cmd/otelc\n")
	replaces := map[string]string{runtimeMod: "/bundle/pkg/runtime"}
	assert.Equal(t, map[string]bool{runtimeMod: true},
		staleOtelcModules(withRuntime, []string{util.OtelcInstRoot + "/net/http/server"}, replaces, fp),
		"a module of the bundle without the fingerprint is replaced; the otelc tool module of the user stays")

	assert.NotEqual(t, "0123456789abcdef", fp)
	assert.Len(t, fp, 16)

	base := user.without(map[string]bool{pkgMod: true})
	assert.Nil(t, base.find(pkgMod))
	merged := user.clone()
	setBundleFingerprint(merged, base, fp)
	assert.Equal(t, fp, merged.find(pkgMod).annotation(annotationBundlePrefix),
		"a module that otelc adds gets the fingerprint")
	assert.Empty(t, merged.find("example.com/dep").annotation(annotationBundlePrefix),
		"a module of the user does not get the fingerprint")
	assert.Contains(t, string(merged.Format()), "## explicit; go 1.25; "+annotationBundlePrefix+fp+"\n")
}

func TestMergedGoSum(t *testing.T) {
	moduleDir := t.TempDir()
	scratchDir := t.TempDir()

	sum, err := mergedGoSum(moduleDir, scratchDir)
	require.NoError(t, err)
	assert.Nil(t, sum, "no go.sum in the scratch module: no change")

	mustWriteFile(t, filepath.Join(scratchDir, goSumFileName), "example.com/dep v1.2.0 h1:new=\n"+
		"example.com/dep v1.2.0/go.mod h1:newmod=\nexample.com/other v1.0.0 h1:other=\n")
	sum, err = mergedGoSum(moduleDir, scratchDir)
	require.NoError(t, err)
	assert.Equal(t, "example.com/dep v1.2.0 h1:new=\nexample.com/dep v1.2.0/go.mod h1:newmod=\n"+
		"example.com/other v1.0.0 h1:other=\n", string(sum), "no go.sum in the module: all the lines are added")

	mustWriteFile(t, filepath.Join(moduleDir, goSumFileName), "example.com/dep v1.0.0 h1:old=\n"+
		"example.com/other v1.0.0 h1:other=")
	sum, err = mergedGoSum(moduleDir, scratchDir)
	require.NoError(t, err)
	assert.Equal(t, "example.com/dep v1.0.0 h1:old=\nexample.com/other v1.0.0 h1:other=\n"+
		"example.com/dep v1.2.0 h1:new=\nexample.com/dep v1.2.0/go.mod h1:newmod=\n", string(sum),
		"the lines of the user stay, and only the missing lines are added")

	mustWriteFile(t, filepath.Join(moduleDir, goSumFileName), "example.com/dep v1.2.0 h1:new=\n"+
		"example.com/dep v1.2.0/go.mod h1:newmod=\nexample.com/other v1.0.0 h1:other=\n")
	sum, err = mergedGoSum(moduleDir, scratchDir)
	require.NoError(t, err)
	assert.Nil(t, sum, "all the lines are there: no change")
}

// TestVendorScratchPinFetchFails: the hook code needs a package of a vendored
// module that vendor/ does not have, and the module cannot be downloaded (a
// private module, or no network). The error tells the package, the module,
// the version and the fix.
func TestVendorScratchPinFetchFails(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, goModFileName), "module "+vendorScratchModule+"\n\ngo 1.25.0\n")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOMODCACHE", t.TempDir())

	s := &vendorScratch{
		dir:      dir,
		versions: map[string]string{"corp.example/private": "v1.0.0"},
		pinned:   map[string]bool{},
	}
	err := s.pin(t.Context(), "corp.example/private",
		[]string{"corp.example/private/sub", "example.com/other", "corp.example/private/sub"})
	var fetchErr *vendorFetchError
	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, []string{"corp.example/private/sub"}, fetchErr.Packages)
	msg := err.Error()
	assert.Contains(t, msg, "corp.example/private/sub")
	assert.Contains(t, msg, "corp.example/private@v1.0.0")
	assert.Contains(t, msg, "go mod vendor")
	assert.Contains(t, msg, "otelc vendor")
	assert.False(t, s.pinned["corp.example/private"])
	assert.Equal(t, fetchErr.Err, errors.Unwrap(fetchErr), "the error of the go command is kept")
}

func TestGoflagsWithoutModfile(t *testing.T) {
	assert.Equal(t, "-trimpath -tags=x",
		goflagsWithoutModfile("-modfile=/abs/custom.mod -trimpath '--modfile=b.mod' -tags=x"))
}

// TestVendorModeQuotedGoflags: the go command accepts quoted GOFLAGS tokens
// (see #1267). A quoted -mod flag must count, so that vendor mode is not used
// against the choice of the user.
func TestVendorModeQuotedGoflags(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
	mustWriteFile(t, filepath.Join(root, "vendor", "modules.txt"), "")
	t.Setenv("GOWORK", "off")

	for _, goflags := range []string{`'-mod=readonly'`, `"-mod=mod"`, `-trimpath '-mod=readonly'`} {
		t.Setenv("GOFLAGS", goflags)
		assert.NotEmpty(t, lastModFlag("build", nil), goflags)
		assert.False(t, mustVendorMode(t.Context(), t, root, nil), goflags)
	}
	t.Setenv("GOFLAGS", `'-mod=vendor'`)
	assert.True(t, mustVendorMode(t.Context(), t, root, nil))
}

// The tests below run vendorSync with real go commands, but with no network:
// the modules come from a module proxy in a local directory (GOPROXY=file://).

// proxyModule is one module version of the test proxy. files maps the
// slash-separated file names to their content. go.mod is required.
type proxyModule struct {
	path    string
	version string
	files   map[string]string
}

// newTestProxy writes the modules in the format of a module proxy, and returns
// the GOPROXY value.
func newTestProxy(t *testing.T, mods ...proxyModule) string {
	t.Helper()
	root := t.TempDir()
	versions := make(map[string][]string)
	for _, m := range mods {
		escaped, err := module.EscapePath(m.path)
		require.NoError(t, err)
		dir := filepath.Join(root, filepath.FromSlash(escaped), "@v")
		require.NoError(t, os.MkdirAll(dir, 0o755))

		src := t.TempDir()
		for name, content := range m.files {
			mustWriteFile(t, filepath.Join(src, filepath.FromSlash(name)), content)
		}
		zipFile, err := os.Create(filepath.Join(dir, m.version+".zip"))
		require.NoError(t, err)
		require.NoError(t, modzip.CreateFromDir(zipFile, module.Version{Path: m.path, Version: m.version}, src))
		require.NoError(t, zipFile.Close())
		mustWriteFile(t, filepath.Join(dir, m.version+".mod"), m.files[goModFileName])
		mustWriteFile(t, filepath.Join(dir, m.version+".info"),
			`{"Version":"`+m.version+`","Time":"2024-01-01T00:00:00Z"}`)
		versions[dir] = append(versions[dir], m.version)
	}
	for dir, list := range versions {
		mustWriteFile(t, filepath.Join(dir, "list"), strings.Join(list, "\n")+"\n")
	}
	// file:///tmp/x on Unix, file:///C:/x on Windows.
	return "file:///" + strings.TrimPrefix(filepath.ToSlash(root), "/")
}

// testProxyModules are the modules of the test proxy:
//   - example.com/dep v1.0.0 and v1.2.0 (packages dep and dep/sub; v1.2.0
//     also has dep/newpkg).
//   - example.com/hook imports dep and requires dep v1.2.0.
//   - example.com/hooksub imports dep/sub and requires dep v1.0.0.
//   - example.com/extra has no dependencies.
//   - example.com/raiser requires example.com/other v1.1.0, but does not
//     import it.
//   - example.com/other v1.0.0 and v1.1.0.
//   - example.com/newgo needs go 1.26.0.
//   - example.com/hookpriv imports corp.example/private/sub, which no proxy
//     has.
//   - example.com/embedlib v1.0.0 and v1.1.0 embed assets/* (a
//     subdirectory): v1.0.0 has assets/old.txt, v1.1.0 has assets/new.txt.
//   - example.com/embedhook imports embedlib (v1.0.0), and embeds data/*.
//   - example.com/embedhooknew imports embedlib and requires v1.1.0.
//   - example.com/ruledep and example.com/tooldep have no dependencies.
func testProxyModules() []proxyModule {
	goMod := func(path string, requires ...string) string {
		var out strings.Builder
		out.WriteString("module " + path + "\n\ngo 1.21\n")
		for _, r := range requires {
			out.WriteString("\nrequire " + r + "\n")
		}
		return out.String()
	}
	return []proxyModule{
		{path: "example.com/dep", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/dep"),
			"dep.go":      "package dep\n\nconst V = \"v1.0.0\"\n",
			"sub/sub.go":  "package sub\n\nconst V = \"v1.0.0\"\n",
			"LICENSE":     "license",
		}},
		{path: "example.com/dep", version: "v1.2.0", files: map[string]string{
			goModFileName:   goMod("example.com/dep"),
			"dep.go":        "package dep\n\nconst V = \"v1.2.0\"\n",
			"sub/sub.go":    "package sub\n\nconst V = \"v1.2.0\"\n",
			"newpkg/new.go": "package newpkg\n",
			"LICENSE":       "license",
		}},
		{path: "example.com/hook", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/hook", "example.com/dep v1.2.0"),
			"hook.go":     "package hook\n\nimport \"example.com/dep\"\n\nconst V = dep.V\n",
		}},
		{path: "example.com/hooksub", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/hooksub", "example.com/dep v1.0.0"),
			"hook.go":     "package hooksub\n\nimport \"example.com/dep/sub\"\n\nconst V = sub.V\n",
		}},
		{path: "example.com/extra", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/extra"),
			"extra.go":    "package extra\n",
		}},
		{path: "example.com/raiser", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/raiser", "example.com/other v1.1.0"),
			"raiser.go":   "package raiser\n",
		}},
		{path: "example.com/other", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/other"),
			"other.go":    "package other\n",
		}},
		{path: "example.com/other", version: "v1.1.0", files: map[string]string{
			goModFileName: goMod("example.com/other"),
			"other.go":    "package other\n\n// v1.1.0\n",
		}},
		{path: "example.com/newgo", version: "v1.0.0", files: map[string]string{
			goModFileName: "module example.com/newgo\n\ngo 1.26.0\n",
			"newgo.go":    "package newgo\n",
		}},
		{path: "example.com/hookpriv", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/hookpriv", "corp.example/private v1.0.0"),
			"hook.go":     "package hookpriv\n\nimport _ \"corp.example/private/sub\"\n",
		}},
		{path: "example.com/embedlib", version: "v1.0.0", files: map[string]string{
			goModFileName:    goMod("example.com/embedlib"),
			"embedlib.go":    embedSource("embedlib", "assets/*", ""),
			"assets/old.txt": "old",
		}},
		{path: "example.com/embedlib", version: "v1.1.0", files: map[string]string{
			goModFileName:    goMod("example.com/embedlib"),
			"embedlib.go":    embedSource("embedlib", "assets/*", ""),
			"assets/new.txt": "new",
		}},
		{path: "example.com/embedhook", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/embedhook", "example.com/embedlib v1.0.0"),
			"hook.go":     embedSource("embedhook", "data/*", "example.com/embedlib"),
			"data/d.txt":  "data",
		}},
		{path: "example.com/embedhooknew", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/embedhooknew", "example.com/embedlib v1.1.0"),
			"hook.go":     "package embedhooknew\n\nimport _ \"example.com/embedlib\"\n",
		}},
		{path: "example.com/ruledep", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/ruledep"),
			"ruledep.go":  "package ruledep\n",
		}},
		{path: "example.com/tooldep", version: "v1.0.0", files: map[string]string{
			goModFileName: goMod("example.com/tooldep"),
			"tooldep.go":  "package tooldep\n",
		}},
	}
}

// embedSource returns a Go file of package name that embeds the files of
// pattern, and that imports imp (if not "").
func embedSource(name, pattern, imp string) string {
	src := "package " + name + "\n\nimport (\n\t\"embed\"\n"
	if imp != "" {
		src += "\n\t_ \"" + imp + "\"\n"
	}
	return src + ")\n\n//go:embed " + pattern + "\nvar Files embed.FS\n"
}

// patchedDep is the content of the vendored dep.go file of the test app: the
// user changed it, so otelc must keep it.
const patchedDep = "package dep\n\nconst V = \"patched\"\n"

// newVendorSyncApp writes a vendored app in a new directory, and sets the
// environment for offline go commands with the test proxy. The app vendors
// example.com/dep v1.0.0 (package dep only, with a patch), example.com/other
// v1.0.0 and corp.example/private v1.0.0 (only in vendor/).
func newVendorSyncApp(t *testing.T) string {
	t.Helper()
	appDir := t.TempDir()
	t.Chdir(appDir)
	t.Setenv(util.EnvOtelcWorkDir, appDir)
	t.Setenv("GOPROXY", newTestProxy(t, testProxyModules()...))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GONOSUMDB", "")
	// GONOPROXY would make the go command skip the local proxy.
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOMODCACHE", t.TempDir())
	// The module cache is read-only by default: t.TempDir could not remove it.
	t.Setenv("GOFLAGS", "-modcacherw")

	mustWriteFile(t, filepath.Join(appDir, goModFileName), `module example.com/app

go 1.25.0

require (
	corp.example/private v1.0.0
	example.com/dep v1.0.0
	example.com/other v1.0.0
)
`)
	mustWriteFile(t, filepath.Join(appDir, "main.go"), `package main

import (
	_ "corp.example/private"
	_ "example.com/dep"
	_ "example.com/other"
)

func main() {}
`)
	mustWriteFile(t, filepath.Join(appDir, "vendor", modulesTxtFileName), `# corp.example/private v1.0.0
## explicit; go 1.21
corp.example/private
# example.com/dep v1.0.0
## explicit; go 1.21
example.com/dep
# example.com/other v1.0.0
## explicit; go 1.21
example.com/other
`)
	mustWriteFile(t, filepath.Join(appDir, "vendor", "corp.example", "private", "private.go"), "package private\n")
	mustWriteFile(t, filepath.Join(appDir, "vendor", "example.com", "dep", "dep.go"), patchedDep)
	mustWriteFile(t, filepath.Join(appDir, "vendor", "example.com", "dep", "LICENSE"), "license")
	mustWriteFile(t, filepath.Join(appDir, "vendor", "example.com", "other", "other.go"), "package other\n")
	return appDir
}

// appTree hashes the files of the app, but not the otelc work files.
func appTree(t *testing.T, appDir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(appDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == util.BuildTempDir {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == util.BuildLockFile {
			return nil
		}
		var data []byte
		var readErr error
		if d.Type()&os.ModeSymlink != 0 {
			// A link is not followed: its target is the content.
			var target string
			target, readErr = os.Readlink(path)
			data = []byte("link:" + target)
		} else {
			data, readErr = os.ReadFile(path)
		}
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(appDir, path)
		if relErr != nil {
			return relErr
		}
		sum := sha256.Sum256(data)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	}))
	return out
}

func readAppFile(t *testing.T, appDir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(appDir, filepath.FromSlash(rel)))
	require.NoError(t, err)
	return string(data)
}

// requireVendorBuild checks that the go command alone builds the app and the
// packages pkgs (the hook code) from vendor/, with no network.
func requireVendorBuild(t *testing.T, appDir string, pkgs ...string) {
	t.Helper()
	builds := [][]string{{"build", "-mod=vendor", "-o", filepath.Join(t.TempDir(), "app"), "."}}
	if len(pkgs) > 0 {
		builds = append(builds, append([]string{"build", "-mod=vendor"}, pkgs...))
	}
	for _, args := range builds {
		cmd := exec.CommandContext(t.Context(), "go", args...)
		cmd.Dir = appDir
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "go %s failed:\n%s", strings.Join(args, " "), out)
	}
}

// TestVendorSyncAddsModuleAndReverts: the hook code needs a module that the
// user does not vendor. A build adds it to vendor/, vendor/modules.txt and
// go.mod; the cleanup restores the app exactly.
func TestVendorSyncAddsModuleAndReverts(t *testing.T) {
	appDir := newVendorSyncApp(t)
	before := appTree(t, appDir)

	stateManager := newStateManager()
	ctx := contextWithStateManager(t.Context(), stateManager)
	require.NoError(t, vendorSync(ctx, map[string]bool{"example.com/extra": true}, appDir))

	modulesTxt := readAppFile(t, appDir, "vendor/modules.txt")
	assert.Contains(t, modulesTxt, "# example.com/extra v1.0.0\n## explicit; go 1.21\nexample.com/extra\n")
	assert.FileExists(t, filepath.Join(appDir, "vendor", "example.com", "extra", "extra.go"))
	assert.Contains(t, readAppFile(t, appDir, "go.mod"), "example.com/extra v1.0.0 // indirect")
	assert.Equal(t, patchedDep, readAppFile(t, appDir, "vendor/example.com/dep/dep.go"))
	assert.NoFileExists(t, filepath.Join(appDir, goSumFileName), "a build does not write go.sum")
	requireVendorBuild(t, appDir, "example.com/extra")

	require.NoError(t, revertState(stateManager))
	assert.Equal(t, before, appTree(t, appDir), "the cleanup restores the app exactly")
}

// TestVendorSyncConflict: the hook code needs a higher version of a vendored
// module. A build fails with a *vendorConflictError, and changes nothing.
func TestVendorSyncConflict(t *testing.T) {
	appDir := newVendorSyncApp(t)
	before := appTree(t, appDir)

	ctx := contextWithStateManager(t.Context(), newStateManager())
	err := vendorSync(ctx, map[string]bool{"example.com/hook": true}, appDir)
	var conflict *vendorConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "example.com/dep", conflict.Module)
	assert.Equal(t, "v1.0.0", conflict.VendoredVersion)
	assert.Equal(t, "v1.2.0", conflict.RequiredVersion)
	assert.Equal(t, "example.com/hook", conflict.RequiredBy)
	assert.Equal(t, before, appTree(t, appDir), "a failed build changes nothing")
}

// TestVendorSyncMissingPackage: the hook code needs a package of a vendored
// module that vendor/ does not have. otelc gets the module at the vendored
// version and adds only the missing package. It keeps the patched file.
func TestVendorSyncMissingPackage(t *testing.T) {
	appDir := newVendorSyncApp(t)

	require.NoError(t, vendorSync(t.Context(), map[string]bool{"example.com/hooksub": true}, appDir))

	modulesTxt := readAppFile(t, appDir, "vendor/modules.txt")
	assert.Contains(
		t,
		modulesTxt,
		"# example.com/dep v1.0.0\n## explicit; go 1.21\nexample.com/dep\nexample.com/dep/sub\n",
		"the missing package is added at the vendored version",
	)
	assert.Equal(
		t,
		"package sub\n\nconst V = \"v1.0.0\"\n",
		readAppFile(t, appDir, "vendor/example.com/dep/sub/sub.go"),
	)
	assert.Equal(t, patchedDep, readAppFile(t, appDir, "vendor/example.com/dep/dep.go"),
		"a vendored file is never replaced")
	requireVendorBuild(t, appDir, "example.com/hooksub")
}

// TestVendorSyncMissingPackageCannotBeFetched: the hook code needs a package
// of a vendored module that no proxy has. The error tells the package, the
// module version and the fix.
func TestVendorSyncMissingPackageCannotBeFetched(t *testing.T) {
	appDir := newVendorSyncApp(t)
	before := appTree(t, appDir)

	err := vendorSync(t.Context(), map[string]bool{"example.com/hookpriv": true}, appDir)
	var fetchErr *vendorFetchError
	require.ErrorAs(t, err, &fetchErr)
	assert.Equal(t, "corp.example/private", fetchErr.Module)
	assert.Equal(t, "v1.0.0", fetchErr.Version)
	assert.Equal(t, []string{"corp.example/private/sub"}, fetchErr.Packages)
	assert.Equal(t, before, appTree(t, appDir))
}

// TestVendorSyncUpdate: otelc vendor (update mode) gets the higher version
// that the hook code needs, like "go get" and "go mod vendor": it replaces
// the vendored files of the module, and changes go.mod and go.sum. A second
// run takes the fast path and changes nothing.
func TestVendorSyncUpdate(t *testing.T) {
	appDir := newVendorSyncApp(t)
	ctx := contextWithVendorUpdate(t.Context())
	imports := map[string]bool{"example.com/hook": true}

	require.NoError(t, vendorSync(ctx, imports, appDir))

	modulesTxt := readAppFile(t, appDir, "vendor/modules.txt")
	assert.Contains(t, modulesTxt, "# example.com/dep v1.2.0\n")
	assert.Contains(t, modulesTxt, "# example.com/hook v1.0.0\n")
	assert.Equal(t, "package dep\n\nconst V = \"v1.2.0\"\n", readAppFile(t, appDir, "vendor/example.com/dep/dep.go"),
		"the files of an updated module come from the new version")
	goMod := readAppFile(t, appDir, "go.mod")
	assert.Contains(t, goMod, "example.com/dep v1.2.0")
	assert.Contains(t, goMod, "example.com/hook v1.0.0 // indirect")
	goSum := readAppFile(t, appDir, goSumFileName)
	assert.Contains(t, goSum, "example.com/dep v1.2.0 h1:")
	assert.Contains(t, goSum, "example.com/hook v1.0.0 h1:")
	requireVendorBuild(t, appDir, "example.com/hook")

	after := appTree(t, appDir)
	require.NoError(t, vendorSync(ctx, imports, appDir))
	assert.Equal(t, after, appTree(t, appDir), "the second run changes nothing")
}

// TestVendorSyncUpdateRaisedModule: in update mode, a vendored module that
// the module graph raises (no package of the hook code imports it) is also
// updated, so that go.mod agrees with minimal version selection.
func TestVendorSyncUpdateRaisedModule(t *testing.T) {
	appDir := newVendorSyncApp(t)

	require.NoError(t, vendorSync(contextWithVendorUpdate(t.Context()),
		map[string]bool{"example.com/raiser": true}, appDir))

	assert.Contains(t, readAppFile(t, appDir, "vendor/modules.txt"), "# example.com/other v1.1.0\n")
	assert.Contains(t, readAppFile(t, appDir, "vendor/example.com/other/other.go"), "// v1.1.0")
	assert.Contains(t, readAppFile(t, appDir, "go.mod"), "example.com/other v1.1.0")
	assert.Contains(t, readAppFile(t, appDir, "vendor/modules.txt"), "# example.com/dep v1.0.0\n",
		"a module that the graph does not raise keeps its version")
	requireVendorBuild(t, appDir, "example.com/raiser")
}

// TestVendorSyncToolFiles: the imports of a tool file go to vendorSync.
func TestVendorSyncToolFiles(t *testing.T) {
	appDir := newVendorSyncApp(t)
	toolFile := filepath.Join(appDir, toolFileCanonical)
	mustWriteFile(t, toolFile, "//go:build tools\n\npackage tools\n\nimport _ \"example.com/extra\"\n")

	require.NoError(t, vendorSyncToolFiles(t.Context(), []string{toolFile}))
	assert.Contains(t, readAppFile(t, appDir, "vendor/modules.txt"), "# example.com/extra v1.0.0\n")
}

// TestVendorSyncUpdateDropsOldPackagesAndStaleModules: in update mode, a
// vendored package that the new version of its module does not have is not
// kept, and an otelc module from a different bundle is removed.
func TestVendorSyncUpdateDropsOldPackagesAndStaleModules(t *testing.T) {
	appDir := newVendorSyncApp(t)
	const staleMod = util.OtelcInstRoot + "/stale"
	const staleVersion = "v0.0.0-00010101000000-000000000000"
	modulesTxt := readAppFile(t, appDir, "vendor/modules.txt")
	modulesTxt = strings.Replace(modulesTxt, "example.com/dep\n", "example.com/dep\nexample.com/dep/old\n", 1)
	modulesTxt += "# " + staleMod + " " + staleVersion + " => ./.otelc-build/stale\n" +
		"## explicit; go 1.21; " + annotationBundlePrefix + "0000000000000000\n" + staleMod + "\n"
	mustWriteFile(t, filepath.Join(appDir, "vendor", modulesTxtFileName), modulesTxt)
	mustWriteFile(t, filepath.Join(appDir, "vendor", "example.com", "dep", "old", "old.go"), "package old\n")
	mustWriteFile(t, filepath.Join(appDir, "vendor", filepath.FromSlash(staleMod), "stale.go"), "package stale\n")
	goMod := readAppFile(t, appDir, goModFileName) + "\nrequire " + staleMod + " " + staleVersion + "\n" +
		"\nreplace " + staleMod + " " + staleVersion + " => ./.otelc-build/stale\n"
	mustWriteFile(t, filepath.Join(appDir, goModFileName), goMod)

	require.NoError(
		t,
		vendorSync(contextWithVendorUpdate(t.Context()), map[string]bool{"example.com/hook": true}, appDir),
	)

	modulesTxt = readAppFile(t, appDir, "vendor/modules.txt")
	assert.Contains(t, modulesTxt, "# example.com/dep v1.2.0\n")
	assert.NotContains(t, modulesTxt, "example.com/dep/old", "v1.2.0 has no package old")
	assert.NoFileExists(t, filepath.Join(appDir, "vendor", "example.com", "dep", "old", "old.go"))
	assert.NotContains(t, modulesTxt, staleMod, "the stale otelc module is removed")
	assert.NoFileExists(t, filepath.Join(appDir, "vendor", filepath.FromSlash(staleMod), "stale.go"))
	assert.NotContains(t, readAppFile(t, appDir, goModFileName), "replace "+staleMod,
		"the replace of the stale module is removed")
}

func TestFindVendorConflictsSorted(t *testing.T) {
	versions := map[string]string{"example.com/a": "v1.0.0", "example.com/b": "v1.0.0"}
	fixed := map[string]bool{"example.com/a": true, "example.com/b": true}
	graph := map[string]map[string]string{
		"example.com/x@v1.0.0": {"example.com/a": "v1.1.0", "example.com/b": "v1.3.0"},
		"example.com/y@v1.0.0": {"example.com/a": "v1.2.0", "example.com/b": "v1.3.0"},
	}
	pkgs := []listedPackage{
		{
			importPath: "example.com/y",
			module:     "example.com/y@v1.0.0",
			imports:    []string{"example.com/b", "example.com/a"},
		},
		{
			importPath: "example.com/x",
			module:     "example.com/x@v1.0.0",
			imports:    []string{"example.com/b", "example.com/a"},
		},
		{importPath: "example.com/a", module: "example.com/a@v1.2.0"},
		{importPath: "example.com/b", module: "example.com/b@v1.3.0"},
	}
	got := findVendorConflicts(versions, fixed, graph, pkgs)
	keys := make([]string, 0, len(got))
	for _, c := range got {
		keys = append(keys, c.Module+" "+c.RequiredVersion+" "+c.RequiredBy)
	}
	assert.Equal(t, []string{
		"example.com/a v1.2.0 example.com/y",
		"example.com/a v1.1.0 example.com/x",
		"example.com/b v1.3.0 example.com/x",
		"example.com/b v1.3.0 example.com/y",
	}, keys, "by module, then the highest version first, then the requiring module")
}

func TestFillVendoredVersionsBadModulesTxt(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "vendor", "modules.txt"), "not a modules.txt\n")
	deps := []*Dependency{{
		ImportPath: "example.com/m/pkg",
		Sources:    []string{filepath.Join(root, "vendor", "example.com", "m", "pkg", "a.go")},
	}}
	fillVendoredVersions(t.Context(), deps)
	assert.Empty(t, deps[0].Version, "a broken modules.txt gives no version")
}

// TestVendorSyncBumpsGoVersion: a module of the hook code needs a higher go
// version than the go.mod of the user. The go directive is raised, because
// the go command uses it as the minimum for all the modules.
func TestVendorSyncBumpsGoVersion(t *testing.T) {
	appDir := newVendorSyncApp(t)
	require.NoError(
		t,
		vendorSync(contextWithVendorUpdate(t.Context()), map[string]bool{"example.com/newgo": true}, appDir),
	)
	assert.Contains(t, readAppFile(t, appDir, goModFileName), "\ngo 1.26.0\n")
	requireVendorBuild(t, appDir, "example.com/newgo")
}

func TestVendorSyncInputErrors(t *testing.T) {
	appDir := newVendorSyncApp(t)
	require.NoError(t, vendorSync(t.Context(), nil, appDir), "no imports: nothing to do")

	modulesTxt := filepath.Join(appDir, "vendor", modulesTxtFileName)
	data, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	mustWriteFile(t, modulesTxt, "not a modules.txt\n")
	require.Error(t, vendorSync(t.Context(), map[string]bool{"example.com/extra": true}, appDir))

	require.NoError(t, os.Remove(modulesTxt))
	require.Error(t, vendorSync(t.Context(), map[string]bool{"example.com/extra": true}, appDir),
		"no vendor/modules.txt")

	if runtime.GOOS != "windows" {
		target := filepath.Join(t.TempDir(), modulesTxtFileName)
		mustWriteFile(t, target, string(data))
		require.NoError(t, os.Symlink(target, modulesTxt))
		err = vendorSync(t.Context(), map[string]bool{"example.com/extra": true}, appDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symbolic link")
		require.NoError(t, os.Remove(modulesTxt))
	}

	mustWriteFile(t, modulesTxt, string(data))
	mustWriteFile(t, filepath.Join(appDir, goModFileName), "not a go.mod")
	require.Error(t, vendorSync(t.Context(), map[string]bool{"example.com/extra": true}, appDir), "a bad go.mod")
}

// TestVendorSyncKeepsCurrentOtelcModule: an otelc module of this bundle in
// vendor/ is not copied into the scratch module and is not replaced.
func TestVendorSyncKeepsCurrentOtelcModule(t *testing.T) {
	appDir := newVendorSyncApp(t)
	const pkgVersion = "v0.0.0-00010101000000-000000000000"
	block := "# " + util.OtelcPkgRoot + " " + pkgVersion + " => ./.otelc-build/pkg\n" +
		"## explicit; go 1.21; " + annotationBundlePrefix + bundleFingerprint() + "\n" + util.OtelcPkgRoot + "/hook\n"
	modulesTxt := filepath.Join(appDir, "vendor", modulesTxtFileName)
	mustWriteFile(t, modulesTxt, readAppFile(t, appDir, "vendor/modules.txt")+block)
	mustWriteFile(t, filepath.Join(appDir, "vendor", filepath.FromSlash(util.OtelcPkgRoot), "hook", "hook.go"),
		"package hook\n// current\n")

	require.NoError(t, vendorSync(t.Context(), map[string]bool{"example.com/extra": true}, appDir))
	assert.Contains(t, readAppFile(t, appDir, "vendor/modules.txt"), block)
	assert.Equal(t, "package hook\n// current\n",
		readAppFile(t, appDir, "vendor/"+util.OtelcPkgRoot+"/hook/hook.go"))
}

func TestVendorScratchHelpers(t *testing.T) {
	graph := parseModGraph([]byte("malformed\nmain example.com/noversion\nmain example.com/a@v1.0.0\n"))
	assert.Equal(t, map[string]map[string]string{"main": {"example.com/a": "v1.0.0"}}, graph,
		"lines without a version are ignored")

	s := &vendorScratch{
		versions: map[string]string{"example.com/dep": "v1.3.0"},
		updated:  map[string]bool{"example.com/dep": true},
		pinned:   map[string]bool{},
		extra:    map[string]bool{},
	}
	require.NoError(t, s.update([]*vendorConflictError{
		{Module: "example.com/dep", VendoredVersion: "v1.0.0", RequiredVersion: "v1.2.0"},
	}, mustParseModulesTxt(t, "# example.com/dep v1.0.0\n## explicit\nexample.com/dep\n")))
	assert.Equal(t, "v1.3.0", s.versions["example.com/dep"], "a lower required version does not change the version")

	conflicts := findVendorConflicts(
		map[string]string{"example.com/dep": "v1.0.0"},
		map[string]bool{"example.com/dep": true},
		map[string]map[string]string{"example.com/sdk@v1.0.0": {"example.com/dep": "v1.2.0"}},
		[]listedPackage{
			{importPath: "example.com/sdk", module: "example.com/sdk@v1.0.0", imports: []string{"example.com/dep"}},
			{importPath: "example.com/sdk/b", module: "example.com/sdk@v1.0.0", imports: []string{"example.com/dep"}},
			{importPath: "example.com/dep", module: "example.com/dep@v1.2.0"},
		})
	assert.Len(t, conflicts, 1, "two packages of one module give one conflict")

	copies, dirs, err := planVendorCopies(mustParseModulesTxt(t, ""), []string{"example.com/absent"},
		vendorCopyRoots{scratchVendor: t.TempDir(), moduleDir: t.TempDir()})
	require.NoError(t, err)
	assert.Empty(t, copies)
	assert.Empty(t, dirs)
}

func TestEmbeddedSourceDirRoots(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	instDir := filepath.Join(workDir, util.BuildTempDir, unzippedInstDir)
	pkgDir := filepath.Join(workDir, util.BuildTempDir, unzippedPkgDir)
	require.NoError(t, os.MkdirAll(filepath.Join(pkgDir, "runtime"), 0o755))
	require.NoError(t, os.MkdirAll(instDir, 0o755))

	for importPath, want := range map[string]string{
		util.OtelcInstRoot:             instDir,
		util.OtelcPkgRoot:              pkgDir,
		util.OtelcPkgRoot + "/runtime": filepath.Join(pkgDir, "runtime"),
	} {
		dir, ok := embeddedSourceDir(importPath)
		assert.True(t, ok, importPath)
		assert.Equal(t, want, dir, importPath)
	}

	set := map[string]bool{}
	require.NoError(
		t,
		collectFileImports(util.OtelcPkgRoot+"/runtime", "absent.go", set, &hookSources{user: &modulesTxt{}}),
	)
	assert.Empty(t, set, "a file that does not exist adds no imports")
	require.NoError(t, collectFileImports("example.com/other", "a.go", set, &hookSources{user: &modulesTxt{}}))
	assert.Empty(t, set, "a package that is not in the bundle adds no imports")
}

func TestUserReplacementVersion(t *testing.T) {
	mf, err := modfile.Parse(goModFileName, []byte(`module example.com/app

replace example.com/a => ../any

replace example.com/a v1.0.0 => example.com/fork v1.0.1
`), nil)
	require.NoError(t, err)
	text, ok := userReplacement(mf, "example.com/a", "v1.0.0")
	assert.True(t, ok)
	assert.Equal(t, "example.com/fork v1.0.1", text, "a replace of the exact version wins")
	text, ok = userReplacement(mf, "example.com/a", "v2.0.0")
	assert.True(t, ok)
	assert.Equal(t, "../any", text)
	_, ok = userReplacement(mf, "example.com/b", "v1.0.0")
	assert.False(t, ok)
}

func TestParseModulesTxtMoreErrors(t *testing.T) {
	m, err := parseModulesTxt([]byte("\n# example.com/a v1.0.0\n\n## explicit\nexample.com/a\n\n"))
	require.NoError(t, err, "blank lines are ignored")
	assert.Equal(t, []string{"example.com/a"}, m.Packages("example.com/a"))

	for name, data := range map[string]string{
		"unknown comment line":       "#comment\n",
		"no module path":             "# \n",
		"text after the version":     "# example.com/a v1.0.0 extra\n",
		"line longer than the limit": "# example.com/a v1.0.0\n" + strings.Repeat("a", 2*1024*1024) + "\n",
	} {
		_, parseErr := parseModulesTxt([]byte(data))
		require.Error(t, parseErr, name)
	}
}

// TestVendorSyncRejectsVendoredLink: a symbolic link in a vendored package
// stops vendorSync before it changes anything.
func TestVendorSyncRejectsVendoredLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need special permissions on Windows")
	}
	appDir := newVendorSyncApp(t)
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "outside.go"),
		filepath.Join(appDir, "vendor", "example.com", "dep", "link.go")))
	before := appTree(t, appDir)

	err := vendorSync(t.Context(), map[string]bool{"example.com/extra": true}, appDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symbolic link")
	assert.Equal(t, before, appTree(t, appDir))
}

// TestApplyVendorMergeWriteErrors: a write that fails stops the merge with
// an error.
func TestApplyVendorMergeWriteErrors(t *testing.T) {
	skipWithoutPermissions(t)
	newModule := func(t *testing.T) (string, *modfile.File, *modulesTxt) {
		t.Helper()
		moduleDir := t.TempDir()
		t.Chdir(moduleDir)
		t.Setenv(util.EnvOtelcWorkDir, moduleDir)
		t.Setenv("GOWORK", "off")
		mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, modulesTxtFileName),
			"# example.com/a v1.0.0\n## explicit\nexample.com/a\n")
		mustWriteFile(t, filepath.Join(moduleDir, goModFileName), "module example.com/app\n")
		mf, err := modfile.Parse(goModFileName, []byte("module example.com/app\n"), nil)
		require.NoError(t, err)
		return moduleDir, mf, mustParseModulesTxt(t, "# example.com/a v1.0.0\n## explicit\nexample.com/a\n")
	}
	readOnly := func(t *testing.T, dir string) {
		t.Helper()
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	}

	t.Run("modules.txt", func(t *testing.T) {
		moduleDir, mf, merged := newModule(t)
		readOnly(t, filepath.Join(moduleDir, vendorDirName))
		require.Error(t, applyVendorMerge(t.Context(), moduleDir, t.TempDir(),
			vendorMergeResult{goMod: mf, merged: merged}))
	})
	t.Run("go.mod", func(t *testing.T) {
		moduleDir, mf, merged := newModule(t)
		readOnly(t, moduleDir)
		require.Error(t, applyVendorMerge(t.Context(), moduleDir, t.TempDir(),
			vendorMergeResult{goMod: mf, merged: merged}))
	})
	t.Run("removed file is a directory", func(t *testing.T) {
		moduleDir, mf, merged := newModule(t)
		dir := filepath.Join(moduleDir, vendorDirName, "example.com", "a")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.Error(t, applyVendorMerge(t.Context(), moduleDir, t.TempDir(),
			vendorMergeResult{goMod: mf, merged: merged, deleted: []string{dir}}))
	})
	t.Run("state cannot be saved", func(t *testing.T) {
		moduleDir, mf, merged := newModule(t)
		require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
		readOnly(t, util.GetBuildTempDir())
		ctx := contextWithStateManager(t.Context(), newStateManager())
		require.Error(t, applyVendorMerge(ctx, moduleDir, t.TempDir(),
			vendorMergeResult{goMod: mf, merged: merged}))
	})
}

// skipWithoutPermissions skips a test that needs directory permissions: on
// Windows they are different, and root ignores them.
func skipWithoutPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions are not enforced")
	}
}

func makeReadOnly(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.Chmod(path, 0o555))
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
}

// TestVendorSyncFailures: each failure stops vendorSync with an error.
func TestVendorSyncFailures(t *testing.T) {
	skipWithoutPermissions(t)

	t.Run("the bundle cannot be extracted", func(t *testing.T) {
		appDir := newVendorSyncApp(t)
		mustWriteFile(t, filepath.Join(appDir, util.BuildTempDir), "not a directory")
		require.Error(t, vendorSync(t.Context(), map[string]bool{"example.com/extra": true}, appDir))
	})

	t.Run("a rule file of the bundle is not valid", func(t *testing.T) {
		appDir := newVendorSyncApp(t)
		mustWriteFile(t, filepath.Join(appDir, util.BuildTempDir, unzippedInstDir, "fake", "otelc.yaml"), "a: [")
		require.Error(t, vendorSync(t.Context(), map[string]bool{util.OtelcInstRoot + "/fake": true}, appDir))
	})

	t.Run("go.sum is not a file", func(t *testing.T) {
		appDir := newVendorSyncApp(t)
		require.NoError(t, os.MkdirAll(filepath.Join(appDir, goSumFileName), 0o755))
		require.Error(
			t,
			vendorSync(contextWithVendorUpdate(t.Context()), map[string]bool{"example.com/hook": true}, appDir),
		)
	})

	t.Run("vendor is read-only", func(t *testing.T) {
		appDir := newVendorSyncApp(t)
		makeReadOnly(t, filepath.Join(appDir, "vendor", "example.com", "dep"))
		require.Error(
			t,
			vendorSync(contextWithVendorUpdate(t.Context()), map[string]bool{"example.com/hook": true}, appDir),
			"the old files of the updated module cannot be removed",
		)
	})

	// The copy of vendor/ uses hard links, so it does not read the vendored
	// files. It must read the vendored directories.
	t.Run("a vendored directory cannot be read", func(t *testing.T) {
		appDir := newVendorSyncApp(t)
		dir := filepath.Join(appDir, "vendor", "example.com", "dep")
		require.NoError(t, os.Chmod(dir, 0o300))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		require.Error(t, vendorSync(t.Context(), map[string]bool{"example.com/extra": true}, appDir))
	})

	t.Run("the scratch module cannot be made", func(t *testing.T) {
		newVendorSyncApp(t)
		require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
		makeReadOnly(t, util.GetBuildTempDir())
		_, err := buildVendorScratch(t.Context(), vendorScratchInput{user: &modulesTxt{}})
		require.Error(t, err)
	})

	t.Run("a tool file has a bad import or a conflict", func(t *testing.T) {
		appDir := newVendorSyncApp(t)
		toolFile := filepath.Join(appDir, toolFileCanonical)
		mustWriteFile(
			t,
			toolFile,
			"//go:build tools\n\npackage tools\n\nimport x \"example.com/extra\"\n\nvar _ = x.V\n",
		)
		require.Error(t, vendorSyncToolFiles(t.Context(), []string{toolFile}), "a named import")
		mustWriteFile(t, toolFile, "//go:build tools\n\npackage tools\n\nimport _ \"example.com/hook\"\n")
		var conflict *vendorConflictError
		require.ErrorAs(t, vendorSyncToolFiles(t.Context(), []string{toolFile}), &conflict)
	})
}

func TestCollectFileImportsBadFile(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	dir := filepath.Join(workDir, util.BuildTempDir, unzippedPkgDir, "bad")
	mustWriteFile(t, filepath.Join(dir, "bad.go"), "this is not Go")
	require.Error(
		t,
		collectFileImports(util.OtelcPkgRoot+"/bad", "bad.go", map[string]bool{}, &hookSources{user: &modulesTxt{}}),
	)
}

// vendoredTestModule is a module that addVendoredModule adds to an app.
type vendoredTestModule struct {
	path    string
	version string
	// replace is the directory of a replace of all the versions, or "".
	replace string
	pkgs    []string
	// files are the files below vendor/<path>, with slash-separated names.
	files map[string]string
}

// addVendoredModule adds a vendored module to the app of newVendorSyncApp:
// a require (and a replace, if mod.replace is not "") in go.mod, a block in
// vendor/modules.txt, and the files.
func addVendoredModule(t *testing.T, appDir string, mod vendoredTestModule) {
	t.Helper()
	goMod := readAppFile(t, appDir, goModFileName) + "\nrequire " + mod.path + " " + mod.version + "\n"
	block := "# " + mod.path + " " + mod.version
	if mod.replace != "" {
		goMod += "\nreplace " + mod.path + " => " + mod.replace + "\n"
		block += " => " + mod.replace
	}
	mustWriteFile(t, filepath.Join(appDir, goModFileName), goMod)
	block += "\n## explicit; go 1.21\n" + strings.Join(mod.pkgs, "\n") + "\n"
	if mod.replace != "" {
		// "go mod vendor" writes a replacement-only line for a replace
		// without a version.
		block += "# " + mod.path + " => " + mod.replace + "\n"
	}
	mustWriteFile(t, filepath.Join(appDir, vendorDirName, modulesTxtFileName),
		readAppFile(t, appDir, "vendor/modules.txt")+block)
	for name, content := range mod.files {
		mustWriteFile(t, filepath.Join(appDir, vendorDirName, filepath.FromSlash(mod.path), filepath.FromSlash(name)),
			content)
	}
}

// vendoredEmbedLib is example.com/embedlib v1.0.0 (see testProxyModules).
func vendoredEmbedLib() vendoredTestModule {
	return vendoredTestModule{
		path: "example.com/embedlib", version: "v1.0.0", pkgs: []string{"example.com/embedlib"},
		files: map[string]string{
			"embedlib.go":    embedSource("embedlib", "assets/*", ""),
			"assets/old.txt": "old",
		},
	}
}

// TestVendorSyncEmbeddedSubdirectories: "go mod vendor" copies the files that
// a package embeds, also in subdirectories. otelc must copy them too: from
// vendor/ to the scratch module (a vendored module with embedded files), and
// from the scratch module to vendor/ (a hook module with embedded files).
// Else the scratch module or the build fails with "pattern ...: no matching
// files found".
func TestVendorSyncEmbeddedSubdirectories(t *testing.T) {
	appDir := newVendorSyncApp(t)
	addVendoredModule(t, appDir, vendoredEmbedLib())
	requireVendorBuild(t, appDir, "example.com/embedlib")

	require.NoError(t, vendorSync(t.Context(), map[string]bool{"example.com/embedhook": true}, appDir))
	assert.Equal(t, "data", readAppFile(t, appDir, "vendor/example.com/embedhook/data/d.txt"),
		"the embedded file of the hook module is copied to vendor/")
	assert.Equal(t, "old", readAppFile(t, appDir, "vendor/example.com/embedlib/assets/old.txt"))
	requireVendorBuild(t, appDir, "example.com/embedhook")
}

// TestVendorSyncUpdateEmbeddedSubdirectories: otelc vendor updates a module
// with embedded files in a subdirectory. The old embedded files are removed,
// and the new ones are copied.
func TestVendorSyncUpdateEmbeddedSubdirectories(t *testing.T) {
	appDir := newVendorSyncApp(t)
	addVendoredModule(t, appDir, vendoredEmbedLib())

	require.NoError(t, vendorSync(contextWithVendorUpdate(t.Context()),
		map[string]bool{"example.com/embedhooknew": true}, appDir))
	assert.Contains(t, readAppFile(t, appDir, "vendor/modules.txt"), "# example.com/embedlib v1.1.0\n")
	assert.Equal(t, "new", readAppFile(t, appDir, "vendor/example.com/embedlib/assets/new.txt"))
	assert.NoFileExists(t, filepath.Join(appDir, "vendor", "example.com", "embedlib", "assets", "old.txt"),
		"the embedded file of the old version is removed")
	requireVendorBuild(t, appDir, "example.com/embedhooknew")
}

// localInstrumentationRules is a rule file of local instrumentation: the hook
// package is hooksPath.
func localInstrumentationRules(hooksPath string) string {
	return `version: "v1.0.0"
local_rule:
  target: example.com/dep
  where:
    func: Run
  do:
    - inject_hooks:
        before: Before
        path: "` + hooksPath + `"
`
}

// TestVendorSyncMainModuleInstrumentation: otel.instrumentation.go imports
// local instrumentation that is a package of the main module. Its rules name
// a hook package of the main module. The scratch module cannot import the
// packages of the main module, so otelc adds their imports: the imports of
// the hook package (example.com/ruledep) and of the instrumentation package
// (example.com/extra). The main module never goes into vendor/modules.txt or
// go.mod.
func TestVendorSyncMainModuleInstrumentation(t *testing.T) {
	appDir := newVendorSyncApp(t)
	mustWriteFile(
		t,
		filepath.Join(appDir, "instr", "instr.go"),
		"package instr\n\nimport (\n\t\"fmt\"\n\n\t_ \"example.com/app/instr/shared\"\n\t_ \"example.com/extra\"\n)\n\nvar _ = fmt.Sprint\n",
	)
	mustWriteFile(t, filepath.Join(appDir, "instr", "instr_test.go"),
		"package instr\n\nimport _ \"example.com/testonly\"\n")
	mustWriteFile(t, filepath.Join(appDir, "instr", "ignored.go"),
		"//go:build ignore\n\npackage main\n\nimport _ \"example.com/ignored\"\n")
	mustWriteFile(t, filepath.Join(appDir, "instr", "shared", "shared.go"), "package shared\n")
	mustWriteFile(t, filepath.Join(appDir, "instr", "otelc.yaml"),
		localInstrumentationRules("example.com/app/hooks"))
	mustWriteFile(t, filepath.Join(appDir, "hooks", "hooks.go"),
		"package hooks\n\nimport _ \"example.com/ruledep\"\n\nfunc Before() {}\n")
	goMod := readAppFile(t, appDir, goModFileName)

	require.NoError(t, vendorSync(t.Context(), map[string]bool{"example.com/app/instr": true}, appDir))

	modulesTxt := readAppFile(t, appDir, "vendor/modules.txt")
	assert.Contains(t, modulesTxt, "# example.com/extra v1.0.0\n", "an import of the instrumentation package")
	assert.Contains(t, modulesTxt, "# example.com/ruledep v1.0.0\n", "an import of the hook package of the rules")
	assert.NotContains(t, modulesTxt, "example.com/app\n", "the main module is not vendored")
	assert.NotContains(t, modulesTxt, "example.com/app ", "the main module is not vendored")
	gotGoMod := readAppFile(t, appDir, goModFileName)
	assert.NotContains(t, gotGoMod, "require example.com/app ", "the main module does not require itself")
	assert.NotContains(t, gotGoMod, "\texample.com/app ", "the main module does not require itself")
	assert.NotContains(t, gotGoMod, "replace example.com/app", "the main module is not replaced")
	assert.NotEqual(t, goMod, gotGoMod)
	requireVendorBuild(t, appDir, "example.com/app/instr", "example.com/app/hooks")

	// vendor/ has all the imports now: a second run is fast, and makes no
	// scratch module.
	require.NoError(t, os.RemoveAll(util.GetBuildTemp(vendorScratchDir)))
	before := appTree(t, appDir)
	require.NoError(t, vendorSync(t.Context(), map[string]bool{"example.com/app/instr": true}, appDir))
	assert.Equal(t, before, appTree(t, appDir))
	assert.NoDirExists(t, util.GetBuildTemp(vendorScratchDir))
}

// addLocalInstrumentationModule adds the module example.com/localinst to the
// app. go.mod replaces it with the directory ./localinst. vendor/ has only
// its package example.com/localinst. Its rules name the hook package
// example.com/localinst/hooks, which vendor/ does not have, and its tool file
// imports example.com/tooldep.
func addLocalInstrumentationModule(t *testing.T, appDir string) {
	t.Helper()
	const version = "v0.0.0-00010101000000-000000000000"
	instr := "package localinst\n\nimport _ \"example.com/dep\"\n"
	rules := localInstrumentationRules("example.com/localinst/hooks")
	localDir := filepath.Join(appDir, "localinst")
	mustWriteFile(t, filepath.Join(localDir, goModFileName), "module example.com/localinst\n\ngo 1.21\n")
	mustWriteFile(t, filepath.Join(localDir, "localinst.go"), instr)
	mustWriteFile(t, filepath.Join(localDir, "otelc.yaml"), rules)
	mustWriteFile(t, filepath.Join(localDir, "hooks", "hooks.go"),
		"package hooks\n\nimport _ \"example.com/ruledep\"\n\nfunc Before() {}\n")
	mustWriteFile(t, filepath.Join(localDir, toolFileCanonical),
		"//go:build tools\n\npackage tools\n\nimport _ \"example.com/tooldep\"\n")
	addVendoredModule(t, appDir, vendoredTestModule{
		path: "example.com/localinst", version: version, replace: "./localinst",
		pkgs: []string{"example.com/localinst"},
		files: map[string]string{
			"localinst.go":    instr,
			"otelc.yaml":      rules,
			toolFileCanonical: "//go:build tools\n\npackage tools\n\nimport _ \"example.com/tooldep\"\n",
		},
	})
}

// TestVendorSyncLocalModuleInstrumentation: otel.instrumentation.go imports
// local instrumentation in a nested module that go.mod replaces with a local
// directory (like test/apps/gincustom). Its hook package is not in vendor/,
// and the go command cannot download the module. The scratch module uses the
// local directory, so it finds the hook package. The rules and the tool file
// of the module add their packages.
func TestVendorSyncLocalModuleInstrumentation(t *testing.T) {
	appDir := newVendorSyncApp(t)
	addLocalInstrumentationModule(t, appDir)
	requireVendorBuild(t, appDir, "example.com/localinst")

	require.NoError(t, vendorSync(t.Context(), map[string]bool{"example.com/localinst": true}, appDir))

	modulesTxt := readAppFile(t, appDir, "vendor/modules.txt")
	assert.Contains(t, modulesTxt,
		"# example.com/localinst v0.0.0-00010101000000-000000000000 => ./localinst\n"+
			"## explicit; go 1.21\nexample.com/localinst\nexample.com/localinst/hooks\n",
		"the hook package comes from the local directory; the replacement of the user stays")
	assert.FileExists(t, filepath.Join(appDir, "vendor", "example.com", "localinst", "hooks", "hooks.go"))
	assert.Contains(t, modulesTxt, "# example.com/ruledep v1.0.0\n", "an import of the hook package")
	assert.Contains(t, modulesTxt, "# example.com/tooldep v1.0.0\n", "an import of the tool file of the module")
	goMod := readAppFile(t, appDir, goModFileName)
	assert.Contains(t, goMod, "replace example.com/localinst => ./localinst")
	assert.NotContains(t, goMod, filepath.ToSlash(appDir), "go.mod has no absolute path")
	requireVendorBuild(t, appDir, "example.com/localinst/hooks", "example.com/tooldep")
}

// TestVendorSyncKeepsLocalOtelcPathModule: a module below the otelc module
// path that go.mod replaces with a local directory (like the instrumentation
// module of test/apps/gincustom) is code of the user. It is not a stale otelc
// module, so it stays in the scratch module.
func TestVendorSyncKeepsLocalOtelcPathModule(t *testing.T) {
	const localPath = util.OtelcRoot + "/test/apps/x/instrumentation"
	user := mustParseModulesTxt(t, "# "+localPath+" v0.0.0-00010101000000-000000000000 => ./instrumentation\n"+
		"## explicit; go 1.21\n"+localPath+"\n"+
		"# "+util.OtelcRoot+"/other v0.1.0\n## explicit; go 1.21\n"+util.OtelcRoot+"/other\n")
	assert.Empty(t, staleOtelcModules(user, []string{localPath}, nil, bundleFingerprint()))
	assert.Equal(t, map[string]bool{util.OtelcRoot + "/other": true},
		staleOtelcModules(user, []string{util.OtelcRoot + "/other"}, nil, bundleFingerprint()),
		"an otelc module from a proxy is stale")
}

// TestVendorSyncChecksUserGoSum: the scratch module has the go.sum file of the
// user. A module that the go command gets at the vendored version is checked
// against it: a different content is an error.
func TestVendorSyncChecksUserGoSum(t *testing.T) {
	appDir := newVendorSyncApp(t)
	const badSum = "example.com/dep v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"
	mustWriteFile(t, filepath.Join(appDir, goSumFileName), badSum)
	before := appTree(t, appDir)

	// The hook code needs dep/sub, which vendor/ does not have: otelc gets
	// example.com/dep v1.0.0.
	err := vendorSync(t.Context(), map[string]bool{"example.com/hooksub": true}, appDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
	assert.Equal(t, before, appTree(t, appDir))
	assert.Equal(t, badSum, readAppFile(t, appDir, goSumFileName))
}

func TestWriteScratchGoSum(t *testing.T) {
	moduleDir := t.TempDir()
	scratchDir := t.TempDir()
	require.NoError(t, writeScratchGoSum(moduleDir, scratchDir), "no go.sum: nothing to write")
	assert.NoFileExists(t, filepath.Join(scratchDir, goSumFileName))

	mustWriteFile(t, filepath.Join(moduleDir, goSumFileName), "example.com/dep v1.0.0 h1:user=")
	require.NoError(t, writeScratchGoSum(moduleDir, scratchDir))
	assert.Equal(t, "example.com/dep v1.0.0 h1:user=\n", readAppFile(t, scratchDir, goSumFileName))

	// go mod tidy removed the line of the user, and added a different one.
	mustWriteFile(t, filepath.Join(scratchDir, goSumFileName),
		"example.com/dep v1.0.0 h1:other=\nexample.com/new v1.0.0 h1:new=\n")
	require.NoError(t, writeScratchGoSum(moduleDir, scratchDir))
	assert.Equal(
		t,
		"example.com/dep v1.0.0 h1:user=\nexample.com/dep v1.0.0 h1:other=\nexample.com/new v1.0.0 h1:new=\n",
		readAppFile(t, scratchDir, goSumFileName),
		"the lines of the user come first",
	)

	require.NoError(t, os.Remove(filepath.Join(moduleDir, goSumFileName)))
	require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, goSumFileName), 0o755))
	require.Error(t, writeScratchGoSum(moduleDir, scratchDir), "go.sum is a directory")
}

func TestHookSources(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	moduleDir := filepath.Join(workDir, "app")
	user := mustParseModulesTxt(t, `# example.com/app/nested v0.0.0-00010101000000-000000000000 => ./nested
## explicit
example.com/app/nested
# example.com/abs v0.0.0-00010101000000-000000000000 => `+filepath.ToSlash(filepath.Join(workDir, "abs"))+`
## explicit
example.com/abs
# example.com/wrongmod v0.0.0-00010101000000-000000000000 => ./wrongmod
## explicit
example.com/wrongmod
# example.com/fork v1.0.0 => example.com/forked v1.0.1
## explicit
example.com/fork
# example.com/lib v1.0.0
## explicit
example.com/lib
`)
	mustWriteFile(t, filepath.Join(moduleDir, "nested", goModFileName), "module example.com/app/nested\n")
	mustWriteFile(t, filepath.Join(moduleDir, "nested", "sub", "sub.go"), "package sub\n")
	mustWriteFile(t, filepath.Join(workDir, "abs", goModFileName), "module example.com/abs\n")
	mustWriteFile(t, filepath.Join(moduleDir, "wrongmod", goModFileName), "module example.com/other\n")
	mustWriteFile(t, filepath.Join(moduleDir, "pkg", "pkg.go"), "package pkg\n")
	mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, "example.com", "lib", "lib.go"), "package lib\n")
	instDir := filepath.Join(workDir, util.BuildTempDir, unzippedInstDir)
	mustWriteFile(t, filepath.Join(instDir, goModFileName), "module "+util.OtelcInstRoot+"\n")
	mustWriteFile(t, filepath.Join(instDir, "net", "http", "http.go"), "package http\n")

	localDirs := localModuleDirs(user, moduleDir)
	assert.Equal(t, map[string]string{
		"example.com/app/nested": filepath.Join(moduleDir, "nested"),
		"example.com/abs":        filepath.Join(workDir, "abs"),
	}, localDirs, "only directory replacements with the right module path")

	h := &hookSources{moduleDir: moduleDir, mainPath: "example.com/app", user: user, localDirs: localDirs}
	for importPath, want := range map[string]packageSource{
		"example.com/app/pkg":        {dir: filepath.Join(moduleDir, "pkg"), moduleDir: moduleDir},
		"example.com/app/nested/sub": {dir: filepath.Join(moduleDir, "nested", "sub"), moduleDir: filepath.Join(moduleDir, "nested")},
		"example.com/lib": {
			dir:       filepath.Join(moduleDir, vendorDirName, "example.com", "lib"),
			moduleDir: filepath.Join(moduleDir, vendorDirName, "example.com", "lib"),
		},
		util.OtelcInstRoot + "/net/http": {dir: filepath.Join(instDir, "net", "http"), moduleDir: instDir},
	} {
		got, ok := h.packageDir(importPath)
		assert.True(t, ok, importPath)
		assert.Equal(t, want, got, importPath)
	}
	for _, importPath := range []string{
		"example.com/app/absent", "example.com/unknown", "example.com/fork",
		"example.com/app/../app/pkg", util.OtelcInstRoot + "/../../../app/pkg",
	} {
		_, ok := h.packageDir(importPath)
		assert.False(t, ok, importPath)
	}
	assert.True(t, h.isMainPackage("example.com/app/pkg"))
	assert.True(t, h.isMainPackage("example.com/app"))
	assert.False(t, h.isMainPackage("example.com/app/nested/sub"), "the longest module path wins")
	assert.False(t, h.isMainPackage("example.com/application"))
	assert.False(t, h.isMainPackage("example.com/lib"))

	assert.True(t, isDirReplacement("./x"))
	assert.True(t, isDirReplacement("../x"))
	assert.True(t, isDirReplacement("/abs/x"))
	assert.False(t, isDirReplacement(""))
	assert.False(t, isDirReplacement("example.com/forked v1.0.1"))
}

func TestPackageImports(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package a\n\nimport (\n\t\"fmt\"\n\t_ \"example.com/a\"\n)\n")
	mustWriteFile(t, filepath.Join(dir, "linux.go"),
		"//go:build linux && !cgo\n\npackage a\n\nimport _ \"example.com/linux\"\n")
	mustWriteFile(t, filepath.Join(dir, "notignore.go"),
		"//go:build !ignore\n\npackage a\n\nimport _ \"example.com/notignore\"\n")
	mustWriteFile(t, filepath.Join(dir, "ignore.go"),
		"// A comment.\n\n//go:build ignore\n\npackage main\n\nimport _ \"example.com/ignored\"\n")
	mustWriteFile(t, filepath.Join(dir, "ignoreor.go"),
		"//go:build ignore || tools\n\npackage a\n\nimport _ \"example.com/tools\"\n")
	mustWriteFile(t, filepath.Join(dir, "a_test.go"), "package a\n\nimport _ \"example.com/test\"\n")
	mustWriteFile(t, filepath.Join(dir, "_hidden.go"), "package a\n\nimport _ \"example.com/hidden\"\n")
	mustWriteFile(t, filepath.Join(dir, "notes.txt"), "import _ \"example.com/txt\"\n")

	imports, err := packageImports(dir)
	require.NoError(t, err)
	slices.Sort(imports)
	assert.Equal(t, []string{"example.com/a", "example.com/linux", "example.com/notignore", "example.com/tools", "fmt"},
		imports, "all the build tags but ignore; no test files")

	imports, err = packageImports(filepath.Join(dir, "absent"))
	require.NoError(t, err)
	assert.Empty(t, imports)

	mustWriteFile(t, filepath.Join(dir, "bad.go"), "not Go")
	_, err = packageImports(dir)
	require.Error(t, err)
}

// TestVendorScratchImportsNestedToolFile: the tool file in the module of an
// instrumentation package adds its imports, and their rules, like
// walkInstrumentation does. The tool file imports are the instrumentation
// packages (toolImports).
func TestVendorScratchImportsNestedToolFile(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	moduleDir := filepath.Join(workDir, "app")
	localDir := filepath.Join(moduleDir, "local")
	mustWriteFile(t, filepath.Join(localDir, goModFileName), "module example.com/local\n")
	mustWriteFile(t, filepath.Join(localDir, toolFileCanonical),
		"//go:build tools\n\npackage tools\n\nimport _ \"example.com/other\"\n")
	mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, "example.com", "other", "otelc.yaml"),
		localInstrumentationRules("example.com/other/hooks"))
	user := mustParseModulesTxt(t, "# example.com/local v0.0.0-00010101000000-000000000000 => ./local\n"+
		"## explicit\nexample.com/local\n# example.com/other v1.0.0\n## explicit\nexample.com/other\n")
	h := &hookSources{
		moduleDir: moduleDir, mainPath: "example.com/app", user: user,
		localDirs: localModuleDirs(user, moduleDir),
	}

	got, err := vendorScratchImports(map[string]bool{"example.com/local": true}, h)
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com/local", "example.com/other", "example.com/other/hooks"}, got.packages)
	assert.Equal(t, map[string]bool{"example.com/local": true, "example.com/other": true}, got.toolImports)

	mustWriteFile(t, filepath.Join(localDir, toolFileCanonical),
		"//go:build tools\n\npackage tools\n\nimport x \"example.com/other\"\n\nvar _ = x.V\n")
	_, err = vendorScratchImports(map[string]bool{"example.com/local": true}, h)
	require.Error(t, err, "a named import in a tool file")
}

// TestExpandMainPackagesWithoutDot: the path of the main module can have no
// dot. Its packages are not packages of the standard library.
func TestExpandMainPackagesWithoutDot(t *testing.T) {
	moduleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(moduleDir, "instr", "instr.go"),
		"package instr\n\nimport (\n\t\"fmt\"\n\n\t_ \"myapp/instr/shared\"\n)\n\nvar _ = fmt.Sprint\n")
	mustWriteFile(t, filepath.Join(moduleDir, "instr", "shared", "shared.go"),
		"package shared\n\nimport _ \"example.com/lib\"\n")
	h := &hookSources{moduleDir: moduleDir, mainPath: "myapp", user: &modulesTxt{}}

	got, err := vendorScratchImports(map[string]bool{"myapp/instr": true}, h)
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com/lib"}, got.packages)
}

// TestPackageImportsBuildConstraints: packageImports reads the build
// constraints like "go mod vendor": both the //go:build form and the legacy
// "// +build" form. A file with the "ignore" tag has no imports. All the other
// tags (and their negations) match.
func TestPackageImportsBuildConstraints(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"plain.go":          "package p\n\nimport _ \"example.com/plain\"\n",
		"goignore.go":       "//go:build ignore\n\npackage p\n\nimport _ \"example.com/goignore\"\n",
		"plusignore.go":     "// +build ignore\n\npackage p\n\nimport _ \"example.com/plusignore\"\n",
		"plusignoreand.go":  "// +build linux\n// +build ignore\n\npackage p\n\nimport _ \"example.com/plusignoreand\"\n",
		"pluslinux.go":      "// +build linux,!windows\n\npackage p\n\nimport _ \"example.com/pluslinux\"\n",
		"plusnotignore.go":  "// +build !ignore\n\npackage p\n\nimport _ \"example.com/plusnotignore\"\n",
		"gobuildwins.go":    "//go:build linux\n// +build ignore\n\npackage p\n\nimport _ \"example.com/gobuildwins\"\n",
		"plusdoccomment.go": "// +build ignore\npackage p\n\nimport _ \"example.com/plusdoccomment\"\n",
	}
	for name, content := range files {
		mustWriteFile(t, filepath.Join(dir, name), content)
	}

	imports, err := packageImports(dir)
	require.NoError(t, err)
	slices.Sort(imports)
	assert.Equal(t, []string{
		"example.com/gobuildwins",
		"example.com/plain",
		"example.com/plusdoccomment",
		"example.com/pluslinux",
		"example.com/plusnotignore",
	}, imports)
}

// newMergeTestModule makes a module with one vendored module example.com/a,
// and a scratch module. It returns the module directory, the scratch
// module, and the input of mergeScratchModules.
func newMergeTestModule(t *testing.T) (string, *vendorScratch, vendorSyncInput) {
	t.Helper()
	const modulesTxt = "# example.com/a v1.0.0\n## explicit\nexample.com/a\n"
	moduleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, modulesTxtFileName), modulesTxt)
	mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, "example.com", "a", "a.go"), "package a")
	mustWriteFile(t, filepath.Join(moduleDir, goModFileName), "module example.com/app\n\ngo 1.21\n")
	scratch := &vendorScratch{
		dir:      t.TempDir(),
		userMods: map[string]bool{},
		updated:  map[string]bool{},
	}
	return moduleDir, scratch, vendorSyncInput{
		moduleDir:   moduleDir,
		user:        mustParseModulesTxt(t, modulesTxt),
		stale:       map[string]bool{},
		fingerprint: "fingerprint",
	}
}

func parseTestGoMod(t *testing.T, content string) *modfile.File {
	t.Helper()
	mf, err := modfile.Parse(goModFileName, []byte(content), nil)
	require.NoError(t, err)
	return mf
}

// TestMergeScratchModulesErrors: each failure of the merge stops it with an
// error, and the module of the user does not change.
func TestMergeScratchModulesErrors(t *testing.T) {
	scratchModulesTxt := func(t *testing.T, s *vendorScratch, content string) {
		t.Helper()
		mustWriteFile(t, filepath.Join(s.dir, vendorDirName, modulesTxtFileName), content)
	}

	t.Run("the scratch module has no modules.txt", func(t *testing.T) {
		moduleDir, scratch, in := newMergeTestModule(t)
		before := appTree(t, moduleDir)
		changed, err := mergeScratchModules(t.Context(), in, scratch, parseTestGoMod(t, "module example.com/app\n"))
		require.Error(t, err)
		assert.False(t, changed)
		assert.Contains(t, err.Error(), modulesTxtFileName)
		assert.Equal(t, before, appTree(t, moduleDir))
	})

	t.Run("nothing to add", func(t *testing.T) {
		moduleDir, scratch, in := newMergeTestModule(t)
		scratchModulesTxt(t, scratch, "# example.com/a v1.0.0\n## explicit\nexample.com/a\n")
		before := appTree(t, moduleDir)
		changed, err := mergeScratchModules(t.Context(), in, scratch, parseTestGoMod(t, "module example.com/app\n"))
		require.NoError(t, err)
		assert.False(t, changed, "the scratch module has the same modules")
		assert.Equal(t, before, appTree(t, moduleDir))
	})

	t.Run("a higher version is a conflict", func(t *testing.T) {
		moduleDir, scratch, in := newMergeTestModule(t)
		scratchModulesTxt(t, scratch, "# example.com/a v1.2.0\n## explicit\nexample.com/a\n")
		before := appTree(t, moduleDir)
		_, err := mergeScratchModules(t.Context(), in, scratch, parseTestGoMod(t, "module example.com/app\n"))
		var conflict *vendorConflictError
		require.ErrorAs(t, err, &conflict)
		assert.Equal(t, "example.com/a", conflict.Module)
		assert.Equal(t, "v1.0.0", conflict.VendoredVersion)
		assert.Equal(t, "v1.2.0", conflict.RequiredVersion)
		assert.Equal(t, before, appTree(t, moduleDir))
	})

	t.Run("the files of a stale module cannot be read", func(t *testing.T) {
		skipWithoutSymlinks(t)
		moduleDir, scratch, in := newMergeTestModule(t)
		scratchModulesTxt(t, scratch, "# example.com/a v1.0.0\n## explicit\nexample.com/a\n")
		require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "outside.go"),
			filepath.Join(moduleDir, vendorDirName, "example.com", "a", "link.go")))
		in.stale = map[string]bool{"example.com/a": true}
		before := appTree(t, moduleDir)
		_, err := mergeScratchModules(t.Context(), in, scratch, parseTestGoMod(t, "module example.com/app\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symbolic link")
		assert.Equal(t, before, appTree(t, moduleDir))
	})

	t.Run("go.sum is a directory", func(t *testing.T) {
		moduleDir, scratch, in := newMergeTestModule(t)
		scratchModulesTxt(t, scratch, "# example.com/a v1.0.0\n## explicit\nexample.com/a\n"+
			"# example.com/b v1.0.0\n## explicit\nexample.com/b\n")
		require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, goSumFileName), 0o755))
		before := appTree(t, moduleDir)
		_, err := mergeScratchModules(contextWithVendorUpdate(t.Context()), in, scratch,
			parseTestGoMod(t, "module example.com/app\n"))
		require.Error(t, err, "otelc vendor reads go.sum")
		assert.Contains(t, err.Error(), goSumFileName)
		assert.Equal(t, before, appTree(t, moduleDir))
	})
}

// TestDropStaleReplacesUnknownModule: a stale module that modules.txt does
// not have has no replace to remove. The other replaces stay.
func TestDropStaleReplacesUnknownModule(t *testing.T) {
	mf := parseTestGoMod(t, "module example.com/app\n\nreplace example.com/a v1.0.0 => ./a\n")
	user := mustParseModulesTxt(t, "# example.com/a v1.0.0 => ./a\n## explicit\nexample.com/a\n")
	dropStaleReplaces(mf, user, map[string]bool{"example.com/absent": true})
	require.Len(t, mf.Replace, 1)
	assert.Equal(t, "example.com/a", mf.Replace[0].Old.Path)

	dropStaleReplaces(mf, user, map[string]bool{"example.com/a": true})
	assert.Empty(t, mf.Replace, "the replace of the stale module is removed")
}

// TestVendoredModuleFilesErrors: vendoredModuleFiles fails when it cannot
// read a vendored directory, and returns each file one time.
func TestVendoredModuleFilesErrors(t *testing.T) {
	t.Run("a package in two modules", func(t *testing.T) {
		moduleDir := t.TempDir()
		file := filepath.Join(moduleDir, vendorDirName, "example.com", "a", "b", "b.go")
		mustWriteFile(t, file, "package b")
		user := mustParseModulesTxt(t, "# example.com/a v1.0.0\n## explicit\nexample.com/a/b\n"+
			"# example.com/a/b v1.0.0\n## explicit\nexample.com/a/b\n")
		files, err := vendoredModuleFiles(user,
			map[string]bool{"example.com/a": true, "example.com/a/b": true}, moduleDir)
		require.NoError(t, err)
		assert.Equal(t, []string{file}, files)
	})

	t.Run("a link in a package", func(t *testing.T) {
		skipWithoutSymlinks(t)
		moduleDir := t.TempDir()
		dir := filepath.Join(moduleDir, vendorDirName, "example.com", "a")
		mustWriteFile(t, filepath.Join(dir, "a.go"), "package a")
		require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "outside.go"), filepath.Join(dir, "link.go")))
		user := mustParseModulesTxt(t, "# example.com/a v1.0.0\n## explicit\nexample.com/a\n")
		_, err := vendoredModuleFiles(user, map[string]bool{"example.com/a": true}, moduleDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symbolic link")
	})

	t.Run("a parent directory cannot be read", func(t *testing.T) {
		skipWithoutPermissions(t)
		moduleDir := t.TempDir()
		root := filepath.Join(moduleDir, vendorDirName, "example.com", "a")
		mustWriteFile(t, filepath.Join(root, "LICENSE"), "license")
		mustWriteFile(t, filepath.Join(root, "sub", "sub.go"), "package sub")
		// The directory can be crossed, but not listed.
		require.NoError(t, os.Chmod(root, 0o300))
		t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
		user := mustParseModulesTxt(t, "# example.com/a v1.0.0\n## explicit\nexample.com/a/sub\n")
		_, err := vendoredModuleFiles(user, map[string]bool{"example.com/a": true}, moduleDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading "+root)
	})
}

// TestMergedGoSumReadErrors: a go.sum that otelc cannot read is an error.
func TestMergedGoSumReadErrors(t *testing.T) {
	moduleDir := t.TempDir()
	scratchDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, goSumFileName), 0o755))
	sum, err := mergedGoSum(moduleDir, scratchDir)
	require.Error(t, err, "the go.sum of the user is a directory")
	assert.Nil(t, sum)

	require.NoError(t, os.Remove(filepath.Join(moduleDir, goSumFileName)))
	require.NoError(t, os.MkdirAll(filepath.Join(scratchDir, goSumFileName), 0o755))
	sum, err = mergedGoSum(moduleDir, scratchDir)
	require.Error(t, err, "the go.sum of the scratch module is a directory")
	assert.Nil(t, sum)
}

// TestWriteScratchGoSumScratchIsDirectory: when the go.sum of the scratch
// module cannot be read, the go.sum of the user does not go into it.
func TestWriteScratchGoSumScratchIsDirectory(t *testing.T) {
	moduleDir := t.TempDir()
	scratchDir := t.TempDir()
	mustWriteFile(t, filepath.Join(moduleDir, goSumFileName), "example.com/dep v1.0.0 h1:user=\n")
	require.NoError(t, os.MkdirAll(filepath.Join(scratchDir, goSumFileName), 0o755))
	require.Error(t, writeScratchGoSum(moduleDir, scratchDir))
	info, err := os.Stat(filepath.Join(scratchDir, goSumFileName))
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "the directory stays")
}

// TestApplyVendorMergeCopyErrors: a merge that cannot plan or make the
// copies fails before it writes modules.txt and go.mod.
func TestApplyVendorMergeCopyErrors(t *testing.T) {
	const modulesTxt = "# example.com/a v1.0.0\n## explicit\nexample.com/a\n"
	newModule := func(t *testing.T) (string, string) {
		t.Helper()
		moduleDir := t.TempDir()
		mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, modulesTxtFileName), modulesTxt)
		mustWriteFile(t, filepath.Join(moduleDir, goModFileName), "module example.com/app\n")
		scratchVendor := t.TempDir()
		mustWriteFile(t, filepath.Join(scratchVendor, "example.com", "b", "b.go"), "package b")
		return moduleDir, scratchVendor
	}
	merged := modulesTxt + "# example.com/b v1.0.0\n## explicit\nexample.com/b\n"

	t.Run("a link in the scratch module", func(t *testing.T) {
		skipWithoutSymlinks(t)
		moduleDir, scratchVendor := newModule(t)
		require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "outside.go"),
			filepath.Join(scratchVendor, "example.com", "b", "link.go")))
		before := appTree(t, moduleDir)
		err := applyVendorMerge(t.Context(), moduleDir, scratchVendor, vendorMergeResult{
			goMod:  parseTestGoMod(t, "module example.com/app\n"),
			merged: mustParseModulesTxt(t, merged),
			added:  []string{"example.com/b"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symbolic link")
		assert.Equal(t, before, appTree(t, moduleDir))
	})

	t.Run("a directory cannot be made", func(t *testing.T) {
		skipWithoutPermissions(t)
		moduleDir, scratchVendor := newModule(t)
		makeReadOnly(t, filepath.Join(moduleDir, vendorDirName))
		err := applyVendorMerge(t.Context(), moduleDir, scratchVendor, vendorMergeResult{
			goMod:  parseTestGoMod(t, "module example.com/app\n"),
			merged: mustParseModulesTxt(t, merged),
			added:  []string{"example.com/b"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "creating "+filepath.Join(moduleDir, vendorDirName, "example.com"))
		assert.Equal(t, modulesTxt, readAppFile(t, moduleDir, filepath.Join(vendorDirName, modulesTxtFileName)))
	})

	t.Run("go.sum cannot be written", func(t *testing.T) {
		moduleDir, scratchVendor := newModule(t)
		goSum := filepath.Join(moduleDir, goSumFileName)
		mustWriteFile(t, filepath.Join(goSum, "keep"), "a file in a directory")
		err := applyVendorMerge(t.Context(), moduleDir, scratchVendor, vendorMergeResult{
			goMod:  parseTestGoMod(t, "module example.com/app\n"),
			goSum:  []byte("example.com/b v1.0.0 h1:b=\n"),
			merged: mustParseModulesTxt(t, merged),
			added:  []string{"example.com/b"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "writing "+goSum)
		assert.FileExists(t, filepath.Join(goSum, "keep"))
	})
}

// TestVendorScratchImportsErrors: a package of the hook code that otelc
// cannot read stops vendorScratchImports with an error.
func TestVendorScratchImportsErrors(t *testing.T) {
	newSources := func(t *testing.T) *hookSources {
		t.Helper()
		moduleDir := t.TempDir()
		mustWriteFile(t, filepath.Join(moduleDir, "instr", "instr.go"),
			"package instr\n\nimport _ \"example.com/lib\"\n")
		return &hookSources{moduleDir: moduleDir, mainPath: "example.com/app", user: &modulesTxt{}}
	}
	imports := map[string]bool{"example.com/app/instr": true}

	t.Run("the tool file imports a package again", func(t *testing.T) {
		h := newSources(t)
		mustWriteFile(t, filepath.Join(h.moduleDir, toolFileCanonical),
			"//go:build tools\n\npackage tools\n\nimport _ \"example.com/app/instr\"\n")
		got, err := vendorScratchImports(imports, h)
		require.NoError(t, err)
		assert.Equal(t, []string{"example.com/lib"}, got.packages, "each package is read one time")
		assert.Equal(t, map[string]bool{"example.com/app/instr": true}, got.toolImports)
	})

	t.Run("a directory of the package cannot be read", func(t *testing.T) {
		skipWithoutPermissions(t)
		h := newSources(t)
		locked := filepath.Join(h.moduleDir, "instr", "locked")
		require.NoError(t, os.MkdirAll(locked, 0o755))
		require.NoError(t, os.Chmod(locked, 0o000))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		_, err := vendorScratchImports(imports, h)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to walk directory")
	})

	t.Run("two tool files", func(t *testing.T) {
		h := newSources(t)
		mustWriteFile(t, filepath.Join(h.moduleDir, toolFileCanonical), "package tools\n")
		mustWriteFile(t, filepath.Join(h.moduleDir, toolFileAlias), "package tools\n")
		_, err := vendorScratchImports(imports, h)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only one instrumentation config file is allowed")
	})

	t.Run("the tool file is not Go", func(t *testing.T) {
		h := newSources(t)
		mustWriteFile(t, filepath.Join(h.moduleDir, toolFileCanonical), "not Go")
		_, err := vendorScratchImports(imports, h)
		require.Error(t, err)
		assert.Contains(t, err.Error(), toolFileCanonical)
	})

	t.Run("a file of a main package is not Go", func(t *testing.T) {
		h := newSources(t)
		bad := filepath.Join(h.moduleDir, "instr", "bad.go")
		mustWriteFile(t, bad, "not Go")
		_, err := vendorScratchImports(imports, h)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing the imports of "+bad)
	})
}

// TestCollectRulePackagesBadAddedFile: the file that a file rule adds must
// be a Go file.
func TestCollectRulePackagesBadAddedFile(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	bad := filepath.Join(workDir, util.BuildTempDir, unzippedInstDir, "init", "init.go")
	mustWriteFile(t, bad, "not Go")
	ruleFile := filepath.Join(workDir, "rules.yaml")
	mustWriteFile(t, ruleFile, `version: "v1.0.0"
file_rule:
  target: main
  do:
    - add_file:
        file: init.go
        path: "`+util.OtelcInstRoot+`/init"
`)
	set := map[string]bool{}
	err := collectRulePackages(ruleFile, set, &hookSources{user: &modulesTxt{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing the imports of "+bad)
}

// TestExpandMainPackagesSharedImport: two main packages import the same main
// package. Its imports are read one time.
func TestExpandMainPackagesSharedImport(t *testing.T) {
	moduleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(moduleDir, "a", "a.go"), "package a\n\nimport _ \"example.com/app/shared\"\n")
	mustWriteFile(t, filepath.Join(moduleDir, "b", "b.go"), "package b\n\nimport _ \"example.com/app/shared\"\n")
	mustWriteFile(t, filepath.Join(moduleDir, "shared", "shared.go"),
		"package shared\n\nimport _ \"example.com/lib\"\n")
	h := &hookSources{moduleDir: moduleDir, mainPath: "example.com/app", user: &modulesTxt{}}

	set := map[string]bool{"example.com/app/a": true, "example.com/app/b": true}
	require.NoError(t, h.expandMainPackages(set))
	assert.Equal(t, map[string]bool{"example.com/lib": true}, set)
}

// TestPackageImportsMoreCases: a comment after the package clause is not a
// build constraint, a bad //go:build line does not exclude the file, and a
// file is not a package directory.
func TestPackageImportsMoreCases(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "after.go"),
		"package p\n\n// +build ignore\n\nimport _ \"example.com/after\"\n")
	mustWriteFile(t, filepath.Join(dir, "badtag.go"), "//go:build (\n\npackage p\n\nimport _ \"example.com/badtag\"\n")

	imports, err := packageImports(dir)
	require.NoError(t, err)
	slices.Sort(imports)
	assert.Equal(t, []string{"example.com/after", "example.com/badtag"}, imports)

	if runtime.GOOS != "windows" {
		// On Windows, reading a file as a directory gives a "not found" error.
		file := filepath.Join(dir, "after.go")
		_, err = packageImports(file)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading "+file)
	}
}

// TestBuildVendorScratchOldScratchStays: when otelc cannot remove the old
// scratch module, it stops with an error.
func TestBuildVendorScratchOldScratchStays(t *testing.T) {
	skipWithoutPermissions(t)
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	dir := util.GetBuildTemp(vendorScratchDir)
	mustWriteFile(t, filepath.Join(dir, "sub", "old.go"), "package old")
	makeReadOnly(t, filepath.Join(dir, "sub"))

	_, err := buildVendorScratch(t.Context(), vendorScratchInput{user: &modulesTxt{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing the old scratch module "+dir)
	assert.FileExists(t, filepath.Join(dir, "sub", "old.go"))
}

// TestCopyVendoredModulesWithoutOriginal: without the full modules.txt
// (original), the copies use the modules.txt of the user.
func TestCopyVendoredModulesWithoutOriginal(t *testing.T) {
	moduleDir := t.TempDir()
	mustWriteFile(t, filepath.Join(moduleDir, vendorDirName, "example.com", "a", "a.go"), "package a")
	scratchDir := t.TempDir()
	userMods, copies, err := copyVendoredModules(scratchDir, vendorScratchInput{
		user:          mustParseModulesTxt(t, "# example.com/a v1.0.0\n## explicit\nexample.com/a\n"),
		userVendorDir: filepath.Join(moduleDir, vendorDirName),
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"example.com/a": true}, userMods)
	require.Contains(t, copies, "example.com/a")
	assert.FileExists(t, filepath.Join(scratchDir, filepath.FromSlash(copies["example.com/a"]), "a.go"))
}

// TestRunScratchRoundWriteErrors: a scratch module that otelc cannot write
// stops the round before the go commands run.
func TestRunScratchRoundWriteErrors(t *testing.T) {
	newScratch := func(t *testing.T) *vendorScratch {
		t.Helper()
		return &vendorScratch{
			dir:      t.TempDir(),
			userMods: map[string]bool{},
			versions: map[string]string{},
			copies:   map[string]string{},
			pinned:   map[string]bool{},
			updated:  map[string]bool{},
			extra:    map[string]bool{},
		}
	}
	newInput := func(t *testing.T) vendorScratchInput {
		t.Helper()
		return vendorScratchInput{
			user:          &modulesTxt{},
			userVendorDir: filepath.Join(t.TempDir(), vendorDirName),
			imports:       []string{"example.com/hook"},
		}
	}

	t.Run("the source file", func(t *testing.T) {
		s := newScratch(t)
		s.dir = filepath.Join(s.dir, "absent")
		done, err := runScratchRound(t.Context(), s, newInput(t))
		require.Error(t, err)
		assert.False(t, done)
		assert.NoDirExists(t, s.dir)
	})

	t.Run("a bad go version", func(t *testing.T) {
		s := newScratch(t)
		in := newInput(t)
		in.goVersion = "not a version"
		done, err := runScratchRound(t.Context(), s, in)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a version")
		assert.False(t, done)
		assert.NoFileExists(t, filepath.Join(s.dir, goModFileName))
	})

	t.Run("the go.sum of the user is a directory", func(t *testing.T) {
		s := newScratch(t)
		in := newInput(t)
		require.NoError(t, os.MkdirAll(filepath.Join(filepath.Dir(in.userVendorDir), goSumFileName), 0o755))
		done, err := runScratchRound(t.Context(), s, in)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading "+filepath.Join(filepath.Dir(in.userVendorDir), goSumFileName))
		assert.False(t, done)
		assert.NoFileExists(t, filepath.Join(s.dir, goSumFileName))
	})
}

// TestRaisedModulesGoFails: when the go command fails, raisedModules
// returns its error.
func TestRaisedModulesGoFails(t *testing.T) {
	s := &vendorScratch{dir: filepath.Join(t.TempDir(), "absent"), userMods: map[string]bool{}}
	raised, err := raisedModules(t.Context(), s)
	require.Error(t, err)
	assert.Empty(t, raised)
	assert.Contains(t, err.Error(), "go list")
}
