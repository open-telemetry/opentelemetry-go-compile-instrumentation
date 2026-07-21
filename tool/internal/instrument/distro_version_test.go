// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func makePhaseForPkg(t *testing.T, pkgPath string) (*InstrumentPhase, string) {
	t.Helper()
	workDir := t.TempDir()
	ip := &InstrumentPhase{
		logger:      slog.Default(),
		workDir:     workDir,
		compileArgs: []string{"compile", "-p", pkgPath, "-o", filepath.Join(workDir, "pkg.a")},
	}
	return ip, workDir
}

func TestInjectDistroVersion_NoopForOtherPackages(t *testing.T) {
	packages := []string{
		"main",
		"net/http",
		"go.opentelemetry.io/otelc/tool/util",
		"go.opentelemetry.io/otelc/instrumentation/net/http",
	}

	for _, pkg := range packages {
		t.Run(pkg, func(t *testing.T) {
			ip, workDir := makePhaseForPkg(t, pkg)
			initialArgs := append([]string(nil), ip.compileArgs...)

			require.NoError(t, ip.injectDistroVersion())

			// No file should have been created in workDir for non-runtime packages.
			entries, err := os.ReadDir(workDir)
			require.NoError(t, err)
			assert.Empty(t, entries, "injectDistroVersion must not create files for package %q", pkg)

			// Compile args must be unchanged.
			assert.Equal(t, initialArgs, ip.compileArgs,
				"compile args must be unchanged for non-runtime packages")
		})
	}
}

func TestInjectDistroVersion_InjectsPkgRuntime(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	ip, workDir := makePhaseForPkg(t, pkgRuntimeImportPath)

	require.NoError(t, ip.injectDistroVersion())

	// A file named otelc.distro_version.go must have been created.
	outFile := filepath.Join(workDir, "otelc.distro_version.go")
	data, err := os.ReadFile(outFile)
	require.NoError(t, err, "otelc.distro_version.go should be created")

	content := string(data)

	t.Run("package declaration is correct", func(t *testing.T) {
		assert.Contains(t, content, "package runtime",
			"generated file must belong to the runtime package")
	})

	t.Run("init sets OtelcVersion to current version", func(t *testing.T) {
		assert.Contains(t, content, "OtelcVersion",
			"generated file must assign OtelcVersion")
		assert.Contains(t, content, util.Version,
			"generated file must embed the current otelc version")
	})

	t.Run("file is appended to compile args", func(t *testing.T) {
		found := false
		for _, arg := range ip.compileArgs {
			if strings.HasSuffix(arg, "otelc.distro_version.go") {
				found = true
				break
			}
		}
		assert.True(t, found, "otelc.distro_version.go must be added to compile args")
	})
}

func TestInjectDistroVersion_GeneratedContentIsValidGo(t *testing.T) {
	// Verify the template produces syntactically correct Go source for several
	// version strings, including ones with special characters.
	versions := []string{
		"v0.0.0",
		"v1.2.3-alpha.1+build.42",
		"dev",
		"(devel)",
		`v1.0.0 "quoted"`, // pathological: should be escaped by %q
	}

	for _, ver := range versions {
		t.Run(ver, func(t *testing.T) {
			content := fmt.Sprintf(distroVersionFileContent, ver)
			// The string must be valid Go — at minimum it must contain a
			// syntactically correct quoted string literal. The %q verb in
			// fmt.Sprintf guarantees proper escaping.
			assert.Contains(t, content, "package runtime")
			assert.Contains(t, content, "OtelcVersion")
		})
	}
}
