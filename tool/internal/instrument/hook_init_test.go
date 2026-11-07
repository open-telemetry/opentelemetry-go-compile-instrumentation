// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dave/dst"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestGuardHookFunctions(t *testing.T) {
	root, err := ast.NewAstParser().ParseSource(`package hooks
func Before() { println("before") }
func After() { println("after") }
func Other() { println("other") }
type T struct{}
func (T) Before() { println("method") }
`)
	require.NoError(t, err)
	changed, err := guardHookFunctions(root, map[string]bool{"Before": true, "After": true})
	require.NoError(t, err)
	require.True(t, changed)
	for _, decl := range root.Decls {
		fn, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}
		if fn.Recv == nil && fn.Name.Name != "Other" {
			require.Len(t, fn.Body.List, 2)
			guard, isIf := fn.Body.List[0].(*dst.IfStmt)
			require.True(t, isIf)
			require.Len(t, guard.Body.List, 1)
			_, isReturn := guard.Body.List[0].(*dst.ReturnStmt)
			require.True(t, isReturn)
		} else {
			require.Len(t, fn.Body.List, 1)
		}
	}
	changed, err = guardHookFunctions(root, map[string]bool{"Missing": true})
	require.NoError(t, err)
	require.False(t, changed)
}

func TestHookNamesForPackage(t *testing.T) {
	set := rule.NewInstRuleSet("target")
	set.FuncRules["target.go"] = []*rule.InstFuncRule{
		{Path: "hooks", Before: "Before", After: "After"},
		{Path: "hooks", Before: "Before"},
		{Path: "other", Before: "Other"},
	}
	require.Equal(
		t,
		map[string]bool{"Before": true, "After": true},
		hookNamesForPackage([]*rule.InstRuleSet{set}, "hooks"),
	)
	require.Empty(t, hookNamesForPackage([]*rule.InstRuleSet{set}, "unrelated"))
}

func TestGuardHookInitializers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "hooks.go")
	require.NoError(t, os.WriteFile(file, []byte("package hooks; func Before() {}"), 0o600))
	work := t.TempDir()
	ip := &instrumentPhase{
		logger:      slog.New(slog.DiscardHandler),
		workDir:     work,
		compileArgs: []string{"compile", "-p", "hooks", file},
	}
	set := rule.NewInstRuleSet("target")
	set.FuncRules["target.go"] = []*rule.InstFuncRule{{Path: "hooks", Before: "Before"}}
	require.NoError(t, ip.guardHookInitializers(t.Context(), []*rule.InstRuleSet{set}))
	require.Equal(t, filepath.Join(work, "otelc.hook-ready.go"), ip.compileArgs[len(ip.compileArgs)-1])
	rewritten, err := os.ReadFile(ip.compileArgs[len(ip.compileArgs)-2])
	require.NoError(t, err)
	require.Contains(t, string(rewritten), "if !otelcHookReady()")
	ready, err := os.ReadFile(ip.compileArgs[len(ip.compileArgs)-1])
	require.NoError(t, err)
	require.Contains(t, string(ready), "package hooks")
	require.Contains(t, string(ready), "StoreUint32(&otelcHooksInitialized, 1)")
	untouched := &instrumentPhase{compileArgs: []string{"compile", "-p", "unrelated", file}}
	require.NoError(t, untouched.guardHookInitializers(t.Context(), []*rule.InstRuleSet{set}))
	require.Equal(t, []string{"compile", "-p", "unrelated", file}, untouched.compileArgs)
}

func TestGuardHookInitializersKeepsRuleDiff(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	t.Setenv(util.EnvOtelcDebug, "true")
	work := t.TempDir()
	// A rule already instrumented this hook file into the work directory.
	file := filepath.Join(work, "hooks.go")
	require.NoError(t, os.WriteFile(file, []byte("package hooks; func Before() {}"), 0o600))
	ip := &instrumentPhase{
		logger:      slog.New(slog.DiscardHandler),
		workDir:     work,
		compileArgs: []string{"compile", "-p", "hooks", file},
	}
	diff := filepath.Join(ip.debugArtifactDir(), "hooks.go.diff")
	require.NoError(t, os.MkdirAll(filepath.Dir(diff), 0o755))
	require.NoError(t, os.WriteFile(diff, []byte("diff of the rule\n"), 0o600))

	set := rule.NewInstRuleSet("target")
	set.FuncRules["target.go"] = []*rule.InstFuncRule{{Path: "hooks", Before: "Before"}}
	require.NoError(t, ip.guardHookInitializers(t.Context(), []*rule.InstRuleSet{set}))

	require.Equal(t, file, ip.compileArgs[3])
	rewritten, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Contains(t, string(rewritten), "if !otelcHookReady()")
	kept, err := os.ReadFile(diff)
	require.NoError(t, err)
	require.Equal(t, "diff of the rule\n", string(kept))
}

func TestHookReadinessPublication(t *testing.T) {
	dir := t.TempDir()
	source := fmt.Sprintf(hookReadySource, "main", "example.com/hooks")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ready.go"), []byte(source), 0o600))
	// Concurrent early callers must all be skipped, with a single warning.
	app := `package main
import "sync"
func init() {
 var wg sync.WaitGroup
 for range 8 { wg.Go(func() {
  for range 100 { if otelcHookReady() { panic("ready too early") } }
 }) }
 wg.Wait()
}
func main() { if !otelcHookReady() { panic("not ready") }; println("ready") }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(app), 0o600))
	cmd := exec.CommandContext(t.Context(), "go", "run", "main.go", "ready.go")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "ready")
	require.Equal(t, 1, strings.Count(string(out), "before package initialization completes"))
}
