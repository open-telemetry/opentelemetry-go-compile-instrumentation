// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests define the contract of the vendor/modules.txt helpers. In a
// vendored project, otelc keeps vendor mode (-mod=vendor). It resolves the
// modules that the injected hook code needs in a scratch module, and then
// merges the result into the vendor/modules.txt file of the user.

// realisticModulesTxt has all the line forms that `go mod vendor` writes:
// explicit modules with and without a go version, a module that is not
// explicit (only "## go"), a directory replacement, a module replacement, a
// replacement-only entry (no packages) and a wildcard replacement at the end.
const realisticModulesTxt = `# example.com/a v1.0.0
## explicit; go 1.25
example.com/a
example.com/a/sub
# example.com/b v0.3.1 => ../x
## explicit; go 1.21
example.com/b
example.com/b/internal/deep
# example.com/c v1.1.0 => example.com/y v1.2.0
## explicit
example.com/c
# example.com/indirect v0.1.0
## go 1.18
example.com/indirect/pkg
# example.com/unused v1.0.0 => ./unused
# example.com/z => ./z
`

func mustParseModulesTxt(t *testing.T, data string) *modulesTxt {
	t.Helper()
	m, err := parseModulesTxt([]byte(data))
	require.NoError(t, err)
	require.NotNil(t, m)
	return m
}

func TestParseModulesTxtRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "empty file", data: ""},
		{name: "single module", data: "# example.com/a v1.0.0\n## explicit; go 1.25\nexample.com/a\n"},
		{name: "realistic file", data: realisticModulesTxt},
		{
			name: "pseudo-version and major version suffix",
			data: "# example.com/m/v2 v2.0.0-20240101000000-abcdefabcdef\n## explicit; go 1.22\nexample.com/m/v2\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mustParseModulesTxt(t, tt.data)
			assert.Equal(t, tt.data, string(m.Format()), "Format must give back the parsed bytes")
		})
	}
}

func TestParseModulesTxtLookups(t *testing.T) {
	m := mustParseModulesTxt(t, realisticModulesTxt)

	v, ok := m.Module("example.com/a")
	assert.True(t, ok)
	assert.Equal(t, "v1.0.0", v)
	assert.True(t, m.Explicit("example.com/a"))
	assert.Equal(t, []string{"example.com/a", "example.com/a/sub"}, m.Packages("example.com/a"))

	// The version is the version of the module path (left side), not the
	// version of the replacement.
	v, ok = m.Module("example.com/c")
	assert.True(t, ok)
	assert.Equal(t, "v1.1.0", v)
	assert.True(t, m.Explicit("example.com/c"))

	v, ok = m.Module("example.com/indirect")
	assert.True(t, ok)
	assert.Equal(t, "v0.1.0", v)
	assert.False(t, m.Explicit("example.com/indirect"))
	assert.Equal(t, []string{"example.com/indirect/pkg"}, m.Packages("example.com/indirect"))

	_, ok = m.Module("example.com/absent")
	assert.False(t, ok)
	assert.False(t, m.Explicit("example.com/absent"))
	assert.Empty(t, m.Packages("example.com/absent"))
}

func TestParseModulesTxtMalformed(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "package line before any module line", data: "example.com/a\n"},
		{name: "annotation before any module line", data: "## explicit\n# example.com/a v1.0.0\nexample.com/a\n"},
		{name: "module line without version", data: "# example.com/a\n## explicit\nexample.com/a\n"},
		{name: "module line with invalid version", data: "# example.com/a notaversion\n## explicit\nexample.com/a\n"},
		{name: "replacement arrow without target", data: "# example.com/a v1.0.0 =>\n## explicit\nexample.com/a\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseModulesTxt([]byte(tt.data))
			require.Error(t, err)
		})
	}
}

// otelcPkgModule is the line form of the otelc pkg module in the scratch
// module: it is not published, so it always has a directory replacement to
// the extracted copy in .otelc-build.
const otelcPkgModule = "# go.opentelemetry.io/otelc/pkg v0.0.0-00010101000000-000000000000 => /abs/.otelc-build/pkg\n"

func TestMergeVendorModulesAddsNewModules(t *testing.T) {
	user := mustParseModulesTxt(t, `# example.com/a v1.0.0
## explicit; go 1.25
example.com/a
# example.com/z => ./z
`)
	injected := mustParseModulesTxt(t, otelcPkgModule+`## explicit; go 1.26
go.opentelemetry.io/otelc/pkg/hook
go.opentelemetry.io/otelc/pkg/hook/internal
# go.opentelemetry.io/otel v1.46.0
## go 1.24
go.opentelemetry.io/otel
go.opentelemetry.io/otel/attribute
`)

	merged, added, err := mergeVendorModules(user, injected)
	require.NoError(t, err)
	require.NotNil(t, merged)

	assert.Equal(t, []string{
		"go.opentelemetry.io/otel",
		"go.opentelemetry.io/otel/attribute",
		"go.opentelemetry.io/otelc/pkg/hook",
		"go.opentelemetry.io/otelc/pkg/hook/internal",
	}, added, "added packages are the sorted import paths of all new packages")

	v, ok := merged.Module("go.opentelemetry.io/otel")
	assert.True(t, ok)
	assert.Equal(t, "v1.46.0", v)
	// Every added module is marked explicit: otelc adds a require to go.mod for
	// it, and the vendor consistency check needs both to agree.
	assert.True(t, merged.Explicit("go.opentelemetry.io/otel"))
	assert.True(t, merged.Explicit("go.opentelemetry.io/otelc/pkg"))

	// Modules are sorted by path, like `go mod vendor` does. The go version
	// annotation and the replacement of an added module are kept. Wildcard
	// replacements of the user stay at the end.
	want := `# example.com/a v1.0.0
## explicit; go 1.25
example.com/a
# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
go.opentelemetry.io/otel/attribute
` + otelcPkgModule + `## explicit; go 1.26
go.opentelemetry.io/otelc/pkg/hook
go.opentelemetry.io/otelc/pkg/hook/internal
# example.com/z => ./z
`
	assert.Equal(t, want, string(merged.Format()))
}

func TestMergeVendorModulesKeepsUserOnlyModules(t *testing.T) {
	user := mustParseModulesTxt(t, realisticModulesTxt)
	injected := mustParseModulesTxt(t, `# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
`)

	merged, added, err := mergeVendorModules(user, injected)
	require.NoError(t, err)
	assert.Equal(t, []string{"go.opentelemetry.io/otel"}, added)

	for _, path := range []string{"example.com/a", "example.com/b", "example.com/c", "example.com/indirect"} {
		wantV, wantOK := user.Module(path)
		gotV, gotOK := merged.Module(path)
		assert.Equal(t, wantOK, gotOK, path)
		assert.Equal(t, wantV, gotV, path)
		assert.Equal(t, user.Explicit(path), merged.Explicit(path), path)
		assert.Equal(t, user.Packages(path), merged.Packages(path), path)
	}

	// The lines of the user modules (with their replacements and annotations)
	// are not changed.
	out := string(merged.Format())
	for _, block := range []string{
		"# example.com/b v0.3.1 => ../x\n## explicit; go 1.21\nexample.com/b\nexample.com/b/internal/deep\n",
		"# example.com/c v1.1.0 => example.com/y v1.2.0\n## explicit\nexample.com/c\n",
		"# example.com/indirect v0.1.0\n## go 1.18\nexample.com/indirect/pkg\n",
		"# example.com/unused v1.0.0 => ./unused\n",
		"# example.com/z => ./z\n",
	} {
		assert.Contains(t, out, block)
	}
}

func TestMergeVendorModulesSameVersionAddsMissingPackage(t *testing.T) {
	user := mustParseModulesTxt(t, `# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
`)
	injected := mustParseModulesTxt(t, `# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
go.opentelemetry.io/otel/propagation
`)

	merged, added, err := mergeVendorModules(user, injected)
	require.NoError(t, err)
	assert.Equal(t, []string{"go.opentelemetry.io/otel/propagation"}, added,
		"only the package that the user does not vendor yet is added")
	assert.Equal(t, []string{
		"go.opentelemetry.io/otel",
		"go.opentelemetry.io/otel/propagation",
	}, merged.Packages("go.opentelemetry.io/otel"))
	v, ok := merged.Module("go.opentelemetry.io/otel")
	assert.True(t, ok)
	assert.Equal(t, "v1.46.0", v)
}

func TestMergeVendorModulesNothingToAdd(t *testing.T) {
	data := `# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
`
	user := mustParseModulesTxt(t, data)
	injected := mustParseModulesTxt(t, data)

	merged, added, err := mergeVendorModules(user, injected)
	require.NoError(t, err)
	assert.Empty(t, added)
	assert.Equal(t, data, string(merged.Format()))
}

func TestMergeVendorModulesHigherVersionConflict(t *testing.T) {
	tests := []struct {
		name     string
		vendored string
		required string
	}{
		{name: "patch and minor", vendored: "v1.30.0", required: "v1.46.0"},
		// Versions compare as semver, not as strings: v1.10.0 > v1.9.0.
		{name: "semver not lexical", vendored: "v1.9.0", required: "v1.10.0"},
		{name: "pseudo-version", vendored: "v0.0.0-20240101000000-abcdefabcdef", required: "v0.1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := func(version string) string {
				return "# example.com/dep " + version + "\n## explicit; go 1.22\nexample.com/dep\n"
			}
			user := mustParseModulesTxt(t, entry(tt.vendored))
			injected := mustParseModulesTxt(t, entry(tt.required))
			before := string(user.Format())

			_, _, err := mergeVendorModules(user, injected)
			require.Error(t, err)

			var conflict *vendorConflictError
			require.ErrorAs(t, err, &conflict)
			assert.Equal(t, "example.com/dep", conflict.Module)
			assert.Equal(t, tt.vendored, conflict.VendoredVersion)
			assert.Equal(t, tt.required, conflict.RequiredVersion)

			msg := err.Error()
			assert.Contains(t, msg, "example.com/dep")
			assert.Contains(t, msg, tt.vendored)
			assert.Contains(t, msg, tt.required)

			// The vendored code of the user is never changed.
			assert.Equal(t, before, string(user.Format()))
		})
	}
}

func TestMergeVendorModulesLowerVersionKeepsUserVersion(t *testing.T) {
	user := mustParseModulesTxt(t, `# go.opentelemetry.io/otel v1.50.0
## explicit; go 1.24
go.opentelemetry.io/otel
go.opentelemetry.io/otel/attribute
`)
	injected := mustParseModulesTxt(t, `# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
`)

	merged, added, err := mergeVendorModules(user, injected)
	require.NoError(t, err)
	assert.Empty(t, added)

	v, ok := merged.Module("go.opentelemetry.io/otel")
	assert.True(t, ok)
	assert.Equal(t, "v1.50.0", v, "the higher version of the user is kept (MVS)")
	assert.Equal(t, string(user.Format()), string(merged.Format()))
}

func TestMergeVendorModulesLowerVersionMissingPackageConflict(t *testing.T) {
	user := mustParseModulesTxt(t, `# go.opentelemetry.io/otel v1.50.0
## explicit; go 1.24
go.opentelemetry.io/otel
`)
	injected := mustParseModulesTxt(t, `# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
go.opentelemetry.io/otel/propagation
`)

	_, _, err := mergeVendorModules(user, injected)
	require.Error(t, err)

	// The package must come from the vendored version v1.50.0, but the
	// scratch module only has it at v1.46.0. Mixing versions of one module is
	// not permitted, so this is a conflict too.
	var conflict *vendorConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "go.opentelemetry.io/otel", conflict.Module)
	assert.Equal(t, "v1.50.0", conflict.VendoredVersion)
	assert.Equal(t, "v1.46.0", conflict.RequiredVersion)
	assert.Equal(t, "go.opentelemetry.io/otel/propagation", conflict.Package)

	msg := err.Error()
	assert.Contains(t, msg, "go.opentelemetry.io/otel/propagation")
	assert.Contains(t, msg, "v1.50.0")
	assert.Contains(t, msg, "v1.46.0")
}

func TestVendorConflictErrorMessage(t *testing.T) {
	err := &vendorConflictError{
		Module:          "go.opentelemetry.io/otel",
		VendoredVersion: "v1.30.0",
		RequiredVersion: "v1.46.0",
	}
	msg := err.Error()
	assert.Contains(t, msg, "go.opentelemetry.io/otel")
	assert.Contains(t, msg, "v1.30.0")
	assert.Contains(t, msg, "v1.46.0")

	var target *vendorConflictError
	require.ErrorAs(t, error(err), &target)
	assert.Same(t, err, target)
}

func TestMergeVendorModulesIsDeterministic(t *testing.T) {
	userData := realisticModulesTxt
	injectedData := otelcPkgModule + `## explicit; go 1.26
go.opentelemetry.io/otelc/pkg/hook
# go.opentelemetry.io/otel/trace v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel/trace
# go.opentelemetry.io/otel v1.46.0
## explicit; go 1.24
go.opentelemetry.io/otel
# github.com/go-logr/logr v1.4.3
## explicit; go 1.18
github.com/go-logr/logr
`

	var first string
	var firstAdded []string
	for i := range 10 {
		user := mustParseModulesTxt(t, userData)
		injected := mustParseModulesTxt(t, injectedData)
		merged, added, err := mergeVendorModules(user, injected)
		require.NoError(t, err)
		out := string(merged.Format())
		if i == 0 {
			first, firstAdded = out, added
			continue
		}
		assert.Equal(t, first, out, "merge output must be the same on each run")
		assert.Equal(t, firstAdded, added)
	}

	// A merged file must also parse again and give the same bytes.
	reparsed := mustParseModulesTxt(t, first)
	assert.Equal(t, first, string(reparsed.Format()))
}
