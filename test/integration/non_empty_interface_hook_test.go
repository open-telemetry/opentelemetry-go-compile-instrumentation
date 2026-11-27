//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// TestNonEmptyInterfaceHookParam verifies that hooks with anonymous non-empty
// interface parameters keep that interface type in the generated //go:linkname
// stub rather than being rewritten to empty interface{}, which would violate
// the runtime iface/eface calling convention and fault on method dispatch.
func TestNonEmptyInterfaceHookParam(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module example.com/otelc-nonempty-iface

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "fmt"

type tagger struct{ label string }

func (t tagger) Tag() string { return t.label }

func Close(c interface{ Tag() string }) string {
	return c.Tag()
}

func Open() interface{ Tag() string } {
	return tagger{label: "opened"}
}

func main() {
	fmt.Println("close", Close(tagger{label: "closed"}))
	fmt.Println("open", Open().Tag())
}
`)
	writeTestFile(t, moduleDir, filepath.Join("hooks", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func BeforeClose(ctx hook.HookContext, c interface{ Tag() string }) {
	_ = ctx
	fmt.Println("before", c.Tag())
}

func AfterOpen(ctx hook.HookContext, c interface{ Tag() string }) {
	_ = ctx
	fmt.Println("after", c.Tag())
}
`)
	writeTestFile(t, moduleDir, "rules.yml", `hook_close:
  target: main
  where:
    func: Close
  do:
    - inject_hooks:
        before: BeforeClose
        path: example.com/otelc-nonempty-iface/hooks
hook_open:
  target: main
  where:
    func: Open
  do:
    - inject_hooks:
        after: AfterOpen
        path: example.com/otelc-nonempty-iface/hooks
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")
	bin := filepath.Join(t.TempDir(), "app")
	if util.IsWindows() {
		bin += ".exe"
	}
	runOtelcCommand(t, moduleDir, env, otelcPath, "go", "build", "-o", bin, ".")

	out, err := exec.CommandContext(t.Context(), bin).CombinedOutput()
	require.NoError(t, err, "running %s:\n%s", bin, out)
	require.Contains(t, string(out), "before closed")
	require.Contains(t, string(out), "close closed")
	require.Contains(t, string(out), "after opened")
	require.Contains(t, string(out), "open opened")
}

func writeEmbeddedInterfaceModule(t *testing.T, moduleDir, rulesYML string) {
	t.Helper()
	otelcPkgDir, err := filepath.Abs(filepath.Join("..", "..", "pkg"))
	require.NoError(t, err)

	goMod := "module example.com/otelc-embedded-iface\n\ngo 1.25.0\n\n" +
		"require go.opentelemetry.io/otelc/pkg v0.0.0\n\n" +
		"replace go.opentelemetry.io/otelc/pkg => " + filepath.ToSlash(otelcPkgDir) + "\n"
	writeTestFile(t, moduleDir, "go.mod", goMod)
	writeTestFile(t, moduleDir, "shared/value.go", `package shared

type Value struct{ Name string }

type Marker interface{ MarkVal(Value) string }
`)
	writeTestFile(t, moduleDir, "a/marker.go", "package a\n\ntype Marker interface{ Mark() string }\n")
	writeTestFile(t, moduleDir, "b/marker.go", "package b\n\ntype Marker interface{ Mark() string }\n")
	writeTestFile(t, moduleDir, "emptya/marker.go", "package emptya\n\ntype Marker interface{}\n")
	writeTestFile(t, moduleDir, "emptyb/marker.go", "package emptyb\n\ntype Marker interface{}\n")
	writeTestFile(t, moduleDir, "hooks/hooks.go", `package hooks

import (
	"fmt"

	"example.com/otelc-embedded-iface/b"
	"example.com/otelc-embedded-iface/emptyb"
	"example.com/otelc-embedded-iface/shared"
	"go.opentelemetry.io/otelc/pkg/hook"
)

func BeforeMarker(ctx hook.HookContext, x interface{ b.Marker }) {
	fmt.Println("before marker", x.Mark())
}

func AfterMarker(ctx hook.HookContext, x interface{ b.Marker }) {
	fmt.Println("after marker", x.Mark())
}

func BeforeEmptyMarker(ctx hook.HookContext, x interface{ emptyb.Marker }) {
	fmt.Printf("before empty %T\n", x)
}

func BeforeSharedMarker(ctx hook.HookContext, x interface{ shared.Marker }) {
	fmt.Println("before shared", x.MarkVal(shared.Value{Name: "hook"}))
}
`)
	writeTestFile(t, moduleDir, "main.go", `package main

import (
	"fmt"

	"example.com/otelc-embedded-iface/a"
	"example.com/otelc-embedded-iface/emptya"
	"example.com/otelc-embedded-iface/shared"
)

type markerImpl struct{}

func (markerImpl) Mark() string { return "marked" }
func (markerImpl) MarkVal(v shared.Value) string { return "val:" + v.Name }

func UseMarker(x interface{ a.Marker }) {
	fmt.Println("use marker", x.Mark())
}

func GetMarker() interface{ a.Marker } {
	return markerImpl{}
}

func UseEmptyMarker(x interface{ emptya.Marker }) {
	fmt.Printf("use empty %T\n", x)
}

func UseSharedMarker(x interface{ shared.Marker }) {
	fmt.Println("use shared", x.MarkVal(shared.Value{Name: "target"}))
}

func main() {
	UseMarker(markerImpl{})
	_ = GetMarker()
	UseEmptyMarker(markerImpl{})
	UseSharedMarker(markerImpl{})
}
`)
	writeTestFile(t, moduleDir, "rules.yml", rulesYML)
}

func TestEmbeddedInterfaceHookParam(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	validModuleDir := t.TempDir()
	writeEmbeddedInterfaceModule(t, validModuleDir, `before_marker:
  target: main
  where:
    func: UseMarker
  do:
    - inject_hooks:
        before: BeforeMarker
        path: example.com/otelc-embedded-iface/hooks
after_marker:
  target: main
  where:
    func: GetMarker
  do:
    - inject_hooks:
        after: AfterMarker
        path: example.com/otelc-embedded-iface/hooks
before_empty:
  target: main
  where:
    func: UseEmptyMarker
  do:
    - inject_hooks:
        before: BeforeEmptyMarker
        path: example.com/otelc-embedded-iface/hooks
before_shared:
  target: main
  where:
    func: UseSharedMarker
  do:
    - inject_hooks:
        before: BeforeSharedMarker
        path: example.com/otelc-embedded-iface/hooks
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")
	bin := filepath.Join(t.TempDir(), "app")
	if util.IsWindows() {
		bin += ".exe"
	}
	runOtelcCommand(t, validModuleDir, env, otelcPath, "go", "build", "-o", bin, ".")

	out, err := exec.CommandContext(t.Context(), bin).CombinedOutput()
	require.NoError(t, err, "running %s:\n%s", bin, out)
	require.Contains(t, string(out), "before marker marked")
	require.Contains(t, string(out), "use marker marked")
	require.Contains(t, string(out), "after marker marked")
	require.Contains(t, string(out), "before empty main.markerImpl")
	require.Contains(t, string(out), "use empty main.markerImpl")
	require.Contains(t, string(out), "before shared val:hook")
	require.Contains(t, string(out), "use shared val:target")

	// interface{ emptyb.Marker } shares the stripped embedded name "Marker"
	// with interface{ a.Marker } but has an empty method set; otelc must reject
	// the mismatch instead of emitting a stub with interface{ a.Marker }.
	mismatchModuleDir := t.TempDir()
	writeEmbeddedInterfaceModule(t, mismatchModuleDir, `mismatched_empty_hook:
  target: main
  where:
    func: UseMarker
  do:
    - inject_hooks:
        before: BeforeEmptyMarker
        path: example.com/otelc-embedded-iface/hooks
`)

	mismatchBin := filepath.Join(t.TempDir(), "mismatch-app")
	if util.IsWindows() {
		mismatchBin += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), otelcPath, "go", "build", "-o", mismatchBin, ".")
	cmd.Dir = mismatchModuleDir
	cmd.Env = env
	mismatchOut, mismatchErr := cmd.CombinedOutput()
	require.Error(t, mismatchErr, "expected mismatched embedded interface method set to fail")
	require.Contains(t, string(mismatchOut), "type mismatch")
}
