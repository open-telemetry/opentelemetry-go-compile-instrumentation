// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestVendoringActive(t *testing.T) {
	writeVendoredModule := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "vendor", "modules.txt"), []byte(""), 0o644))
		return root
	}

	t.Run("vendor present at module root", func(t *testing.T) {
		assert.True(t, vendoringActive(t.Context(), writeVendoredModule(t)))
	})
	t.Run("vendor found from subdirectory", func(t *testing.T) {
		root := writeVendoredModule(t)
		sub := filepath.Join(root, "cmd")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		assert.True(t, vendoringActive(t.Context(), sub))
	})
	t.Run("module without vendor", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
		assert.False(t, vendoringActive(t.Context(), root))
	})
	t.Run("no module", func(t *testing.T) {
		assert.False(t, vendoringActive(t.Context(), t.TempDir()))
	})
	t.Run("build directory does not exist", func(t *testing.T) {
		// go env cannot start in a missing directory, so the lookup fails and
		// the build is treated as not vendored.
		assert.False(t, vendoringActive(t.Context(), filepath.Join(t.TempDir(), "missing")))
	})
	t.Run("workspace vendor present at workspace root", func(t *testing.T) {
		root := writeVendoredModule(t)
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "go.work"),
			[]byte("go 1.25.0\n\nuse .\n"),
			0o644,
		))
		t.Setenv("GOWORK", "")
		assert.True(t, vendoringActive(t.Context(), root))
	})
	t.Run("per-module vendor ignored when workspace has no root vendor", func(t *testing.T) {
		ws := t.TempDir()
		mod := filepath.Join(ws, "app")
		require.NoError(t, os.MkdirAll(filepath.Join(mod, "vendor"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(mod, "vendor", "modules.txt"), []byte(""), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(ws, "go.work"), []byte("go 1.25.0\n\nuse ./app\n"), 0o644))
		t.Setenv("GOWORK", "")
		assert.False(t, vendoringActive(t.Context(), ws))
		assert.False(t, vendoringActive(t.Context(), mod))
	})
}

func TestBuildDir(t *testing.T) {
	tests := []struct {
		name       string
		subcommand string
		args       []string
		want       string
	}{
		{"no -C", subcmdBuild, []string{"-o", "out", "."}, ""},
		{"separate value", subcmdBuild, []string{"-C", "app", "."}, "app"},
		{"joined value", subcmdBuild, []string{"-C=app", "."}, "app"},
		{"double dash", subcmdBuild, []string{"--C", "app", "."}, "app"},
		{"go test", subcmdTest, []string{"-C", "app", "-run", "TestX", "."}, "app"},
		{"value of another flag", subcmdBuild, []string{"-tags", "-C", "."}, ""},
		{"test binary argument", subcmdTest, []string{".", "-args", "-C", "app"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildDir(tt.subcommand, tt.args))
		})
	}
}

// TestPrepareVendoredBuildInBuildDir checks that vendoring is detected in the
// directory the go command builds in, which is neither the otelc work
// directory nor, with -C, the current directory.
func TestPrepareVendoredBuildInBuildDir(t *testing.T) {
	// -mod=mod is forbidden in a workspace, so a stray ambient go.work would
	// otherwise hide the vendor directory.
	t.Setenv("GOWORK", "off")

	writeVendoredModule := func(t *testing.T, dir string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "vendor"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor", "modules.txt"), []byte(""), 0o644))
	}

	t.Run("relative -C directory", func(t *testing.T) {
		root := t.TempDir()
		writeVendoredModule(t, filepath.Join(root, "app"))
		t.Chdir(root)
		t.Setenv(util.EnvOtelcWorkDir, root)
		t.Setenv("GOFLAGS", "")

		opts := PinOptions{Args: []string{"-C", "app", "-mod=vendor", "."}}
		_, _, err := resolvePinModuleDirs(t.Context(), &opts)
		require.NoError(t, err)

		assert.Equal(t, []string{"-C", "app", "-mod=mod", "."}, opts.Args)
		assert.Equal(t, modMod, os.Getenv("GOFLAGS"))
	})

	t.Run("work dir outside the module", func(t *testing.T) {
		module := t.TempDir()
		writeVendoredModule(t, module)
		t.Chdir(module)
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		t.Setenv("GOFLAGS", "")

		opts := PinOptions{Args: []string{"-mod=vendor", "."}}
		_, _, err := resolvePinModuleDirs(t.Context(), &opts)
		require.NoError(t, err)

		assert.Equal(t, []string{"-mod=mod", "."}, opts.Args)
		assert.Equal(t, modMod, os.Getenv("GOFLAGS"))
	})
}

func TestForceModMod(t *testing.T) {
	tests := []struct {
		name    string
		goflags string
		want    string
	}{
		{"empty appends", "", "-mod=mod"},
		{"no mod token appends", "-trimpath", "-trimpath -mod=mod"},
		{"vendor overridden", "-mod=vendor", "-mod=mod"},
		{"vendor overridden among flags", "-trimpath -mod=vendor", "-trimpath -mod=mod"},
		{"mod left unchanged", "-mod=mod", "-mod=mod"},
		{"readonly left unchanged", "-mod=readonly", "-mod=readonly"},
		{"readonly among flags left unchanged", "-trimpath -mod=readonly", "-trimpath -mod=readonly"},
		// Go applies last-wins for a repeated flag, so every -mod=vendor must be
		// rewritten and no -mod=mod appended when a -mod token already exists.
		{"readonly then vendor last wins", "-mod=readonly -mod=vendor", "-mod=readonly -mod=mod"},
		{"duplicate vendor both rewritten", "-mod=vendor -mod=vendor", "-mod=mod -mod=mod"},
		{"bare mod left as is, no append", "-mod", "-mod"},
		// A -mod substring in another flag is not a -mod token, so -mod=mod is appended.
		{"modcacherw is not a mod flag", "-modcacherw=true", "-modcacherw=true -mod=mod"},
		{"mod substring in another value", "-ldflags=-X=v=-mod=x", "-ldflags=-X=v=-mod=x -mod=mod"},
		// Go's flag parser treats the double-dash form the same as single-dash.
		{"double-dash vendor overridden", "--mod=vendor", "-mod=mod"},
		{"double-dash mod left unchanged, no append", "--mod=mod", "--mod=mod"},
		{"double-dash readonly left unchanged, no append", "--mod=readonly", "--mod=readonly"},
		{"bare double-dash mod left as is, no append", "--mod", "--mod"},
		// The go command accepts quoted GOFLAGS tokens. A quoted -mod flag is
		// still a -mod flag, so it must be recognized and not have -mod=mod
		// appended after it, which would win by last-value.
		{"single-quoted readonly left unchanged", "'-mod=readonly'", "'-mod=readonly'"},
		{"double-quoted readonly left unchanged", `"-mod=readonly"`, `"-mod=readonly"`},
		{"quoted readonly among flags left unchanged", "-trimpath '-mod=readonly'", "-trimpath '-mod=readonly'"},
		{"quoted vendor overridden", "'-mod=vendor'", "-mod=mod"},
		{"quoted double-dash readonly left unchanged", "'--mod=readonly'", "'--mod=readonly'"},
		{"quoted double-dash vendor overridden", "'--mod=vendor'", "-mod=mod"},
		// A quoted flag whose value contains a space stays one token, so the
		// following -mod flag is still recognized and not clobbered.
		{"spaced quoted flag before quoted mod", "-tags='foo bar' '-mod=readonly'", "-tags='foo bar' '-mod=readonly'"},
		{"spaced quoted flag no mod appends once", "-tags='foo bar'", "-tags='foo bar' -mod=mod"},
		// The go command strips only a surrounding quote pair, so -mod='vendor' keeps
		// its quoted value and is not a -mod=vendor token. It stays as written and go
		// rejects the value later.
		{"value-quoted vendor preserved; go rejects it later", "-mod='vendor'", "-mod='vendor'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, forceModMod(tt.goflags))
		})
	}
}

func TestRewriteModVendor(t *testing.T) {
	tests := []struct {
		name       string
		subcommand string
		args       []string
		want       []string
	}{
		{
			name:       "vendor single token",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod=vendor", "./..."},
			want:       []string{"build", "-mod=mod", "./..."},
		},
		{
			name:       "vendor two token",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod", "vendor", "./..."},
			want:       []string{"build", "-mod", "mod", "./..."},
		},
		{
			name:       "readonly untouched",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod=readonly", "./..."},
			want:       []string{"build", "-mod=readonly", "./..."},
		},
		{
			name:       "mod untouched",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod=mod", "./..."},
			want:       []string{"build", "-mod=mod", "./..."},
		},
		{
			name:       "no mod flag",
			subcommand: subcmdBuild,
			args:       []string{"build", "-race", "./..."},
			want:       []string{"build", "-race", "./..."},
		},
		// -mod as the last arg: the two-token branch must not index past the end.
		{
			name:       "bare mod as last arg",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod"},
			want:       []string{"build", "-mod"},
		},
		{
			name:       "multiple occurrences all rewritten",
			subcommand: subcmdBuild,
			args:       []string{"build", "-mod=vendor", "-o", "x", "-mod", "vendor"},
			want:       []string{"build", "-mod=mod", "-o", "x", "-mod", "mod"},
		},
		// A positional "vendor" not preceded by -mod is a build target, not a flag.
		{
			name:       "positional vendor left alone",
			subcommand: subcmdBuild,
			args:       []string{"build", "vendor"},
			want:       []string{"build", "vendor"},
		},
		// Go's flag parser treats the double-dash form the same as single-dash.
		{
			name:       "double-dash vendor single token",
			subcommand: subcmdBuild,
			args:       []string{"build", "--mod=vendor", "./..."},
			want:       []string{"build", "-mod=mod", "./..."},
		},
		{
			name:       "double-dash vendor two token",
			subcommand: subcmdBuild,
			args:       []string{"build", "--mod", "vendor", "./..."},
			want:       []string{"build", "--mod", "mod", "./..."},
		},
		{
			name:       "double-dash readonly untouched",
			subcommand: subcmdBuild,
			args:       []string{"build", "--mod=readonly", "./..."},
			want:       []string{"build", "--mod=readonly", "./..."},
		},
		// Test-binary arguments after delimiters must never be mutated
		{
			name:       "test delimiter dash-dash preserves -mod=vendor tail",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "--", "-mod=vendor"},
			want:       []string{"./pkg", "--", "-mod=vendor"},
		},
		{
			name:       "test delimiter -args preserves -mod=vendor tail",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-args", "-mod=vendor"},
			want:       []string{"./pkg", "-args", "-mod=vendor"},
		},
		{
			name:       "test delimiter --args preserves -mod=vendor tail",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "--args", "-mod=vendor"},
			want:       []string{"./pkg", "--args", "-mod=vendor"},
		},
		{
			name:       "test delimiter dash-dash preserves -mod vendor two-token tail",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "--", "-mod", "vendor"},
			want:       []string{"./pkg", "--", "-mod", "vendor"},
		},
		// Test flag values that resemble build flags must never be mutated
		{
			name:       "test flag value -test.run preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"-test.run", "-mod=vendor", "./pkg"},
			want:       []string{"-test.run", "-mod=vendor", "./pkg"},
		},
		{
			name:       "test flag value -run preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"-run", "-mod=vendor", "./pkg"},
			want:       []string{"-run", "-mod=vendor", "./pkg"},
		},
		// Real build flags in go test must be rewritten
		{
			name:       "genuine build flag in go test is rewritten",
			subcommand: subcmdTest,
			args:       []string{"-mod=vendor", "./pkg"},
			want:       []string{"-mod=mod", "./pkg"},
		},
		{
			name:       "genuine two-token build flag in go test is rewritten",
			subcommand: subcmdTest,
			args:       []string{"-mod", "vendor", "./pkg"},
			want:       []string{"-mod", "mod", "./pkg"},
		},
		// Unknown test flag value is preserved
		{
			name:       "unknown test flag value preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-custom=-mod=vendor"},
			want:       []string{"./pkg", "-custom=-mod=vendor"},
		},
		// Positional test-argv boundary preserves -mod=vendor
		{
			name:       "positional test arg after unjoined unknown flag preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-custom", "value", "positional", "-mod=vendor"},
			want:       []string{"./pkg", "-custom", "value", "positional", "-mod=vendor"},
		},
		{
			name:       "joined unknown flag followed by positional preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-custom=x", "positional", "-mod=vendor"},
			want:       []string{"./pkg", "-custom=x", "positional", "-mod=vendor"},
		},
		{
			name:       "definitive positional tail after unknown flag preserves -mod=vendor",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-custom=x", "positional", "-race", "-mod=vendor", "-tags=x", "./other"},
			want:       []string{"./pkg", "-custom=x", "positional", "-race", "-mod=vendor", "-tags=x", "./other"},
		},
		{
			name:       "joined vendor flag in the positional tail is preserved",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-run", "TestX", "math", "-mod=vendor"},
			want:       []string{"./pkg", "-run", "TestX", "math", "-mod=vendor"},
		},
		{
			name:       "separated vendor flag in the positional tail is preserved",
			subcommand: subcmdTest,
			args:       []string{"./pkg", "-run", "TestX", "math", "-mod", "vendor"},
			want:       []string{"./pkg", "-run", "TestX", "math", "-mod", "vendor"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcmd := tt.subcommand
			if subcmd == "" {
				subcmd = subcmdBuild
			}
			assert.Equal(t, tt.want, rewriteModVendor(subcmd, tt.args, modMod))
		})
	}

	t.Run("rewrites to -mod=readonly when requested", func(t *testing.T) {
		assert.Equal(
			t,
			[]string{"build", "-mod=readonly", "./..."},
			rewriteModVendor(subcmdBuild, []string{"build", "-mod=vendor", "./..."}, modReadonly),
		)
		assert.Equal(
			t,
			[]string{"build", "-mod", "readonly", "./..."},
			rewriteModVendor(subcmdBuild, []string{"build", "-mod", "vendor", "./..."}, modReadonly),
		)
	})
}

func TestForceModFlag_Readonly(t *testing.T) {
	assert.Equal(t, "-mod=readonly", forceModFlag("", modReadonly))
	assert.Equal(t, "-trimpath -mod=readonly", forceModFlag("-trimpath -mod=vendor", modReadonly))
	assert.Equal(t, "-mod=readonly", forceModFlag("--mod=vendor", modReadonly))
	assert.Equal(t, "-mod=readonly", forceModFlag("-mod=readonly", modReadonly))
}

func TestSetupLockedForcesModuleModeForVendoredBuildDir(t *testing.T) {
	// -mod=mod is forbidden in a workspace, so a stray ambient go.work would
	// otherwise hide the vendor directory.
	t.Setenv("GOWORK", "off")

	root := t.TempDir()
	app := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(filepath.Join(app, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(app, "go.mod"), []byte("module m\n\ngo 1.25.0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(app, "vendor", "modules.txt"), []byte(""), 0o644))
	t.Chdir(root)
	t.Setenv(util.EnvOtelcWorkDir, root)
	t.Setenv("GOFLAGS", "")

	// The package does not exist, so setup stops right after the vendor
	// check, before it edits any files.
	cmd := &cli.Command{Name: "go", SkipFlagParsing: true, Action: setupLocked}
	err := cmd.Run(t.Context(), []string{"go", "build", "-C", "app", "./missing"})
	require.Error(t, err)

	assert.Equal(t, modMod, os.Getenv("GOFLAGS"))
}

func TestSetupLockedForcesReadonlyForUnvendoredWorkspace(t *testing.T) {
	ws := t.TempDir()
	app := filepath.Join(ws, "app")
	require.NoError(t, os.MkdirAll(app, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "go.work"), []byte("go 1.25.0\n\nuse ./app\n"), 0o644))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(app, "go.mod"), []byte("module example.com/app\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "vendor", "modules.txt"), []byte("## workspace\n"), 0o644))
	t.Chdir(ws)
	t.Setenv(util.EnvOtelcWorkDir, ws)
	t.Setenv("GOWORK", "")
	t.Setenv("GOFLAGS", "-mod=vendor")

	cmd := &cli.Command{Name: "go", SkipFlagParsing: true, Action: setupLocked}
	err := cmd.Run(t.Context(), []string{"go", "build", "./app/missing"})
	require.Error(t, err)

	assert.Equal(t, modReadonly, os.Getenv("GOFLAGS"))
}

func TestParseVendorModules(t *testing.T) {
	dir := t.TempDir()
	modulesTxt := filepath.Join(dir, "modules.txt")
	content := strings.Join([]string{
		"# github.com/gin-gonic/gin v1.10.0",
		"## explicit; go 1.21",
		"github.com/gin-gonic/gin",
		"github.com/gin-gonic/gin/render",
		"# example.com/replaced v1.0.0 => example.com/fork v1.4.2",
		"## explicit; go 1.21",
		"example.com/replaced",
		"# example.com/localver v1.2.3 => ../localver",
		"## explicit; go 1.21",
		"example.com/localver",
		"# example.com/unversioned v0.0.0-00010101000000-000000000000 => ../unversioned",
		"## explicit; go 1.21",
		"example.com/unversioned",
		"# example.com/bare => ../bare",
		"## explicit; go 1.21",
		"#malformed-comment",
		"example.com/bare",
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(modulesTxt, []byte(content), 0o644))

	vm, err := parseVendorModules(modulesTxt)
	require.NoError(t, err)

	assert.Contains(t, vm, "github.com/gin-gonic/gin")
	assert.Contains(t, vm, "github.com/gin-gonic/gin/render")
	assert.Contains(t, vm, "example.com/bare")
	assert.NotContains(t, vm, "github.com/gin-gonic/gin/binding")

	assert.Equal(t, "v1.10.0", vm["github.com/gin-gonic/gin"])
	assert.Equal(t, "v1.10.0", vm["github.com/gin-gonic/gin/render"])
	assert.Equal(t, "v1.4.2", vm["example.com/replaced"])
	assert.Equal(t, "v1.2.3", vm["example.com/localver"])
	assert.Empty(t, vm["example.com/unversioned"])
	assert.Empty(t, vm["example.com/bare"])
}

func TestDetectVendorState(t *testing.T) {
	t.Setenv("GOWORK", "off")

	const (
		depModules  = "# example.com/dep v1.0.0\nexample.com/dep\n"
		instModules = "# example.com/inst v1.0.0\nexample.com/inst\n"
	)

	tests := []struct {
		name         string
		noVendor     bool
		modulesTxt   string
		toolFile     string
		toolImports  string
		setup        func(t *testing.T, root string)
		wantActive   bool
		wantVendored bool
	}{
		{
			name:     "no vendor dir",
			noVendor: true,
		},
		{
			name:       "vendor without tool file forces module mode",
			modulesTxt: depModules,
			wantActive: true,
		},
		{
			name:        "tool file with unvendored import forces module mode",
			modulesTxt:  depModules,
			toolFile:    toolFileCanonical,
			toolImports: `"example.com/missing"`,
			wantActive:  true,
		},
		{
			name:         "canonical tool file with vendored imports preserves vendor mode",
			modulesTxt:   instModules,
			toolFile:     toolFileCanonical,
			toolImports:  `"example.com/inst"`,
			wantActive:   true,
			wantVendored: true,
		},
		{
			name:         "alias tool file with vendored imports preserves vendor mode",
			modulesTxt:   instModules,
			toolFile:     toolFileAlias,
			toolImports:  `"example.com/inst"`,
			wantActive:   true,
			wantVendored: true,
		},
		{
			name:        "local same-module package is allowed in vendor mode",
			modulesTxt:  instModules,
			toolFile:    toolFileCanonical,
			toolImports: "\"example.com/inst\"\n\t_ \"example.com/app/internal/hooks\"",
			setup: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "hooks"), 0o755))
			},
			wantActive:   true,
			wantVendored: true,
		},
		{
			name:        "missing same-module package forces module mode",
			modulesTxt:  instModules,
			toolFile:    toolFileCanonical,
			toolImports: `"example.com/app/doesnotexist"`,
			wantActive:  true,
		},
		{
			name:       "unparsable tool file forces module mode",
			modulesTxt: instModules,
			setup: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.WriteFile(
					filepath.Join(root, toolFileCanonical),
					[]byte("not valid go syntax"),
					0o644,
				))
			},
			wantActive: true,
		},
		{
			name:     "unreadable modules.txt forces module mode",
			noVendor: true,
			setup: func(t *testing.T, root string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor", "modules.txt"), 0o755))
			},
			wantActive: true,
		},
		{
			name:        "nested submodule under main module prefix forces module mode when unvendored",
			modulesTxt:  instModules,
			toolFile:    toolFileCanonical,
			toolImports: `"example.com/app/hooks"`,
			setup: func(t *testing.T, root string) {
				t.Helper()
				sub := filepath.Join(root, "hooks")
				require.NoError(t, os.MkdirAll(sub, 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(sub, "go.mod"),
					[]byte("module example.com/app/hooks\n\ngo 1.25.0\n"),
					0o644,
				))
			},
			wantActive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(
				filepath.Join(root, "go.mod"),
				[]byte("module example.com/app\n\ngo 1.25.0\n"),
				0o644,
			))
			if !tt.noVendor {
				require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(root, "vendor", "modules.txt"),
					[]byte(tt.modulesTxt),
					0o644,
				))
			}
			if tt.setup != nil {
				tt.setup(t, root)
			}
			if tt.toolFile != "" {
				src := "//go:build tools\n\npackage tools\n\nimport (\n\t_ " + tt.toolImports + "\n)\n"
				require.NoError(t, os.WriteFile(filepath.Join(root, tt.toolFile), []byte(src), 0o644))
			}

			vs := detectVendorState(t.Context(), root)
			assert.Equal(t, tt.wantActive, vs.active())
			assert.Equal(t, tt.wantVendored, vs.vendored)
			assert.Equal(t, tt.wantActive && !tt.wantVendored, vs.needsModMod())
		})
	}
}

func TestSetupLockedPreservesModVendorWhenPinnedAndVendored(t *testing.T) {
	t.Setenv("GOWORK", "off")

	root := t.TempDir()
	app := filepath.Join(root, "app")
	require.NoError(t, os.MkdirAll(filepath.Join(app, "vendor"), 0o755))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(app, "go.mod"), []byte("module example.com/app\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(app, "vendor", "modules.txt"),
		[]byte("# example.com/inst v1.0.0\nexample.com/inst\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(app, toolFileCanonical),
		[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/inst\"\n"),
		0o644,
	))
	t.Chdir(root)
	t.Setenv(util.EnvOtelcWorkDir, root)
	t.Setenv("GOFLAGS", "-mod=vendor")

	cmd := &cli.Command{Name: "go", SkipFlagParsing: true, Action: setupLocked}
	err := cmd.Run(t.Context(), []string{"go", "build", "-C", "app", "./missing"})
	require.Error(t, err)

	assert.Equal(t, "-mod=vendor", os.Getenv("GOFLAGS"))
}

func TestSetupLockedPreservesModVendorForVendoredWorkspace(t *testing.T) {
	ws := t.TempDir()
	app := filepath.Join(ws, "app")
	require.NoError(t, os.MkdirAll(app, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "go.work"), []byte("go 1.25.0\n\nuse ./app\n"), 0o644))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(app, "go.mod"), []byte("module example.com/app\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(ws, "vendor", "modules.txt"),
		[]byte("## workspace\n# example.com/inst v1.0.0\nexample.com/inst\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(app, toolFileCanonical),
		[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/inst\"\n"),
		0o644,
	))
	t.Chdir(ws)
	t.Setenv(util.EnvOtelcWorkDir, ws)
	t.Setenv("GOWORK", "")
	t.Setenv("GOFLAGS", "-mod=vendor")

	cmd := &cli.Command{Name: "go", SkipFlagParsing: true, Action: setupLocked}
	err := cmd.Run(t.Context(), []string{"go", "build", "./app/missing"})
	require.Error(t, err)

	assert.Equal(t, "-mod=vendor", os.Getenv("GOFLAGS"))
}

func TestDetectVendorState_Workspace(t *testing.T) {
	t.Setenv("GOWORK", "")

	const instModules = "## workspace\n# example.com/inst v1.0.0\nexample.com/inst\n"

	writeWorkspace := func(t *testing.T, withRootVendor bool, modulesTxt string) (string, string, string) {
		t.Helper()
		ws := t.TempDir()
		modA := filepath.Join(ws, "modA")
		modB := filepath.Join(ws, "modB")
		require.NoError(t, os.MkdirAll(modA, 0o755))
		require.NoError(t, os.MkdirAll(modB, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(ws, "go.work"),
			[]byte("go 1.25.0\n\nuse (\n\t./modA\n\t./modB\n)\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(modA, "go.mod"),
			[]byte("module example.com/modA\n\ngo 1.25.0\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(modB, "go.mod"),
			[]byte("module example.com/modB\n\ngo 1.25.0\n"),
			0o644,
		))
		if withRootVendor {
			require.NoError(t, os.MkdirAll(filepath.Join(ws, "vendor"), 0o755))
			require.NoError(t, os.WriteFile(
				filepath.Join(ws, "vendor", "modules.txt"),
				[]byte(modulesTxt),
				0o644,
			))
		}
		return ws, modA, modB
	}

	t.Run("workspace without vendor dir", func(t *testing.T) {
		ws, modA, _ := writeWorkspace(t, false, "")
		vs := detectVendorState(t.Context(), ws)
		assert.False(t, vs.active())
		assert.Empty(t, vs.overrideModFlag())
		assert.False(t, detectVendorState(t.Context(), modA).active())
	})

	t.Run("workspace vendor without tool files forces readonly", func(t *testing.T) {
		ws, modA, _ := writeWorkspace(t, true, instModules)
		for _, dir := range []string{ws, modA} {
			vs := detectVendorState(t.Context(), dir)
			assert.True(t, vs.active())
			assert.True(t, vs.workspace)
			assert.False(t, vs.vendored)
			assert.False(t, vs.needsModMod())
			assert.Equal(t, modReadonly, vs.overrideModFlag())
		}
	})

	t.Run("single tool file with vendored and sibling workspace imports preserves vendor mode", func(t *testing.T) {
		ws, modA, modB := writeWorkspace(t, true, instModules)
		require.NoError(t, os.MkdirAll(filepath.Join(modB, "hooks"), 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(modA, toolFileCanonical),
			[]byte(
				"//go:build tools\n\npackage tools\n\nimport (\n\t_ \"example.com/inst\"\n\t_ \"example.com/modB/hooks\"\n)\n",
			),
			0o644,
		))

		vs := detectVendorState(t.Context(), ws)
		assert.True(t, vs.active())
		assert.True(t, vs.workspace)
		assert.True(t, vs.vendored)
		assert.Empty(t, vs.overrideModFlag())
		assert.Equal(t, filepath.Join(ws, "vendor"), vs.vendorDir())
		assert.Equal(
			t,
			"v1.0.0",
			vs.packageVersion("example.com/inst", filepath.Join(ws, "vendor", "example.com", "inst", "inst.go")),
		)
	})

	t.Run("multiple tool files all vendored preserves vendor mode", func(t *testing.T) {
		_, modA, modB := writeWorkspace(t, true, instModules+"# example.com/inst2 v2.0.0\nexample.com/inst2\n")
		require.NoError(t, os.WriteFile(
			filepath.Join(modA, toolFileCanonical),
			[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/inst\"\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(modB, toolFileAlias),
			[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/inst2\"\n"),
			0o644,
		))

		vs := detectVendorState(t.Context(), modA)
		assert.True(t, vs.active())
		assert.True(t, vs.vendored)
		assert.Empty(t, vs.overrideModFlag())
	})

	t.Run("unvendored import in second workspace module forces readonly", func(t *testing.T) {
		ws, modA, modB := writeWorkspace(t, true, instModules)
		require.NoError(t, os.WriteFile(
			filepath.Join(modA, toolFileCanonical),
			[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/inst\"\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(modB, toolFileCanonical),
			[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/missing\"\n"),
			0o644,
		))

		vs := detectVendorState(t.Context(), ws)
		assert.True(t, vs.active())
		assert.True(t, vs.workspace)
		assert.False(t, vs.vendored)
		assert.Equal(t, modReadonly, vs.overrideModFlag())
	})

	t.Run("nested non-workspace submodule forces readonly when unvendored", func(t *testing.T) {
		ws, modA, _ := writeWorkspace(t, true, instModules)
		sub := filepath.Join(modA, "nested")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(sub, "go.mod"),
			[]byte("module example.com/modA/nested\n\ngo 1.25.0\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(modA, toolFileCanonical),
			[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/modA/nested\"\n"),
			0o644,
		))

		vs := detectVendorState(t.Context(), ws)
		assert.True(t, vs.active())
		assert.False(t, vs.vendored)
		assert.Equal(t, modReadonly, vs.overrideModFlag())
	})

	t.Run("sibling use module outside workspace dir", func(t *testing.T) {
		base := t.TempDir()
		ws := filepath.Join(base, "ws")
		sibling := filepath.Join(base, "sibling")
		require.NoError(t, os.MkdirAll(filepath.Join(ws, "vendor"), 0o755))
		require.NoError(t, os.MkdirAll(sibling, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(ws, "go.work"),
			[]byte("go 1.25.0\n\nuse ../sibling\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(sibling, "go.mod"),
			[]byte("module example.com/sibling\n\ngo 1.25.0\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(sibling, toolFileCanonical),
			[]byte("//go:build tools\n\npackage tools\n\nimport _ \"example.com/inst\"\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(ws, "vendor", "modules.txt"),
			[]byte(instModules),
			0o644,
		))

		vs := detectVendorState(t.Context(), ws)
		assert.True(t, vs.active())
		assert.True(t, vs.vendored)
		ctx := contextWithVendorState(t.Context(), vs)
		_, ok := vendorStateFromContext(ctx, sibling)
		assert.True(t, ok, "context cache should hit for workspace use module outside workspace root")
	})
}

func TestWrapWorkspaceVendorFallbackError(t *testing.T) {
	wsVS := vendorState{workspace: true, vendorRoot: "/ws", vendored: false}
	lookupErr := assert.AnError
	assert.Equal(t, lookupErr, wrapWorkspaceVendorFallbackError(lookupErr, wsVS))

	goproxyErr := wrapWorkspaceVendorFallbackError(
		os.ErrNotExist,
		vendorState{workspace: true, vendorRoot: "/ws", vendored: true},
	)
	require.ErrorIs(t, goproxyErr, os.ErrNotExist)
	assert.NotContains(t, goproxyErr.Error(), "go work vendor")

	wrapped := wrapWorkspaceVendorFallbackError(
		ex.New("module lookup disabled by GOPROXY=off"),
		wsVS,
	)
	require.ErrorContains(t, wrapped, "otelc pin")
	require.ErrorContains(t, wrapped, "go work vendor")
	require.ErrorContains(t, wrapped, "GOWORK=off")
}

func TestGetVendorState_ReusesContext(t *testing.T) {
	root := t.TempDir()
	canonRoot := resolveAbsDir(root)
	modA := filepath.Join(root, "modA")
	canonModA := resolveAbsDir(modA)
	vendorFile := filepath.Join(root, "vendor", "example.com", "pkg", "pkg.go")
	cached := vendorState{
		vendored:   true,
		workspace:  true,
		queryDir:   canonRoot,
		vendorRoot: canonRoot,
		moduleDirs: []string{canonModA},
		modules:    vendorModules{"example.com/pkg": "v1.2.3"},
	}
	ctx := contextWithVendorState(t.Context(), cached)

	got := getVendorState(ctx, root)
	assert.True(t, got.vendored)
	assert.Equal(t, "v1.2.3", got.packageVersion("example.com/pkg", vendorFile))
	assert.True(t, vendoringActive(ctx, root))

	_, okModA := vendorStateFromContext(ctx, modA)
	assert.True(t, okModA)

	// A different directory does not return the cached state for root.
	other := t.TempDir()
	_, ok := vendorStateFromContext(ctx, other)
	assert.False(t, ok)
}

func TestIsUnderDir_Symlink(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	linkRoot := filepath.Join(base, "link")
	vendorPkg := filepath.Join(realRoot, "vendor", "example.com", "dep")
	require.NoError(t, os.MkdirAll(vendorPkg, 0o755))
	src := filepath.Join(vendorPkg, "dep.go")
	require.NoError(t, os.WriteFile(src, []byte("package dep\n"), 0o644))

	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	assert.True(t, isUnderDir(
		filepath.Join(linkRoot, "vendor", "example.com", "dep", "dep.go"),
		filepath.Join(realRoot, "vendor"),
	))
	assert.True(t, isUnderDir(
		src,
		filepath.Join(linkRoot, "vendor"),
	))
	assert.False(t, isUnderDir("", realRoot))
}

func TestVendorHelpers_EmptyModuleAndMissingManifest(t *testing.T) {
	assert.Empty(t, (vendorState{}).vendorDir())

	_, err := parseVendorModules(filepath.Join(t.TempDir(), "missing.txt"))
	require.Error(t, err)

	assert.False(t, isLocalModulePackage("", "", "example.com/pkg"))
	assert.True(t, isLocalModulePackage("", "example.com/app", "example.com/app"))
}

func chdirRemovedDir(t *testing.T) {
	t.Helper()
	dead := filepath.Join(t.TempDir(), "dead")
	require.NoError(t, os.MkdirAll(dead, 0o755))
	t.Chdir(dead)
	if err := os.RemoveAll(dead); err != nil {
		t.Skipf("cannot remove active working directory: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd still resolves removed working directory on this OS")
	}
}

func TestVendorPathResolution_MissingWorkingDir(t *testing.T) {
	chdirRemovedDir(t)

	assert.Empty(t, resolveAbsDir(""))
	assert.Equal(t, filepath.Clean("rel/path"), resolveCanonicalPath("rel/path"))
	ctx := contextWithVendorState(t.Context(), vendorState{queryDir: "/tmp/other"})
	_, ok := vendorStateFromContext(ctx, "")
	assert.False(t, ok)
}
