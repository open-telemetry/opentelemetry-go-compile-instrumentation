// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	goast "go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestGLSEmbeddedSource(t *testing.T) {
	require.NotEmpty(t, RuntimeGLSSource)

	// Verify that the embedded file parses as valid Go after stripping build tags
	stripped := ast.StripBuildIgnoreTag(string(RuntimeGLSSource))
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, GLSFileName, stripped, 0)
	require.NoError(t, err)
	assert.Equal(t, "runtime", file.Name.Name)

	// API Stability Policy Enforcement:
	// Verify that all required function signatures are declared and preserved.
	expectedFuncs := map[string]bool{
		"GetTraceContextFromGLS":     false,
		"GetBaggageContainerFromGLS": false,
		"SetTraceContextToGLS":       false,
		"SetBaggageContainerToGLS":   false,
		"SuppressHooks":              false,
		"UnsuppressHooks":            false,
		"HooksSuppressed":            false,
		"propagateGoroutineContext":  false,
	}

	for _, decl := range file.Decls {
		if fn, ok := decl.(*goast.FuncDecl); ok {
			if _, expected := expectedFuncs[fn.Name.Name]; expected {
				expectedFuncs[fn.Name.Name] = true
			}
		}
	}

	for name, found := range expectedFuncs {
		assert.Truef(t, found, "API Stability violation: required function %q is missing from runtime_gls.go", name)
	}

	// Verify OtelContextCloner interface is present
	assert.Contains(t, stripped, "type OtelContextCloner interface")
}

func TestRules(t *testing.T) {
	sr := StructRule()
	require.NotNil(t, sr)
	assert.Equal(t, "add_gls_field", sr.Name)
	assert.Equal(t, "g", sr.Struct)
	assert.Len(t, sr.NewField, 3)

	fields := make(map[string]string)
	for _, f := range sr.NewField {
		fields[f.Name] = f.Type
	}
	assert.Equal(t, "interface{}", fields["otel_trace_context"])
	assert.Equal(t, "interface{}", fields["otel_baggage_container"])
	assert.Equal(t, "int", fields["otel_ignored_call_count"])

	rr := RawRule()
	require.NotNil(t, rr)
	assert.Equal(t, "goroutine_propagate", rr.Name)
	assert.Equal(t, "newproc1", rr.Func)
	assert.Contains(t, rr.Raw, "propagateGoroutineContext")
	assert.Contains(t, rr.Raw, "{{.FuncReturn 0}}")

	fr := FileRule()
	require.NotNil(t, fr)
	assert.Equal(t, "gls_linker", fr.Name)
	assert.Equal(t, GLSFileName, fr.File)
	assert.Equal(t, util.InternalPath, fr.Path)
	assert.Empty(t, fr.ResolvedPath)
}

func TestReadInternalFile(t *testing.T) {
	content, err := ReadInternalFile(GLSFileName)
	require.NoError(t, err)
	assert.Equal(t, RuntimeGLSSource, content)

	_, err = ReadInternalFile("unknown.go")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown internal runtime file")
}

func TestBuildRuleSet(t *testing.T) {
	runtime2File := filepath.Join(t.TempDir(), "runtime2.go")
	procFile := filepath.Join(t.TempDir(), "proc.go")

	rset := BuildRuleSet(runtime2File, procFile)
	require.NotNil(t, rset)
	assert.Equal(t, TargetRuntime, rset.ModulePath)
	assert.Equal(t, TargetRuntime, rset.PackageName)
	assert.False(t, rset.IsTest)
	assert.Len(t, rset.StructRules[runtime2File], 1)
	assert.Len(t, rset.RawRules[procFile], 1)
	require.Len(t, rset.FileRules, 1)
	assert.Empty(t, rset.FileRules[0].ResolvedPath)
}
