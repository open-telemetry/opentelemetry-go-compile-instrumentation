// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportsFileSource(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", importsFileSource("foo", []string{"b", "a"}), parser.ImportsOnly)
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
		{ImportPath: "command-line-arguments", Name: "main", Dir: t.TempDir(), Kind: kindRuntimeFile, Imports: []string{"fmt"}},
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

	o, err := buildImportOverlay([]*importTarget{{
		ImportPath: "example.com/dep", Name: "dep", Dir: mod.Dir, Kind: kindModCache, Module: mod, Imports: []string{"fmt"},
	}}, goMod, buildDir)
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

	o, err := buildImportOverlay([]*importTarget{{
		ImportPath: "example.com/orig", Name: "dep", Dir: mod.Dir, Kind: kindModCache, Module: mod, Imports: []string{"fmt"},
	}}, goMod, buildDir)
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
		{ImportPath: "example.com/dep", Name: "dep", Dir: mod.Dir, Kind: kindModCache, Module: mod, Imports: []string{"fmt"}},
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
