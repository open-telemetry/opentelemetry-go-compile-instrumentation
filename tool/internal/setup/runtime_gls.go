// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"path/filepath"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	toolruntime "go.opentelemetry.io/otelc/tool/internal/runtime"
)

// matchRuntimeGLS builds the matched InstRuleSet for standard library runtime
// when runtime is present in deps. It attaches struct rules (for runtime.g in
// runtime2.go), raw rules (for runtime.newproc1 in proc.go), and an internal
// file rule (for runtime_gls.go) read from embedded sources during instrumentation.
func matchRuntimeGLS(deps []*Dependency) []*rule.InstRuleSet {
	var runtimeDep *Dependency
	for _, dep := range deps {
		if dep.ImportPath == toolruntime.TargetRuntime {
			runtimeDep = dep
			break
		}
	}
	if runtimeDep == nil {
		return nil
	}

	var runtime2File, procFile string
	for _, src := range runtimeDep.Sources {
		switch filepath.Base(src) {
		case "runtime2.go":
			runtime2File = src
		case "proc.go":
			procFile = src
		}
	}
	return []*rule.InstRuleSet{toolruntime.BuildRuleSet(runtime2File, procFile)}
}
