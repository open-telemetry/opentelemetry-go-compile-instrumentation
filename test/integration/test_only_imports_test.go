//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

// TestRuleImportAliasForTestOnlyImport checks that otelc go test rewrites a
// rule's import alias to the name a _test.go file uses for the same path,
// when no other file of the package imports that path.
func TestRuleImportAliasForTestOnlyImport(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	moduleDir := t.TempDir()
	writeTestFile(t, moduleDir, "go.mod", `module example.com/otelc-testimports

go 1.25.0
`)
	writeTestFile(t, moduleDir, filepath.Join("lib", "lib.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package lib

func Do() int { return 1 }
`)
	writeTestFile(t, moduleDir, filepath.Join("helper", "helper.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package helper

func Wrap(v int) int { return v + 1 }

func Name() string { return "helper" }
`)
	writeTestFile(t, moduleDir, filepath.Join("svc", "svc.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package svc

func Hello() string { return "hello" }
`)
	writeTestFile(t, moduleDir, filepath.Join("svc", "svc_test.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package svc

import (
	"testing"

	"example.com/otelc-testimports/helper"
	"example.com/otelc-testimports/lib"
)

func TestDo(t *testing.T) {
	if got := lib.Do(); got != 2 {
		t.Fatalf("lib.Do() = %d, want 2 (%s)", got, helper.Name())
	}
}
`)
	writeTestFile(t, moduleDir, "rules.yml", `wrap_do:
  target: example.com/otelc-testimports/svc
  where:
    function_call: example.com/otelc-testimports/lib.Do
  do:
    - wrap_call:
        replace: "h.Wrap({{ . }})"
  imports:
    h: example.com/otelc-testimports/helper
`)

	env := append(os.Environ(), "OTELC_RULES=rules.yml")
	output := runOtelcCommand(t, moduleDir, env, otelcPath, "go", "test", "-count=1", "-v", "./svc")
	require.Contains(t, output, "--- PASS: TestDo")
}
