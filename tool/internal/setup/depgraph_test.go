// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"go.opentelemetry.io/otelc/tool/internal/pkgload"
)

func TestImportGraphReaches(t *testing.T) {
	dir := t.TempDir()
	file := func(name string) string { return filepath.Join(dir, name) }
	pkg := func(path string, files []string, imports ...*packages.Package) *packages.Package {
		p := &packages.Package{PkgPath: path, GoFiles: files, Imports: map[string]*packages.Package{}}
		for _, imp := range imports {
			p.Imports[imp.PkgPath] = imp
		}
		return p
	}
	target := pkg("example.com/p", []string{file("p.go")})
	factory := pkg("example.com/factory", []string{file("factory.go")}, target)
	viaFactory := pkg("example.com/viafactory", []string{file("viafactory.go")}, factory)
	unrelated := pkg("example.com/unrelated", []string{file("unrelated.go")})
	main := pkg("example.com/cmd", []string{file("main.go")}, viaFactory, unrelated)

	g := &importGraph{}
	g.add([]*packages.Package{main})

	tests := []struct {
		name string
		dep  *Dependency
		want bool
	}{
		{"the package itself", &Dependency{ImportPath: "example.com/p", Sources: []string{file("p.go")}}, true},
		{"direct import", &Dependency{ImportPath: "example.com/factory", Sources: []string{file("factory.go")}}, true},
		{
			"import of an import",
			&Dependency{ImportPath: "example.com/viafactory", Sources: []string{file("viafactory.go")}},
			true,
		},
		{
			"no path to the package",
			&Dependency{ImportPath: "example.com/unrelated", Sources: []string{file("unrelated.go")}},
			false,
		},
		{"main package found by its files", &Dependency{ImportPath: "main", Sources: []string{file("main.go")}}, true},
		{"package found by its import path", &Dependency{ImportPath: "example.com/factory"}, true},
		{
			"package the graph does not know",
			&Dependency{ImportPath: "example.com/other", Sources: []string{file("other.go")}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, g.reaches(tt.dep, "example.com/p"))
		})
	}

	t.Run("a package nothing imports", func(t *testing.T) {
		assert.False(t, g.reaches(tests[0].dep, "example.com/missing"))
	})
}

func TestImportGraphReachesThroughTestVariant(t *testing.T) {
	dir := t.TempDir()
	libFile := filepath.Join(dir, "lib.go")
	libTestFile := filepath.Join(dir, "lib_test.go")
	target := &packages.Package{PkgPath: "example.com/p"}
	lib := &packages.Package{PkgPath: "example.com/lib", GoFiles: []string{libFile}}
	// The test variant of lib imports p in lib_test.go.
	libTest := &packages.Package{
		ID:      "example.com/lib [example.com/lib.test]",
		PkgPath: "example.com/lib",
		ForTest: "example.com/lib",
		GoFiles: []string{libFile, libTestFile},
		Imports: map[string]*packages.Package{"example.com/p": target},
	}

	g := &importGraph{}
	g.add([]*packages.Package{lib, libTest})

	testDep := &Dependency{ImportPath: "example.com/lib", Sources: []string{libFile, libTestFile}, IsTest: true}
	assert.True(t, g.reaches(testDep, "example.com/p"))
	// The graph is keyed by import path, so the normal compile gets the
	// imports of its test variant too.
	normalDep := &Dependency{ImportPath: "example.com/lib", Sources: []string{libFile}}
	assert.True(t, g.reaches(normalDep, "example.com/p"))
}

func TestImportGraphReachesThroughPackageWithErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "app.go")
	broken := &packages.Package{PkgPath: "example.com/broken", Errors: []packages.Error{{Msg: "no Go files"}}}
	app := &packages.Package{
		PkgPath: "example.com/app",
		GoFiles: []string{file},
		Imports: map[string]*packages.Package{"example.com/broken": broken},
	}

	g := &importGraph{}
	g.add([]*packages.Package{app})

	assert.True(t, g.reaches(&Dependency{ImportPath: "example.com/app", Sources: []string{file}}, "example.com/p"),
		"the imports of a package loaded with errors are not known")
}

func TestImportGraphAddResetsAnswers(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hook.go")
	dep := &Dependency{ImportPath: "example.com/hook", Sources: []string{file}}
	target := &packages.Package{PkgPath: "example.com/p"}

	g := &importGraph{}
	g.add([]*packages.Package{target, {PkgPath: "example.com/other"}})
	assert.True(t, g.reaches(dep, "example.com/p"), "the graph does not know hook yet")

	// A later load, such as the one of the packages otelc injects, adds hook.
	g.add([]*packages.Package{{PkgPath: "example.com/hook", GoFiles: []string{file}}})
	assert.False(t, g.reaches(dep, "example.com/p"))

	g.add([]*packages.Package{{
		PkgPath: "example.com/hook",
		GoFiles: []string{file},
		Imports: map[string]*packages.Package{"example.com/p": target},
	}})
	assert.True(t, g.reaches(dep, "example.com/p"))
}

func TestImportGraphNil(t *testing.T) {
	var g *importGraph
	assert.True(t, g.reaches(&Dependency{ImportPath: "example.com/app"}, "example.com/p"))
}

// methodCallDepsModule writes a module with a package p that declares methods,
// and one package for each way Go code can call a method of p. It returns the
// module directory.
func methodCallDepsModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/mcdeps\n\ngo 1.25.0\n",
		"p/p.go": `package p

type T struct{ n int }

func New(n int) *T { return &T{n: n} }

func (t *T) Do() int { return t.n }

type G[V any] struct{ n int }

func NewG[V any](n int) *G[V] { return &G[V]{n: n} }

func (g *G[V]) Do() int { return g.n }
`,
		"factory/factory.go": `package factory

import "example.com/mcdeps/p"

func Make() *p.T { return p.New(2) }

func MakeG() *p.G[string] { return p.NewG[string](3) }
`,
		"viaret/viaret.go": `package viaret

import "example.com/mcdeps/factory"

func Run() int { return factory.Make().Do() + factory.MakeG().Do() }
`,
		"embed/embed.go": `package embed

import "example.com/mcdeps/p"

type W struct{ *p.T }

func Make() W { return W{p.New(4)} }
`,
		"promoted/promoted.go": `package promoted

import "example.com/mcdeps/embed"

func Run() int { return embed.Make().Do() }
`,
		"alias/alias.go": `package alias

import "example.com/mcdeps/p"

type A = p.T

func Make() *A { return p.New(5) }
`,
		"viaalias/viaalias.go": `package viaalias

import "example.com/mcdeps/alias"

func Run() int {
	var a *alias.A = alias.Make()
	return a.Do()
}
`,
		"iface/iface.go": `package iface

type Doer interface{ Do() int }

func Run(d Doer) int { return d.Do() }
`,
		"generic/generic.go": `package generic

func Call[X interface{ Do() int }](x X) int { return x.Do() }
`,
		"unrelated/unrelated.go": `package unrelated

type T struct{}

func (*T) Do() int { return 7 }

func Run() int { return (&T{}).Do() }
`,
		"testonly/testonly.go": "package testonly\n\nfunc Value() int { return 9 }\n",
		"testonly/testonly_test.go": `package testonly

import (
	"testing"

	"example.com/mcdeps/p"
)

func TestDo(t *testing.T) { _ = p.New(10).Do() }
`,
		"main.go": `package main

import (
	"fmt"

	"example.com/mcdeps/generic"
	"example.com/mcdeps/iface"
	"example.com/mcdeps/p"
	"example.com/mcdeps/promoted"
	"example.com/mcdeps/testonly"
	"example.com/mcdeps/unrelated"
	"example.com/mcdeps/viaalias"
	"example.com/mcdeps/viaret"
)

func main() {
	fmt.Println(viaret.Run(), promoted.Run(), viaalias.Run(), iface.Run(p.New(6)),
		generic.Call(p.New(8)), unrelated.Run(), testonly.Value())
}
`,
	}
	for name, content := range files {
		mustWriteFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	return dir
}

// TestImportGraphReachesLoadedPackages loads a real module, test variants
// included, and checks each way a package can hold a value of a type of p.
func TestImportGraphReachesLoadedPackages(t *testing.T) {
	dir := methodCallDepsModule(t)
	pkgs, err := pkgload.LoadPackages(t.Context(), packages.NeedName|packages.NeedFiles|packages.NeedImports|
		packages.NeedDeps|packages.NeedForTest, []string{"-C", dir}, true, "./...")
	require.NoError(t, err)

	g := &importGraph{}
	g.add(pkgs)

	src := func(name string) string { return filepath.Join(dir, filepath.FromSlash(name)) }
	dep := func(importPath string, isTest bool, sources ...string) *Dependency {
		for i, s := range sources {
			sources[i] = src(s)
		}
		return &Dependency{ImportPath: importPath, Sources: sources, IsTest: isTest}
	}
	const p = "example.com/mcdeps/p"
	tests := []struct {
		name string
		dep  *Dependency
		want bool
	}{
		{"p itself", dep(p, false, "p/p.go"), true},
		{
			"a method of a type that another package returns",
			dep("example.com/mcdeps/viaret", false, "viaret/viaret.go"),
			true,
		},
		{
			"a promoted method of an embedded type",
			dep("example.com/mcdeps/promoted", false, "promoted/promoted.go"),
			true,
		},
		{"a method through a type alias", dep("example.com/mcdeps/viaalias", false, "viaalias/viaalias.go"), true},
		{"main, which compiles with -p main", dep("main", false, "main.go"), true},
		{"the test variant whose test file imports p", dep("example.com/mcdeps/testonly", true,
			"testonly/testonly.go", "testonly/testonly_test.go"), true},
		{"a call through an interface", dep("example.com/mcdeps/iface", false, "iface/iface.go"), false},
		{"a call on a type parameter", dep("example.com/mcdeps/generic", false, "generic/generic.go"), false},
		{"a method of another type with the same name", dep("example.com/mcdeps/unrelated", false,
			"unrelated/unrelated.go"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotEmpty(t, g.packagesOf(tt.dep), "the graph must know the package")
			assert.Equal(t, tt.want, g.reaches(tt.dep, p))
		})
	}
}
