// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"

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
	assert.True(t, vendorMode(t.Context(), root, "build", nil))
	assert.False(t, vendorMode(t.Context(), root, "build", []string{"-mod=mod"}))
	assert.False(t, vendorMode(contextWithVendorMode(t.Context(), false), root, "build", nil),
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
	mustWriteFile(t, ruleFile, `version: "v1.0.0"
hook_rule:
  target: example.com/lib
  do:
    - inject_hooks:
        before: Before
        path: "example.com/hooks"
raw_rule:
  target: example.com/lib
  imports:
    ctx: "context"
    extra: "example.com/extra"
file_rule:
  target: main
  do:
    - add_file:
        file: init.go
        path: "`+util.OtelcInstRoot+`/init"
`)
	set := map[string]bool{}
	require.NoError(t, collectRulePackages(ruleFile, set))
	assert.Equal(t, map[string]bool{
		"example.com/hooks":            true,
		"context":                      true,
		"example.com/extra":            true,
		util.OtelcInstRoot + "/init":   true,
		util.OtelcPkgRoot + "/runtime": true,
	}, set)
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
	needed, err := vendorScratchImports(imports)
	require.NoError(t, err)
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
		assert.False(t, vendorMode(t.Context(), root, "build", nil), goflags)
	}
	t.Setenv("GOFLAGS", `'-mod=vendor'`)
	assert.True(t, vendorMode(t.Context(), root, "build", nil))
}
