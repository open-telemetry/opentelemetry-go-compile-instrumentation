// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

// guardHookInitializers runs after normal instrumentation so generated hooks
// and any files added by rules are covered too.
func (ip *instrumentPhase) guardHookInitializers(ctx context.Context, sets []*rule.InstRuleSet) error {
	pkg := util.FindFlagValue(ip.compileArgs, "-p")
	names := hookNamesForPackage(sets, pkg)
	if len(names) == 0 {
		return nil
	}
	packageName, err := ip.resolvePackageName()
	if err != nil {
		return err
	}
	for _, file := range slices.Clone(ip.compileArgs) {
		if !util.IsGoFile(file) {
			continue
		}
		root, parseErr := ip.parseFile(file)
		if parseErr != nil {
			return parseErr
		}
		changed, guardErr := guardHookFunctions(root, names)
		if guardErr != nil {
			return guardErr
		}
		if !changed {
			continue
		}
		abs, absErr := filepath.Abs(file)
		if absErr != nil {
			return ex.Wrap(absErr)
		}
		if err = ip.writeGuardedHookFile(root, abs); err != nil {
			return err
		}
	}
	// The compiler runs init functions in source argument order. Append this
	// file after all original and rule-added sources, rather than relying on its name.
	source := fmt.Sprintf(hookReadySource, packageName, pkg)
	path := filepath.Join(ip.workDir, "otelc.hook-ready.go")
	if err = os.WriteFile(path, []byte(source), 0o600); err != nil {
		return ex.Wrapf(err, "writing hook readiness file")
	}
	ip.addCompileArg(path)
	ip.keepForDebug(path)
	return ip.updateImportConfig(ctx, map[string]string{"atomic": "sync/atomic"})
}

// writeGuardedHookFile writes the guarded AST of file. A file that a rule
// already instrumented is in the work directory and is rewritten in place:
// writeInstrumented would compare it with itself and remove the diff of
// those rules. The guard then shows only in the kept copy of the file.
func (ip *instrumentPhase) writeGuardedHookFile(root *dst.File, file string) error {
	workDir, err := filepath.Abs(ip.workDir)
	if err != nil {
		return ex.Wrap(err)
	}
	if filepath.Dir(file) != workDir {
		return ip.writeInstrumented(root, file, nil)
	}
	if err = ast.WriteFile(file, root); err != nil {
		return ex.Wrapf(err, "writing guarded hook file %s", file)
	}
	ip.keepForDebug(file)
	return nil
}

func hookNamesForPackage(sets []*rule.InstRuleSet, pkg string) map[string]bool {
	names := make(map[string]bool)
	for _, set := range sets {
		for _, r := range set.AllFuncRules() {
			if r.Path == pkg {
				if r.Before != "" {
					names[r.Before] = true
				}
				if r.After != "" {
					names[r.After] = true
				}
			}
		}
	}
	return names
}

func guardHookFunctions(root *dst.File, names map[string]bool) (bool, error) {
	changed := false
	for _, decl := range root.Decls {
		fn, ok := decl.(*dst.FuncDecl)
		if !ok || fn.Recv != nil || !names[fn.Name.Name] || fn.Body == nil {
			continue
		}
		guard, err := ast.NewAstParser().ParseSnippet("if !otelcHookReady() { return }")
		if err != nil {
			return false, err
		}
		fn.Body.List = append(guard, fn.Body.List...)
		changed = true
	}
	return changed, nil
}

const hookReadySource = `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package %s

import otelcAtomic "sync/atomic"

var otelcHooksInitialized uint32
var otelcHooksInitWarning uint32

func init() { otelcAtomic.StoreUint32(&otelcHooksInitialized, 1) }

func otelcHookReady() bool {
 if otelcAtomic.LoadUint32(&otelcHooksInitialized) != 0 { return true }
 if otelcAtomic.CompareAndSwapUint32(&otelcHooksInitWarning, 0, 1) {
  println("otelc: skipping hooks in %s before package initialization completes")
 }
 return false
}
`
