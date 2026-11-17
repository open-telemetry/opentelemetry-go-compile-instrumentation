// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	_ "embed"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

const (
	TargetRuntime = "runtime"
	GLSFileName   = "runtime_gls.go"
)

//go:embed runtime_gls.go
var RuntimeGLSSource []byte

// StructRule returns the InstStructRule that adds GLS context and suppression
// fields to the runtime.g struct.
func StructRule() *rule.InstStructRule {
	return &rule.InstStructRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "add_gls_field",
			Target: rule.NewTarget(TargetRuntime),
		},
		Struct: "g",
		NewField: []*rule.InstStructField{
			{Name: "otel_trace_context", Type: "interface{}"},
			{Name: "otel_baggage_container", Type: "interface{}"},
			{Name: "otel_ignored_call_count", Type: "int"},
		},
	}
}

// RawRule returns the InstRawRule that injects the goroutine context propagation
// defer into runtime.newproc1.
func RawRule() *rule.InstRawRule {
	return &rule.InstRawRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "goroutine_propagate",
			Target: rule.NewTarget(TargetRuntime),
		},
		Func: "newproc1",
		Raw: `defer func(){
  {{.FuncReturn 0}}.otel_trace_context = propagateGoroutineContext(callergp.otel_trace_context);
  {{.FuncReturn 0}}.otel_baggage_container = propagateGoroutineContext(callergp.otel_baggage_container);
}()`,
	}
}

// FileRule returns the InstFileRule that injects runtime_gls.go into stdlib runtime.
// ResolvedPath is intentionally empty so matched.json contains no work-dir paths.
func FileRule() *rule.InstFileRule {
	return &rule.InstFileRule{
		InstBaseRule: rule.InstBaseRule{
			Name:   "gls_linker",
			Target: rule.NewTarget(TargetRuntime),
		},
		File: GLSFileName,
		Path: util.InternalPath,
	}
}

// ReadInternalFile returns the embedded source bytes for an internal runtime file.
func ReadInternalFile(name string) ([]byte, error) {
	if name == GLSFileName {
		return RuntimeGLSSource, nil
	}
	return nil, ex.Newf("unknown internal runtime file %q", name)
}

// BuildRuleSet creates the matched rule set for stdlib runtime.
func BuildRuleSet(runtime2File, procFile string) *rule.InstRuleSet {
	rset := rule.NewInstRuleSet(TargetRuntime)
	rset.PackageName = TargetRuntime
	rset.IsTest = false

	if runtime2File != "" {
		rset.AddStructRule(runtime2File, StructRule())
	}
	if procFile != "" {
		rset.AddRawRule(procFile, RawRule())
	}
	rset.AddFileRule(FileRule())

	return rset
}
