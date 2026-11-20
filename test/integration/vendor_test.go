//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

const vendoredApp = "vendored"

func TestVendoredBuild(t *testing.T) {
	t.Parallel()

	otelcPath, err := testutil.OtelcPath()
	require.NoError(t, err)
	otelcPath, err = filepath.Abs(otelcPath)
	require.NoError(t, err)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)

	appsDir := t.TempDir()
	app := filepath.Join(appsDir, vendoredApp)
	hooksDir := filepath.Join(app, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))

	repoRootSlash := filepath.ToSlash(repoRoot)

	goModContent := fmt.Sprintf(`module example.com/vendored

go 1.26.0

require (
	example.com/vendored/hooks v0.0.0
	golang.org/x/time v0.16.0
	go.opentelemetry.io/otelc/pkg v0.0.0
)

replace (
	example.com/vendored/hooks => ./hooks
	go.opentelemetry.io/otelc => %s
	go.opentelemetry.io/otelc/pkg => %s/pkg
)
`, repoRootSlash, repoRootSlash)

	goSumContent := `golang.org/x/time v0.16.0 h1:vMb6ptszcQMkcwiRTAuNNU50gom6++Q/6gY2hDM6VDE=
golang.org/x/time v0.16.0/go.mod h1:rVKOqvZeKvrDKTQiAHJ7wmwP0RzleSphoEA9RcdLA0s=
`

	toolsContent := `//go:build tools

package tools

import _ "example.com/vendored/hooks"
`

	hooksGoMod := fmt.Sprintf(`module example.com/vendored/hooks

go 1.26.0

require go.opentelemetry.io/otelc/pkg v0.0.0

replace go.opentelemetry.io/otelc/pkg => %s/pkg
`, repoRootSlash)

	hooksSrc := `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"
	"go.opentelemetry.io/otelc/pkg/hook"
)

func Before(ctx hook.HookContext) {
	_ = ctx
	fmt.Println("vendored hook executed")
}
`

	hooksYaml := `hook_work:
  target: main
  where:
    func: RunApp
  do:
    - inject_hooks:
        before: Before
        path: example.com/vendored/hooks
`

	mainSrc := `// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"golang.org/x/time/rate"
)

func RunApp() {
	limiter := rate.NewLimiter(1, 1)
	fmt.Println("rate limit:", limiter.Limit())
}

func main() {
	RunApp()
}
`

	require.NoError(t, os.WriteFile(filepath.Join(app, "go.mod"), []byte(goModContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(app, "go.sum"), []byte(goSumContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(app, "otel.instrumentation.go"), []byte(toolsContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(app, "main.go"), []byte(mainSrc), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "go.mod"), []byte(hooksGoMod), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "hooks.go"), []byte(hooksSrc), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, "otelc.yaml"), []byte(hooksYaml), 0o644))

	goMod(t, app, "vendor")
	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	// setup edits go.mod but not vendor/modules.txt, so the build needs -mod=mod
	// to pass the vendor consistency check.
	testutil.Build(t, appsDir, vendoredApp, "go", "build")

	binPath := filepath.Join(app, "app")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), binPath)
	cmd.Dir = app
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "vendored hook executed")
	require.Contains(t, string(out), "rate limit: 1")

	after, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)
	require.Equal(t, before, after, "otelc must not modify the vendor directory")
}

func goMod(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", append([]string{"mod"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
