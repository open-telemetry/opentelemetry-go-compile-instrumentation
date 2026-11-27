// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
	"go.opentelemetry.io/otelc/tool/util"
)

// namedRule is a rule of matched.json, with only its name.
type namedRule struct {
	Name string `json:"name"`
}

// matchedRuleSet holds the fields of a matched.json rule set that these tests
// check.
type matchedRuleSet struct {
	ModulePath  string                 `json:"module_path"`
	FileRules   []namedRule            `json:"file_rules"`
	StructRules map[string][]namedRule `json:"struct_rules"`
	RawRules    map[string][]namedRule `json:"raw_rules"`
}

// readMatchedRuleSets returns the rule sets of importPath in the matched.json
// that setup wrote in moduleDir.
func readMatchedRuleSets(t *testing.T, moduleDir, importPath string) []matchedRuleSet {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleDir, util.BuildTempDir, "matched.json"))
	require.NoError(t, err)
	var all []matchedRuleSet
	require.NoError(t, json.Unmarshal(raw, &all))
	var sets []matchedRuleSet
	for _, set := range all {
		if set.ModulePath == importPath {
			sets = append(sets, set)
		}
	}
	return sets
}

// ruleNames returns the names of the rules in byFile, ordered by file.
func ruleNames(byFile map[string][]namedRule) []string {
	var names []string
	for _, file := range slices.Sorted(maps.Keys(byFile)) {
		for _, r := range byFile[file] {
			names = append(names, r.Name)
		}
	}
	return names
}

// writeRulesModule writes a module named module in a new directory, with the
// pkg module replaced by the one of this repository. It returns the
// directory.
func writeRulesModule(t *testing.T, module string) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", fmt.Sprintf(`module %s

go 1.25.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s
`, module, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, moduleDir, filepath.Join("lib", "lib.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

func Answer() int { return 42 }
`)
	return moduleDir
}

// buildAndRun builds the module in moduleDir with otelc and returns the
// output of the binary.
func buildAndRun(t *testing.T, moduleDir string) string {
	t.Helper()
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	bin := filepath.Join(t.TempDir(), "app")
	if util.IsWindows() {
		bin += ".exe"
	}
	runOtelcCommand(t, moduleDir, os.Environ(), otelcPath, "go", "build", "-o", bin, ".")
	out, err := exec.CommandContext(t.Context(), bin).CombinedOutput()
	require.NoError(t, err, "running %s:\n%s", bin, out)
	return strings.ReplaceAll(string(out), "\r\n", "\n")
}

// TestRuntimeRulesKeepOtelcRuntimeGLS builds an application whose rules add a
// goroutine-local storage field and accessors to runtime, as tracers do. otelc
// also adds its own runtime_gls.go to runtime, which defines the functions a
// statement-level //otelc:ignore calls. Both must apply to the one compile of
// runtime, so the application builds, the accessors work, and the hook does
// not run for the ignored call.
func TestRuntimeRulesKeepOtelcRuntimeGLS(t *testing.T) {
	const module = "example.com/otelc-runtime-rules"
	moduleDir := writeRulesModule(t, module)
	writeTestFile(t, moduleDir, "otel.instrumentation.go", fmt.Sprintf(`// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build tools

package main

import _ "%s/rules"
`, module))
	writeTestFile(t, moduleDir, "main.go", fmt.Sprintf(`// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	_ "unsafe" // for go:linkname

	"%s/lib"
)

//go:linkname glsGet __otelc_test_gls_get
var glsGet func() any

//go:linkname glsSet __otelc_test_gls_set
var glsSet func(any)

func main() {
	glsSet("stored")
	fmt.Println("gls:", glsGet())
	fmt.Println("first:", lib.Answer())
	//otelc:ignore
	second := lib.Answer()
	fmt.Println("second:", second)
}
`, module))
	writeTestFile(t, moduleDir, filepath.Join("rules", "hooks.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package rules

import (
	"fmt"

	"go.opentelemetry.io/otelc/pkg/hook"
)

func BeforeAnswer(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("hook ran")
}
`)
	writeTestFile(t, moduleDir, filepath.Join("rules", "gls_accessors.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build ignore

package rules

import _ "unsafe" // for go:linkname

//go:linkname testGLSGet __otelc_test_gls_get
var testGLSGet = func() any { return getg().m.curg.__test_gls }

//go:linkname testGLSSet __otelc_test_gls_set
var testGLSSet = func(v any) { getg().m.curg.__test_gls = v }
`)
	writeTestFile(t, moduleDir, filepath.Join("rules", "otelc.yaml"), fmt.Sprintf(`runtime_field:
  target: runtime
  where:
    struct: g
  do:
    - add_struct_fields:
        new_field:
          - name: __test_gls
            type: any
runtime_accessors:
  target: runtime
  do:
    - add_file:
        file: gls_accessors.go
        path: %[1]s/rules
runtime_goexit1_reset:
  target: runtime
  where:
    func: goexit1
  do:
    - inject_code:
        raw: "getg().__test_gls = nil"
hook_answer:
  target: %[1]s/lib
  where:
    func: Answer
  do:
    - inject_hooks:
        before: BeforeAnswer
        path: %[1]s/rules
`, module))

	output := buildAndRun(t, moduleDir)
	assert.Equal(t, "gls: stored\nhook ran\nfirst: 42\nsecond: 42\n", output)

	sets := readMatchedRuleSets(t, moduleDir, "runtime")
	require.Len(t, sets, 1, "runtime must have one rule set")
	assert.Equal(t, []namedRule{{Name: "runtime_accessors"}, {Name: "gls_linker"}}, sets[0].FileRules)
	assert.Equal(t, []string{"runtime_field", "add_gls_field"}, ruleNames(sets[0].StructRules))
	assert.Equal(t, []string{"runtime_goexit1_reset", "goroutine_propagate"}, ruleNames(sets[0].RawRules))
}
