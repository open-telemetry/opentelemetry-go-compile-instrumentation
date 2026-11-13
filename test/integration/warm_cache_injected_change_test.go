//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
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

// TestWarmCacheSeesInjectedPackageChange covers a package that only a rule's
// imports bring into the build. dep imports extra only after instrumentation,
// and extra inlines a constant from extra2, so go has no way to tell that the
// compile of dep depends on either. A change to extra2 alone must still reach
// the binary on a warm build.
func TestWarmCacheSeesInjectedPackageChange(t *testing.T) {
	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	appDir := t.TempDir()

	writeTestFile(t, appDir, filepath.Join("extra2", "go.mod"), "module example.com/extra2\n\ngo 1.25.0\n")
	writeExtra2 := func(tag string) {
		writeTestFile(t, appDir, filepath.Join("extra2", "extra2.go"), fmt.Sprintf(`// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package extra2

const Tag = %q
`, tag))
	}
	writeExtra2("v1")

	writeTestFile(t, appDir, filepath.Join("extra", "go.mod"), `module example.com/extra

go 1.25.0

require example.com/extra2 v0.0.0

replace example.com/extra2 => ../extra2
`)
	writeTestFile(t, appDir, filepath.Join("extra", "extra.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package extra

import "example.com/extra2"

func Marker() string { return "marker-" + extra2.Tag }
`)

	writeTestFile(t, appDir, filepath.Join("dep", "dep.go"), `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dep

func Work() {}
`)
	writeTestFile(t, appDir, "main.go", `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "example.com/app/dep"

func main() { dep.Work() }
`)
	writeTestFile(t, appDir, "go.mod", fmt.Sprintf(`module example.com/app

go 1.25.0

require (
	example.com/extra v0.0.0
	example.com/extra2 v0.0.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace (
	example.com/extra => ./extra
	example.com/extra2 => ./extra2
	go.opentelemetry.io/otelc/pkg => %s
)
`, filepath.ToSlash(filepath.Join(repoRoot, "pkg"))))
	writeTestFile(t, appDir, "rules.yml", `print_marker:
  target: example.com/app/dep
  where:
    func: Work
  do:
    - inject_code:
        raw: 'println(extra.Marker())'
  imports:
    extra: "example.com/extra"
`)

	binary := filepath.Join(appDir, "app")
	if util.IsWindows() {
		binary += ".exe"
	}
	env := append(
		os.Environ(),
		"GOWORK=off",
		"GOCACHE="+filepath.Join(t.TempDir(), "gocache"),
		"OTELC_RULES=rules.yml",
	)
	buildAndRun := func() string {
		t.Helper()
		runOtelcCommand(t, appDir, env, otelcPath, "go", "build", "-o", binary, ".")
		out, runErr := exec.CommandContext(t.Context(), binary).CombinedOutput()
		require.NoError(t, runErr, "running the binary failed:\n%s", out)
		return strings.TrimSpace(string(out))
	}

	assert.Equal(t, "marker-v1", buildAndRun(), "cold build")

	writeExtra2("v2")
	assert.Equal(t, "marker-v2", buildAndRun(), "warm build after extra2 changed")
}
