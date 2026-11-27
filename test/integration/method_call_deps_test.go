// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// methodCallDepsFiles is a module with a package p that declares methods, and
// one package for each way Go code can call them. The rules add 1000 to the
// result of every call they wrap.
var methodCallDepsFiles = map[string]string{ //nolint:gochecknoglobals // test fixture
	"go.mod": "module example.com/otelc-mcdeps\n\ngo 1.25.0\n",
	"rules.yml": `wrap_t_do:
  target: "**"
  where:
    method_call: example.com/otelc-mcdeps/p.*T.Do
  do:
    - wrap_call:
        replace: "({{ . }} + 1000)"
wrap_g_do:
  target: "**"
  where:
    method_call: example.com/otelc-mcdeps/p.*G.Do
  do:
    - wrap_call:
        replace: "({{ . }} + 1000)"
`,
	"p/p.go": `package p

type T struct{ n int }

func New(n int) *T { return &T{n: n} }

func (t *T) Do() int { return t.n }

type G[V any] struct{ n int }

func NewG[V any](n int) *G[V] { return &G[V]{n: n} }

func (g *G[V]) Do() int { return g.n }

// Self calls Do in the package that declares it.
func Self() int { return New(1).Do() }
`,
	"factory/factory.go": `package factory

import "example.com/otelc-mcdeps/p"

func Make() *p.T { return p.New(2) }

func MakeG() *p.G[string] { return p.NewG[string](3) }
`,
	// viaret calls methods of types that another package returns. It does
	// not import p.
	"viaret/viaret.go": `package viaret

import "example.com/otelc-mcdeps/factory"

func Run() int { return factory.Make().Do() }

func RunG() int { return factory.MakeG().Do() }
`,
	"embed/embed.go": `package embed

import "example.com/otelc-mcdeps/p"

type W struct{ *p.T }

func Make() W { return W{p.New(4)} }
`,
	// promoted calls the method that embed.W promotes from *p.T.
	"promoted/promoted.go": `package promoted

import "example.com/otelc-mcdeps/embed"

func Run() int { return embed.Make().Do() }
`,
	"alias/alias.go": `package alias

import "example.com/otelc-mcdeps/p"

type A = p.T

func Make() *A { return p.New(5) }
`,
	// viaalias calls the method through a type alias of p.T.
	"viaalias/viaalias.go": `package viaalias

import "example.com/otelc-mcdeps/alias"

func Run() int {
	var a *alias.A = alias.Make()
	return a.Do()
}
`,
	// methodvalue calls the method through a method value, which a
	// method_call rule does not match.
	"methodvalue/methodvalue.go": `package methodvalue

import "example.com/otelc-mcdeps/factory"

func Run() int {
	do := factory.Make().Do
	return do()
}
`,
	// iface, generic and unrelated do not depend on p. A method_call rule
	// does not match a call through an interface or a type parameter, or a
	// method of another type.
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
	// testonly imports p only in its tests.
	"testonly/testonly.go": "package testonly\n\nfunc Value() int { return 9 }\n",
	"testonly/testonly_test.go": `package testonly

import (
	"testing"

	"example.com/otelc-mcdeps/p"
)

func TestDo(t *testing.T) {
	if got := p.New(10).Do(); got != 1010 {
		t.Fatalf("p.New(10).Do() = %d, want 1010", got)
	}
}
`,
	"testonly/external_test.go": `package testonly_test

import (
	"testing"

	"example.com/otelc-mcdeps/p"
)

func TestExternalDo(t *testing.T) {
	if got := p.New(11).Do(); got != 1011 {
		t.Fatalf("p.New(11).Do() = %d, want 1011", got)
	}
}
`,
	"main.go": `package main

import (
	"fmt"

	"example.com/otelc-mcdeps/generic"
	"example.com/otelc-mcdeps/iface"
	"example.com/otelc-mcdeps/methodvalue"
	"example.com/otelc-mcdeps/p"
	"example.com/otelc-mcdeps/promoted"
	"example.com/otelc-mcdeps/testonly"
	"example.com/otelc-mcdeps/unrelated"
	"example.com/otelc-mcdeps/viaalias"
	"example.com/otelc-mcdeps/viaret"
)

func main() {
	fmt.Println("main:", p.New(12).Do())
	fmt.Println("self:", p.Self())
	fmt.Println("viaret:", viaret.Run(), viaret.RunG())
	fmt.Println("promoted:", promoted.Run())
	fmt.Println("viaalias:", viaalias.Run())
	fmt.Println("methodvalue:", methodvalue.Run())
	fmt.Println("iface:", iface.Run(p.New(6)))
	fmt.Println("generic:", generic.Call(p.New(8)))
	fmt.Println("unrelated:", unrelated.Run())
	fmt.Println("testonly:", testonly.Value())
}
`,
}

// TestMethodCallRulesFollowPackageDependencies builds and tests an application
// whose method_call rules target "**". Setup adds the rules only to the
// packages that depend on the package of the receiver type. Every call the
// rules match must still be wrapped, in the build and in the tests.
func TestMethodCallRulesFollowPackageDependencies(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	moduleDir := t.TempDir()
	for name, content := range methodCallDepsFiles {
		writeTestFile(t, moduleDir, filepath.FromSlash(name), content)
	}
	env := append(os.Environ(), "OTELC_RULES=rules.yml")

	bin := filepath.Join(t.TempDir(), "app")
	if util.IsWindows() {
		bin += ".exe"
	}
	runOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "-o", bin, ".")
	out, err := exec.CommandContext(t.Context(), bin).CombinedOutput()
	require.NoError(t, err, "running %s:\n%s", bin, out)
	assert.Equal(t, `main: 1012
self: 1001
viaret: 1002 1003
promoted: 1004
viaalias: 1005
methodvalue: 2
iface: 6
generic: 8
unrelated: 7
testonly: 9
`, strings.ReplaceAll(string(out), "\r\n", "\n"))

	raw, err := os.ReadFile(filepath.Join(moduleDir, util.BuildTempDir, "matched.json"))
	require.NoError(t, err)
	var sets []struct {
		ModulePath string          `json:"module_path"`
		CallRules  json.RawMessage `json:"call_rules"`
	}
	require.NoError(t, json.Unmarshal(raw, &sets))
	var withCallRules []string
	for _, set := range sets {
		if len(set.CallRules) > 0 && string(set.CallRules) != "{}" && string(set.CallRules) != "null" {
			withCallRules = append(withCallRules, set.ModulePath)
		}
	}
	const module = "example.com/otelc-mcdeps/"
	assert.ElementsMatch(t, []string{
		"main", module + "p", module + "factory", module + "viaret", module + "embed", module + "promoted",
		module + "alias", module + "viaalias", module + "methodvalue",
	}, withCallRules, "only the packages that depend on p get the rules")

	output := runOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", "./testonly/")
	assert.Contains(t, output, "--- PASS: TestDo")
	assert.Contains(t, output, "--- PASS: TestExternalDo")
}
