// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"path/filepath"

	"go.opentelemetry.io/otelc/tool/internal/rule"
	toolruntime "go.opentelemetry.io/otelc/tool/internal/runtime"
	"go.opentelemetry.io/otelc/tool/util"
)

// matchRuntimeGLS builds the matched InstRuleSet for standard library runtime
// when runtime is present in deps. It writes the embedded runtime_gls.go into
// the build temp directory and attaches struct rules (for runtime.g in runtime2.go),
// raw rules (for runtime.newproc1 in proc.go), and file rules (for runtime_gls.go).
func matchRuntimeGLS(deps []*Dependency) ([]*rule.InstRuleSet, error) {
	var runtimeDep *Dependency
	for _, dep := range deps {
		if dep.ImportPath == toolruntime.TargetRuntime {
			runtimeDep = dep
			break
		}
	}
	if runtimeDep == nil {
		return nil, nil
	}

	glsDir := util.GetBuildTemp("runtime")
	if err := toolruntime.MaterializeGLSSource(glsDir); err != nil {
		return nil, err
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
	rset := toolruntime.BuildRuleSet(runtime2File, procFile, glsDir)
	rset.Sources = runtimeDep.Sources
	return []*rule.InstRuleSet{rset}, nil
}
