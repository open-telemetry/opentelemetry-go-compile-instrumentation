//go:build integration

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/test/testutil"
)

const vendoredApp = "vendored"

const vendoredAppGoMod = `module vendored

go 1.25.0

require github.com/gin-gonic/gin v1.12.0
`

const vendoredAppMain = `package main

import (
	"flag"
	"fmt"

	"github.com/gin-gonic/gin"
)

func main() {
	port := flag.Int("port", 8080, "listen port")
	flag.Parse()

	r := gin.New()
	r.GET("/hello/:name", func(c *gin.Context) {
		c.String(200, "Hello %s", c.Param("name"))
	})

	if err := r.Run(fmt.Sprintf(":%d", *port)); err != nil {
		panic(err)
	}
}
`

func TestVendoredBuild(t *testing.T) {
	t.Parallel()

	appsDir := t.TempDir()
	app := filepath.Join(appsDir, vendoredApp)
	require.NoError(t, os.MkdirAll(app, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(app, "go.mod"), []byte(vendoredAppGoMod), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(app, "main.go"), []byte(vendoredAppMain), 0o644))

	goMod(t, app, "tidy")
	goMod(t, app, "vendor")
	modulesTxt := filepath.Join(app, "vendor", "modules.txt")
	before, err := os.ReadFile(modulesTxt)
	require.NoError(t, err)

	// setup edits go.mod but not vendor/modules.txt, so the build needs -mod=mod
	// to pass the vendor consistency check.
	testutil.Build(t, appsDir, vendoredApp, "go", "build")

	port := testutil.FreePort(t)

	f := testutil.NewTestFixture(t, testutil.WithAppsDir(appsDir))
	f.Start(vendoredApp, fmt.Sprintf("-port=%d", port))

	testutil.WaitForTCP(t, fmt.Sprintf("127.0.0.1:%d", port))

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/hello/OpenTelemetry", port)) //nolint:noctx
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	testutil.WaitForSpanFlush(t)

	f.RequireTraceCount(1)
	f.RequireSpansPerTrace(1)

	span := testutil.RequireSpan(t, f.Traces(), testutil.IsServer)
	require.Equal(t, "GET /hello/:name", span.Name())

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
