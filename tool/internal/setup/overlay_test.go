// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestImportsFileSource(t *testing.T) {
	f, err := parser.ParseFile(
		token.NewFileSet(),
		"x.go",
		importsFileSource("foo", []string{"b", "a"}),
		parser.ImportsOnly,
	)
	require.NoError(t, err)

	assert.Equal(t, "foo", f.Name.Name)
	require.Len(t, f.Imports, 2)
	for i, want := range []string{"a", "b"} {
		assert.Equal(t, strconv.Quote(want), f.Imports[i].Path.Value)
		assert.Equal(t, "_", f.Imports[i].Name.Name)
	}
}

func readOverlayFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestBuildImportOverlayInPlace(t *testing.T) {
	buildDir := t.TempDir()
	pkgDir := t.TempDir()

	o, err := buildImportOverlay([]*importTarget{
		{ImportPath: "example.com/a", Name: "a", Dir: pkgDir, Kind: kindInPlace, Imports: []string{"container/list"}},
	}, filepath.Join(t.TempDir(), "go.mod"), buildDir)
	require.NoError(t, err)

	require.Len(t, o.Replace, 1)
	src := o.Replace[filepath.Join(pkgDir, "otelc.imports.go")]
	require.NotEmpty(t, src)
	assert.Equal(t, string(importsFileSource("a", []string{"container/list"})), readOverlayFile(t, src))
}

func TestBuildImportOverlayXTestName(t *testing.T) {
	pkgDir := t.TempDir()

	o, err := buildImportOverlay([]*importTarget{
		{ImportPath: "example.com/a_test", Name: "a_test", Dir: pkgDir, Kind: kindInPlace, Imports: []string{"fmt"}},
	}, filepath.Join(t.TempDir(), "go.mod"), t.TempDir())
	require.NoError(t, err)

	assert.Contains(t, o.Replace, filepath.Join(pkgDir, "otelc.imports_test.go"))
}

func TestBuildImportOverlaySkipsRuntimeFileTargets(t *testing.T) {
	o, err := buildImportOverlay([]*importTarget{
		{
			ImportPath: "command-line-arguments",
			Name:       "main",
			Dir:        t.TempDir(),
			Kind:       kindRuntimeFile,
			Imports:    []string{"fmt"},
		},
	}, filepath.Join(t.TempDir(), "go.mod"), t.TempDir())
	require.NoError(t, err)

	assert.Empty(t, o.Replace)
}

// fakeModCacheModule writes a module the way the module cache lays it out.
func fakeModCacheModule(t *testing.T, withGoMod bool) moduleRoot {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "example.com", "dep@v1.2.3")
	if withGoMod {
		mustWriteFile(t, filepath.Join(dir, "go.mod"), "module example.com/dep\n")
	}
	mustWriteFile(t, filepath.Join(dir, "x.go"), "package dep\n")
	mustWriteFile(t, filepath.Join(dir, "sub", "y.go"), "package sub\n")
	mustWriteFile(t, filepath.Join(dir, "testdata", "z.txt"), "z\n")
	return moduleRoot{Path: "example.com/dep", Version: "v1.2.3", Dir: dir}
}

func TestBuildImportOverlayModCache(t *testing.T) {
	buildDir := t.TempDir()
	mod := fakeModCacheModule(t, true)
	goMod := filepath.Join(t.TempDir(), "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n\ngo 1.25\n\nrequire example.com/dep v1.2.3\n")

	o, err := buildImportOverlay([]*importTarget{{
		ImportPath: "example.com/dep/sub", Name: "sub", Dir: filepath.Join(mod.Dir, "sub"),
		Kind: kindModCache, Module: mod, Imports: []string{"container/list"},
	}}, goMod, buildDir)
	require.NoError(t, err)

	replaceDir := filepath.Join(buildDir, "modreplace", "example.com", "dep@v1.2.3")
	for _, rel := range []string{"go.mod", "x.go", filepath.Join("sub", "y.go"), filepath.Join("testdata", "z.txt")} {
		assert.Equal(t, filepath.Join(mod.Dir, rel), o.Replace[filepath.Join(replaceDir, rel)], rel)
	}
	assert.DirExists(t, filepath.Join(replaceDir, "sub"))
	assert.DirExists(t, filepath.Join(replaceDir, "testdata"))
	assert.NoFileExists(t, filepath.Join(replaceDir, "x.go"), "module files must come from the overlay, not copies")

	imports := o.Replace[filepath.Join(replaceDir, "sub", "otelc.imports.go")]
	assert.Equal(t, string(importsFileSource("sub", []string{"container/list"})), readOverlayFile(t, imports))

	assert.Contains(t, readOverlayFile(t, o.Replace[goMod]), "replace example.com/dep => "+replaceDir)
}

func TestBuildImportOverlayModCacheNoGoMod(t *testing.T) {
	buildDir := t.TempDir()
	mod := fakeModCacheModule(t, false)
	goMod := filepath.Join(t.TempDir(), "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")

	o, err := buildImportOverlay([]*importTarget{
		{
			ImportPath: "example.com/dep",
			Name:       "dep",
			Dir:        mod.Dir,
			Kind:       kindModCache,
			Module:     mod,
			Imports:    []string{"fmt"},
		},
	}, goMod, buildDir)
	require.NoError(t, err)

	replaceDir := filepath.Join(buildDir, "modreplace", "example.com", "dep@v1.2.3")
	assert.Equal(t, "module example.com/dep\n", readOverlayFile(t, o.Replace[filepath.Join(replaceDir, "go.mod")]))
}

func TestBuildImportOverlayModCacheReplacedModule(t *testing.T) {
	buildDir := t.TempDir()
	mod := fakeModCacheModule(t, true)
	goMod := filepath.Join(t.TempDir(), "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n\nrequire example.com/orig v1.0.0\n\n"+
		"replace example.com/orig => example.com/dep v1.2.3\n")

	o, err := buildImportOverlay([]*importTarget{
		{
			ImportPath: "example.com/orig",
			Name:       "dep",
			Dir:        mod.Dir,
			Kind:       kindModCache,
			Module:     mod,
			Imports:    []string{"fmt"},
		},
	}, goMod, buildDir)
	require.NoError(t, err)

	replaceDir := filepath.Join(buildDir, "modreplace", "example.com", "dep@v1.2.3")
	assert.Contains(t, readOverlayFile(t, o.Replace[goMod]), "replace example.com/orig => "+replaceDir,
		"the replace must apply to the module path the build list uses")
}

func TestBuildImportOverlayDeterministic(t *testing.T) {
	buildDir := t.TempDir()
	mod := fakeModCacheModule(t, true)
	goMod := filepath.Join(t.TempDir(), "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")
	targets := []*importTarget{
		{
			ImportPath: "example.com/dep",
			Name:       "dep",
			Dir:        mod.Dir,
			Kind:       kindModCache,
			Module:     mod,
			Imports:    []string{"fmt"},
		},
		{ImportPath: "example.com/a", Name: "a", Dir: t.TempDir(), Kind: kindInPlace, Imports: []string{"fmt"}},
	}

	first, err := buildImportOverlay(targets, goMod, buildDir)
	require.NoError(t, err)
	second, err := buildImportOverlay(targets, goMod, buildDir)
	require.NoError(t, err)

	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	assert.JSONEq(t, string(a), string(b))
}

func TestWriteImportOverlay(t *testing.T) {
	buildDir := t.TempDir()

	path, err := writeImportOverlay(overlayFile{}, buildDir)
	require.NoError(t, err)
	assert.Empty(t, path)
	assert.NoFileExists(t, filepath.Join(buildDir, overlayFileName))

	path, err = writeImportOverlay(overlayFile{Replace: map[string]string{"/a.go": "/b.go"}}, buildDir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(buildDir, overlayFileName), path)
	assert.JSONEq(t, `{"Replace":{"/a.go":"/b.go"}}`, readOverlayFile(t, path))
}

func TestWriteImportOverlayRemovesStaleFile(t *testing.T) {
	buildDir := t.TempDir()
	stale := filepath.Join(buildDir, overlayFileName)
	mustWriteFile(t, stale, `{"Replace":{"/old.go":"/x.go"}}`)

	path, err := writeImportOverlay(overlayFile{}, buildDir)
	require.NoError(t, err)
	assert.Empty(t, path)
	assert.NoFileExists(t, stale, "an overlay left from an earlier setup must not reach the next build")
}

func TestMergeUserOverlay(t *testing.T) {
	user := filepath.Join(t.TempDir(), "user.json")
	mustWriteFile(t, user, `{"Replace":{"/app/a.go":"/tmp/a.go"}}`)
	ours := overlayFile{Replace: map[string]string{"/app/otelc.imports.go": "/b/x.go"}}

	merged, err := mergeUserOverlay(ours, user)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"/app/a.go": "/tmp/a.go", "/app/otelc.imports.go": "/b/x.go"}, merged.Replace)

	mustWriteFile(t, user, `{"Replace":{"/app/otelc.imports.go":"/tmp/mine.go"}}`)
	_, err = mergeUserOverlay(ours, user)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/app/otelc.imports.go")
}

func TestMergeUserOverlayNoUserFile(t *testing.T) {
	ours := overlayFile{Replace: map[string]string{"/a": "/b"}}

	merged, err := mergeUserOverlay(ours, "")
	require.NoError(t, err)
	assert.Equal(t, ours, merged)
}

func TestStripOverlayFlag(t *testing.T) {
	assert.Equal(t,
		[]string{"-C", "dir", "-v", "./..."},
		stripOverlayFlag([]string{"-C", "dir", "-overlay", "u.json", "-v", "--overlay=v.json", "./..."}))
	assert.Equal(t,
		[]string{"./pkg", "-args", "-overlay", "x"},
		stripOverlayFlag([]string{"-overlay=u.json", "./pkg", "-args", "-overlay", "x"}),
		"arguments after -args belong to the test binary")
}

func TestUserOverlayPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOFLAGS", "")

	abs := filepath.Join(t.TempDir(), "u.json")
	assert.Empty(t, userOverlayPath([]string{"./..."}, dir))
	assert.Equal(t, filepath.Join(dir, "u.json"), userOverlayPath([]string{"-overlay", "u.json", "."}, dir))
	assert.Equal(t, abs, userOverlayPath([]string{"--overlay=" + abs, "."}, dir))

	flags := filepath.Join(t.TempDir(), "flags.json")
	t.Setenv("GOFLAGS", "-overlay="+flags)
	assert.Equal(t, flags, userOverlayPath([]string{"."}, dir))
	assert.Equal(t, filepath.Join(dir, "cli.json"), userOverlayPath([]string{"-overlay=cli.json", "."}, dir),
		"a command-line flag wins over GOFLAGS")
}

func TestPrepareImportOverlay(t *testing.T) {
	sp := &setupPhase{logger: slog.New(slog.DiscardHandler)}
	buildDir := t.TempDir()
	appDir := t.TempDir()
	goMod := filepath.Join(appDir, "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")
	libSrc := filepath.Join(appDir, "lib", "lib.go")
	mustWriteFile(t, libSrc, "package lib\n")
	mainSrc := filepath.Join(appDir, "cmd", "main.go")
	mustWriteFile(t, mainSrc, "package main\n")

	lib := rawSet("example.com/app/lib", []string{libSrc}, map[string]string{"list": "container/list"})
	fileTarget := rawSet("command-line-arguments", []string{mainSrc}, map[string]string{"ring": "container/ring"})

	o, runtimeImports, err := sp.prepareImportOverlay(
		[]*rule.InstRuleSet{lib, fileTarget}, goMod, "/g", "/m", buildDir)
	require.NoError(t, err)

	assert.Equal(t, map[string][]string{filepath.Dir(mainSrc): {"container/ring"}}, runtimeImports)
	assert.Contains(t, o.Replace, filepath.Join(appDir, "lib", "otelc.imports.go"))
	assert.Len(t, o.Replace, 1)
}

func TestWithImportOverlay(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	args := []string{"build", "-overlay", "user.json", "./..."}

	gotArgs, flag := withImportOverlay(args)
	assert.Equal(t, args, gotArgs, "no overlay from setup: the user's flags reach the build unchanged")
	assert.Empty(t, flag)

	overlay := filepath.Join(util.GetBuildTempDir(), overlayFileName)
	mustWriteFile(t, overlay, `{"Replace":{}}`)
	gotArgs, flag = withImportOverlay(args)
	assert.Equal(t, []string{"build", "./..."}, gotArgs)
	assert.Equal(t, "-overlay="+overlay, flag)
}

func TestBuildImportOverlayGoModReplaceOrderIsStable(t *testing.T) {
	buildDir := t.TempDir()
	goMod := filepath.Join(t.TempDir(), "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")
	names := []string{"a", "b", "c", "d"}
	targets := make([]*importTarget, 0, len(names))
	for _, name := range names {
		dir := filepath.Join(t.TempDir(), "example.com", name+"@v1.0.0")
		mustWriteFile(t, filepath.Join(dir, "x.go"), "package "+name+"\n")
		mod := moduleRoot{Path: "example.com/" + name, Version: "v1.0.0", Dir: dir}
		targets = append(targets, &importTarget{
			ImportPath: mod.Path, Name: name, Dir: dir, Kind: kindModCache, Module: mod, Imports: []string{"fmt"},
		})
	}

	first, err := buildImportOverlay(targets, goMod, buildDir)
	require.NoError(t, err)
	want := readOverlayFile(t, first.Replace[goMod])
	for range 20 {
		o, buildErr := buildImportOverlay(targets, goMod, buildDir)
		require.NoError(t, buildErr)
		require.Equal(
			t,
			want,
			readOverlayFile(t, o.Replace[goMod]),
			"the go.mod overlay must be identical on every build",
		)
	}
}

func TestBuildImportOverlayModCacheWorkspace(t *testing.T) {
	buildDir := t.TempDir()
	mod := fakeModCacheModule(t, true)
	goWork := filepath.Join(t.TempDir(), "go.work")
	mustWriteFile(t, goWork, "go 1.25.0\n\nuse ./app\n")

	o, err := buildImportOverlay([]*importTarget{
		{
			ImportPath: "example.com/dep",
			Name:       "dep",
			Dir:        mod.Dir,
			Kind:       kindModCache,
			Module:     mod,
			Imports:    []string{"fmt"},
		},
	}, goWork, buildDir)
	require.NoError(t, err)

	replaceDir := filepath.Join(buildDir, "modreplace", "example.com", "dep@v1.2.3")
	got := readOverlayFile(t, o.Replace[goWork])
	assert.Contains(t, got, "use ./app")
	assert.Contains(t, got, "replace example.com/dep => "+replaceDir)
}

func TestUserModFilePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOFLAGS", "")

	abs := filepath.Join(t.TempDir(), "alt.mod")
	assert.Empty(t, userModFilePath([]string{"."}, dir))
	assert.Equal(t, filepath.Join(dir, "alt.mod"), userModFilePath([]string{"-modfile=alt.mod", "."}, dir))
	assert.Equal(t, abs, userModFilePath([]string{"-modfile", abs, "."}, dir))

	flags := filepath.Join(t.TempDir(), "flags.mod")
	t.Setenv("GOFLAGS", "-modfile="+flags)
	assert.Equal(t, flags, userModFilePath([]string{"."}, dir))
}

func TestBuildImportOverlayRecordsModFile(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	buildDir := util.GetBuildTempDir()
	changed, err := util.ModFileChangedSinceSetup()
	require.NoError(t, err)
	assert.False(t, changed, "no record: nothing to compare")

	mod := fakeModCacheModule(t, true)
	goMod := filepath.Join(t.TempDir(), "go.mod")
	mustWriteFile(t, goMod, "module example.com/app\n")
	_, err = buildImportOverlay([]*importTarget{
		{
			ImportPath: "example.com/dep",
			Name:       "dep",
			Dir:        mod.Dir,
			Kind:       kindModCache,
			Module:     mod,
			Imports:    []string{"fmt"},
		},
	}, goMod, buildDir)
	require.NoError(t, err)

	changed, err = util.ModFileChangedSinceSetup()
	require.NoError(t, err)
	assert.False(t, changed)

	mustWriteFile(t, goMod, "module example.com/app\n\nrequire example.com/x v1.0.0\n")
	changed, err = util.ModFileChangedSinceSetup()
	require.NoError(t, err)
	assert.True(t, changed)
}
