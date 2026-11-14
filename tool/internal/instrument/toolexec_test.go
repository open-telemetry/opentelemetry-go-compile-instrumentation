// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestStripCompleteFlag(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "empty args",
			args:     []string{},
			expected: []string{},
		},
		{
			name:     "no complete flag",
			args:     []string{"-o", "output.a", "-p", "main", "file.go"},
			expected: []string{"-o", "output.a", "-p", "main", "file.go"},
		},
		{
			name:     "complete flag at beginning",
			args:     []string{"-complete", "-o", "output.a", "-p", "main"},
			expected: []string{"-o", "output.a", "-p", "main"},
		},
		{
			name:     "complete flag in middle",
			args:     []string{"-o", "output.a", "-complete", "-p", "main"},
			expected: []string{"-o", "output.a", "-p", "main"},
		},
		{
			name:     "complete flag at end",
			args:     []string{"-o", "output.a", "-p", "main", "-complete"},
			expected: []string{"-o", "output.a", "-p", "main"},
		},
		{
			name:     "only complete flag",
			args:     []string{"-complete"},
			expected: []string{},
		},
		{
			name:     "complete as value not flag",
			args:     []string{"-mode", "-complete", "-o", "output.a"},
			expected: []string{"-mode", "-o", "output.a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of original args to verify they are not mutated
			origArgs := make([]string, len(tt.args))
			copy(origArgs, tt.args)

			result := stripCompleteFlag(tt.args)
			assert.Equal(t, tt.expected, result)
			assert.Equal(t, origArgs, tt.args, "original slice should not be mutated")
		})
	}
}

func TestInterceptVetUsesPreservedCgoSource(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "vet.cfg")
	cgoPath := filepath.Join(tempDir, "source.cgo1.go")
	vetPath := cgoVetSourcePath(cgoPath)
	require.NoError(t, os.WriteFile(vetPath, []byte("package example"), 0o600))
	inputConfig := map[string]any{
		"GoFiles":    []string{"source.go", cgoPath},
		"ImportPath": "example.com/cgo",
	}
	data, err := json.Marshal(inputConfig)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, data, 0o600))

	args := []string{vetToolName, "-tests", configPath}
	got, err := interceptVet(t.Context(), args)
	require.NoError(t, err)
	assert.Equal(t, args, got)

	data, err = os.ReadFile(configPath)
	require.NoError(t, err)
	var config map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &config))

	var goFiles []string
	require.NoError(t, json.Unmarshal(config["GoFiles"], &goFiles))
	assert.Equal(t, []string{"source.go", vetPath}, goFiles)
	assert.JSONEq(t, `"example.com/cgo"`, string(config["ImportPath"]))
}

func TestInterceptVetIgnoresCgoSourceWithoutPreservedCopy(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "vet.cfg")
	original := []byte(`{"GoFiles":["source.go","source.cgo1.go"]}`)
	require.NoError(t, os.WriteFile(configPath, original, 0o600))

	_, err := interceptVet(t.Context(), []string{vetToolName, configPath})
	require.NoError(t, err)

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, original, data)
}

func TestInterceptVetLogsUnexpectedArguments(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := util.ContextWithLogger(t.Context(), logger)
	args := []string{vetToolName, "-tests"}

	got, err := interceptVet(ctx, args)

	require.NoError(t, err)
	assert.Equal(t, args, got)
	assert.Contains(t, logs.String(), "vet invocation missing expected vet.cfg argument")
}

// TestInterceptVetWithoutGoFiles covers a vet config that carries no GoFiles
// key. There is no source list to rewrite, so the command passes through; the
// previous code indexed the missing key and reported the resulting nil as
// malformed JSON, failing the build.
func TestInterceptVetWithoutGoFiles(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))

	t.Run("absent key passes through", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "vet.cfg")
		require.NoError(t, os.WriteFile(cfg,
			[]byte(`{"Compiler":"gc","ImportPath":"example.com/p"}`), 0o644))
		args := []string{vetToolName, cfg}

		got, err := interceptVet(ctx, args)

		require.NoError(t, err)
		assert.Equal(t, args, got)
	})

	t.Run("malformed value still errors", func(t *testing.T) {
		cfg := filepath.Join(t.TempDir(), "vet.cfg")
		require.NoError(t, os.WriteFile(cfg,
			[]byte(`{"GoFiles":"not-a-list"}`), 0o644))

		_, err := interceptVet(ctx, []string{vetToolName, cfg})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing GoFiles from vet config")
	})
}

func TestUpdateImportConfig(t *testing.T) {
	t.Run("no importcfg path", func(t *testing.T) {
		ip := &instrumentPhase{
			importConfigPath: "",
		}
		err := ip.updateImportConfig(map[string]string{"fmt": "fmt"})
		require.NoError(t, err)
	})

	t.Run("empty new imports", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644)
		require.NoError(t, err)

		ip := &instrumentPhase{
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(map[string]string{})
		require.NoError(t, err)

		// File should not be modified
		content, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "packagefile fmt=/path/to/fmt.a\n", string(content))
	})

	t.Run("unsafe import is skipped", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644)
		require.NoError(t, err)

		ip := &instrumentPhase{
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(map[string]string{"unsafe": "unsafe"})
		require.NoError(t, err)

		// File should not be modified since unsafe is skipped
		content, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "packagefile fmt=/path/to/fmt.a\n", string(content))
	})

	t.Run("cgo C pseudo-package is skipped", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644)
		require.NoError(t, err)

		ip := &instrumentPhase{
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(map[string]string{"C": "C"})
		require.NoError(t, err)

		// File should not be modified since C is the cgo pseudo-package
		content, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "packagefile fmt=/path/to/fmt.a\n", string(content))
	})

	t.Run("import already exists", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte("packagefile fmt=/path/to/fmt.a\n"), 0o644)
		require.NoError(t, err)

		ip := &instrumentPhase{
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"fmt": "/path/to/fmt.a"},
			},
		}
		err = ip.updateImportConfig(map[string]string{"fmt": "fmt"})
		require.NoError(t, err)

		// File should not be modified since fmt already exists
		content, err := os.ReadFile(cfgPath)
		require.NoError(t, err)
		assert.Equal(t, "packagefile fmt=/path/to/fmt.a\n", string(content))
	})

	t.Run("nil PackageFile map", func(t *testing.T) {
		tempDir := t.TempDir()
		cfgPath := filepath.Join(tempDir, "importcfg")
		err := os.WriteFile(cfgPath, []byte(""), 0o644)
		require.NoError(t, err)

		ip := &instrumentPhase{
			logger:           slog.Default(),
			importConfigPath: cfgPath,
			importConfig: imports.ImportConfig{
				PackageFile: nil, // Intentionally nil
			},
		}

		// Should not panic, even though we're trying to add imports
		err = ip.updateImportConfig(map[string]string{"unsafe": "unsafe"})
		require.NoError(t, err)
	})
}

// goCompileToolPath returns the path to the toolchain's compile binary, which
// answers the `-V=full` probe the same way a real toolexec sees it.
func goCompileToolPath(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOTOOLDIR").Output()
	require.NoError(t, err)
	name := "compile"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(strings.TrimSpace(string(out)), name)
}

func TestKeepForDebugCopyError(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)

	ip := &instrumentPhase{
		logger:      slog.Default(),
		compileArgs: []string{"-p", "example.com/mod/pkg"},
	}
	// The source file does not exist, so CopyFile fails. The failure is only
	// logged as a warning because this is best-effort debugging output.
	ip.keepForDebug(filepath.Join(workDir, "missing.go"))
}

func TestInterceptCompileImportCfgParseError(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	args := []string{
		"compile",
		"-o", filepath.Join(t.TempDir(), "out.a"),
		"-importcfg", filepath.Join(t.TempDir(), "missing.importcfg"),
		"-p", "main",
	}
	_, err := interceptCompile(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "importcfg")
}

func TestInterceptCompileImportNamesLoadError(t *testing.T) {
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(`[]`), 0o644))
	require.NoError(t, os.WriteFile(util.GetImportNamesFile(), []byte("not json"), 0o644))

	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	args := []string{
		"compile",
		"-o", filepath.Join(t.TempDir(), "out.a"),
		"-p", "main",
	}
	_, err := interceptCompile(ctx, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestUpdateImportConfigMissingImportErrors(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte("packagefile context=/unused/context.a\n"), 0o644))

	ip := &instrumentPhase{
		logger:           slog.Default(),
		importConfigPath: cfgPath,
		compileArgs:      []string{"compile", "-p", "example.com/app"},
		importConfig: imports.ImportConfig{
			PackageFile: map[string]string{"context": "/unused/context.a"},
		},
	}
	err := ip.updateImportConfig(map[string]string{"fmt": "fmt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"fmt"`)
	assert.Contains(t, err.Error(), "example.com/app")
	assert.Contains(t, err.Error(), "-overlay")
}

func TestUpdateImportConfigPresentIsNoop(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "importcfg")
	const content = "packagefile fmt=/path/to/fmt.a\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(content), 0o644))

	ip := &instrumentPhase{
		logger:           slog.Default(),
		importConfigPath: cfgPath,
		importConfig:     imports.ImportConfig{PackageFile: map[string]string{"fmt": "/path/to/fmt.a"}},
	}
	require.NoError(t, ip.updateImportConfig(map[string]string{"fmt": "fmt", "u": "unsafe", "C": "C"}))

	got, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
}

func writeAddedImports(t *testing.T, packages map[string]string) {
	t.Helper()
	data, err := json.Marshal(packages)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(util.GetBuildTempDir(), "added_imports.1.json"), data, 0o644))
}

func TestResolvePackageName(t *testing.T) {
	dir := t.TempDir()
	testMain := filepath.Join(dir, "_testmain.go")
	require.NoError(t, os.WriteFile(testMain,
		[]byte("// Code generated by 'go test'. DO NOT EDIT.\n\npackage main\n"), 0o644))

	t.Run("reads the package clause from the compile sources", func(t *testing.T) {
		ip := &instrumentPhase{compileArgs: []string{
			"compile", "-o", "_pkg_.a", "-p", "main", "-pack", testMain,
		}}
		name, err := ip.resolvePackageName()
		require.NoError(t, err)
		assert.Equal(t, "main", name)
	})

	t.Run("prefers the package clause over the -p import path", func(t *testing.T) {
		ip := &instrumentPhase{compileArgs: []string{
			"compile", "-o", "_pkg_.a", "-p", "example.com/foo", "-pack", testMain,
		}}
		name, err := ip.resolvePackageName()
		require.NoError(t, err)
		assert.Equal(t, "main", name)
	})

	t.Run("skips flag values that are not readable Go files", func(t *testing.T) {
		ip := &instrumentPhase{compileArgs: []string{
			"compile", "-o", filepath.Join(dir, "absent.go"), "-pack", testMain,
		}}
		name, err := ip.resolvePackageName()
		require.NoError(t, err)
		assert.Equal(t, "main", name)
	})

	t.Run("returns empty when the command lists no Go source", func(t *testing.T) {
		ip := &instrumentPhase{compileArgs: []string{"compile", "-o", "_pkg_.a", "-p", "main"}}
		name, err := ip.resolvePackageName()
		require.NoError(t, err)
		assert.Empty(t, name)
	})

	t.Run("propagates a parse failure", func(t *testing.T) {
		broken := filepath.Join(dir, "broken.go")
		require.NoError(t, os.WriteFile(broken, []byte("not a go file"), 0o644))
		ip := &instrumentPhase{compileArgs: []string{"compile", broken}}
		_, err := ip.resolvePackageName()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing package clause")
	})
}

func TestInterceptCompileFileRuleWithoutPackageName(t *testing.T) {
	setup := func(t *testing.T, sources ...string) (string, []string) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

		rulesDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(rulesDir, "added.go"),
			[]byte("//go:build ignore\n\npackage myrules\n\nfunc init() {}\n"), 0o644))

		// Setup hands over an empty PackageName for the generated test main.
		set := rule.NewInstRuleSet("main")
		fileRule := &rule.InstFileRule{File: "added.go", Path: rulesDir, ResolvedPath: rulesDir}
		fileRule.Name = "add_to_main"
		set.AddFileRule(fileRule)
		// Setup records whether the compile belongs to a test binary.
		set.IsTest = util.IsTestBuild(sources)
		data, err := json.Marshal([]*rule.InstRuleSet{set})
		require.NoError(t, err)
		matched := util.GetMatchedRuleFile()
		require.NoError(t, os.MkdirAll(filepath.Dir(matched), 0o755))
		require.NoError(t, os.WriteFile(matched, data, 0o644))

		buildDir := t.TempDir()
		args := make([]string, 0, 7+len(sources))
		args = append(args, "compile", "-o", filepath.Join(buildDir, "_pkg_.a"), "-p", "main", "-buildid", "x")
		for _, name := range sources {
			path := filepath.Join(buildDir, name)
			require.NoError(t, os.WriteFile(path, []byte("package main\n"), 0o644))
			args = append(args, path)
		}
		return buildDir, args
	}

	t.Run("resolves the name from the compile sources", func(t *testing.T) {
		buildDir, args := setup(t, "_testmain.go")
		ctx := util.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))

		newArgs, err := interceptCompile(ctx, args)
		require.NoError(t, err)

		added := filepath.Join(buildDir, "otelc.added.go")
		assert.Contains(t, newArgs, added)
		name, err := ast.ParsePackageName(added)
		require.NoError(t, err)
		assert.Equal(t, "main", name)
	})

	t.Run("errors when there is nothing to resolve from", func(t *testing.T) {
		buildDir, args := setup(t)
		ctx := util.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))

		_, err := interceptCompile(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot resolve the package name")
		assert.NoFileExists(t, filepath.Join(buildDir, "otelc.added.go"))
	})

	t.Run("propagates a parse failure", func(t *testing.T) {
		buildDir, args := setup(t, "_testmain.go")
		broken := filepath.Join(buildDir, "_testmain.go")
		require.NoError(t, os.WriteFile(broken, []byte("not a go file"), 0o644))
		ctx := util.ContextWithLogger(t.Context(), slog.New(slog.DiscardHandler))

		_, err := interceptCompile(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parsing package clause")
		assert.NoFileExists(t, filepath.Join(buildDir, "otelc.added.go"))
	})
}

func TestInterceptToolVersionWriteError(t *testing.T) {
	ctx := util.ContextWithLogger(t.Context(), slog.Default())
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	// Replace os.Stdout with a closed file so the tool version line write fails.
	oldStdout := os.Stdout
	f, err := os.Create(filepath.Join(t.TempDir(), "closed"))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	os.Stdout = f                               //nolint:reassign // interceptToolVersion writes to os.Stdout; replace it with a closed file to force a write error
	t.Cleanup(func() { os.Stdout = oldStdout }) //nolint:reassign // restore the original os.Stdout

	err = interceptToolVersion(ctx, []string{goCompileToolPath(t), "-V=full"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing tool version")
}

func TestToolVersionLine(t *testing.T) {
	marker := "otelc@" + util.Version

	t.Run("release toolchain without rules hash", func(t *testing.T) {
		got := toolVersionLine("compile version go1.26.5", "")
		assert.Equal(t, "compile version go1.26.5 "+marker, got)
	})

	t.Run("release toolchain with rules hash", func(t *testing.T) {
		got := toolVersionLine("compile version go1.26.5", "abcd1234")
		assert.Equal(t, "compile version go1.26.5 "+marker+"/abcd1234", got)
	})

	t.Run("devel toolchain changes the content ID used by Go", func(t *testing.T) {
		line := "compile version devel go1.27-abc123 buildID=x/y/z"
		got := toolVersionLine(line, "abcd1234")
		assert.Equal(t, "compile version devel go1.27-abc123 buildID=x/y/z+"+marker+"+abcd1234", got)
		contentID := got[strings.LastIndex(got, "/")+1:]
		assert.Equal(t, "z+"+marker+"+abcd1234", contentID)
	})
}

func TestMarkedToolVersion(t *testing.T) {
	const raw = "compile version go1.26.5\n"

	t.Run("no rules hash when matched.json is absent", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

		got := markedToolVersion(raw)
		assert.Equal(t, "compile version go1.26.5 otelc@"+util.Version, got)
	})

	t.Run("appends a 16-hex-digit rules hash when matched.json is present", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		require.NoError(t, os.MkdirAll(filepath.Join(workDir, util.BuildTempDir), 0o755))
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(`[{"module_path":"main"}]`), 0o644))

		got := markedToolVersion(raw)
		assert.Regexp(t, `^compile version go1\.26\.5 otelc@\S+/[0-9a-f]{16}$`, got)
	})

	t.Run("build tags change the rules hash", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		t.Setenv("GOFLAGS", "")
		require.NoError(t, os.MkdirAll(filepath.Join(workDir, util.BuildTempDir), 0o755))
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(`[{"module_path":"main"}]`), 0o644))

		t.Setenv(util.EnvOtelcBuildFlags, util.EncodeBuildFlags(nil))
		untagged := markedToolVersion(raw)
		t.Setenv(util.EnvOtelcBuildFlags, util.EncodeBuildFlags([]string{"-tags=enterprise"}))
		tagged := markedToolVersion(raw)
		t.Setenv(util.EnvOtelcBuildFlags, util.EncodeBuildFlags([]string{"-tags=beta"}))
		otherTag := markedToolVersion(raw)

		assert.NotEqual(t, untagged, tagged)
		assert.NotEqual(t, tagged, otherTag)
	})

	t.Run("injected package hash changes the rules hash", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		require.NoError(t, os.MkdirAll(filepath.Join(workDir, util.BuildTempDir), 0o755))
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(`[{"module_path":"main"}]`), 0o644))

		without := markedToolVersion(raw)
		require.NoError(t, os.WriteFile(util.GetInjectedHashFile(), []byte("0011223344556677"), 0o644))
		first := markedToolVersion(raw)
		require.NoError(t, os.WriteFile(util.GetInjectedHashFile(), []byte("8899aabbccddeeff"), 0o644))
		second := markedToolVersion(raw)
		require.NoError(t, os.WriteFile(util.GetInjectedHashFile(), nil, 0o644))

		assert.NotEqual(t, without, first)
		assert.NotEqual(t, first, second)
		assert.Equal(t, without, markedToolVersion(raw), "an empty hash file must not change the rules hash")
	})
}

func TestEnableNestedToolexec(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	// Built rather than hardcoded: the quoting depends on whether the test
	// binary's own path contains a space, which varies by machine.
	wantToken, err := nestedToolexecGoflagsToken(exe)
	require.NoError(t, err)

	t.Run("appends to existing GOFLAGS and sets the nested marker", func(t *testing.T) {
		t.Setenv("GOFLAGS", "-mod=mod")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		goflags := os.Getenv("GOFLAGS")
		assert.Contains(t, goflags, "-mod=mod", "existing flags are preserved")
		assert.Contains(t, goflags, wantToken, "otelc is added as the toolexec")
		assert.Equal(t, "1", os.Getenv(util.EnvOtelcNestedToolexec))
	})

	t.Run("strips only unpatterned gcflags from GOFLAGS", func(t *testing.T) {
		t.Setenv("GOFLAGS", "-mod=mod -gcflags=-l '-gcflags=all=-N -l'")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		tokens := util.SplitGoflags(os.Getenv("GOFLAGS"))
		assert.Contains(t, tokens, "-mod=mod")
		assert.Contains(t, tokens, "'-gcflags=all=-N -l'")
		assert.NotContains(t, tokens, "-gcflags=-l")
		assert.Contains(t, tokens, wantToken)
	})

	t.Run("handles empty GOFLAGS without leading whitespace", func(t *testing.T) {
		t.Setenv("GOFLAGS", "")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		assert.Equal(t, wantToken, os.Getenv("GOFLAGS"))
	})

	t.Run("GOFLAGS survives both splits cmd/go performs", func(t *testing.T) {
		t.Setenv("GOFLAGS", "")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		assert.Equal(t, []string{exe, "toolexec"}, parseToolexecFromGoflags(t, os.Getenv("GOFLAGS")))
	})

	t.Run("a spaced executable path survives both splits", func(t *testing.T) {
		// EnableNestedToolexec resolves its own path, so the spaced case is
		// exercised by calling the extracted helper directly and parsing the
		// result back the way cmd/go would.
		spaced := filepath.Join(string(filepath.Separator)+"opt", "my tools", "otelc")
		token, tokenErr := nestedToolexecGoflagsToken(spaced)
		require.NoError(t, tokenErr)

		assert.Equal(t, []string{spaced, "toolexec"}, parseToolexecFromGoflags(t, token))
	})

	t.Run("a spaced path carrying either quote is rejected", func(t *testing.T) {
		// Whitespace forces the inner quoting to pick one quote character, and
		// a quote already in the path supplies the other. Either kind gets us
		// there, so the rejection is not specific to single quotes.
		for _, path := range []string{`/home/it's me/otelc`, `/home/my "tools"/otelc`} {
			_, tokenErr := nestedToolexecGoflagsToken(path)
			require.Error(t, tokenErr, "%q should not be representable in GOFLAGS", path)
			assert.Contains(t, tokenErr.Error(), "cannot be passed to nested go commands through GOFLAGS")
		}
	})

	t.Run("a spaced path already carrying both quotes fails building the inner flag", func(t *testing.T) {
		// Distinct from the case above: here execPath itself has both quote
		// characters plus whitespace, so BuildToolexecFlag's own quoting of
		// execPath fails before the inner/outer nesting is even attempted.
		// nestedToolexecGoflagsToken adds no message of its own for this one,
		// BuildToolexecFlag's error already names execPath, so this asserts on
		// its wording rather than anything added downstream.
		_, tokenErr := nestedToolexecGoflagsToken(`/home/it's "here"/otelc`)
		require.Error(t, tokenErr)
		assert.Contains(t, tokenErr.Error(), "quoting otelc executable path for -toolexec")
	})

	t.Run("a path with an interior quote but no space survives both splits", func(t *testing.T) {
		// quoted.Split reads an interior quote literally, so this path needs no
		// quoting and must keep working rather than being rejected.
		quoted := "/opt/it's/otelc"
		token, tokenErr := nestedToolexecGoflagsToken(quoted)
		require.NoError(t, tokenErr)

		assert.Equal(t, []string{quoted, "toolexec"}, parseToolexecFromGoflags(t, token))
	})

	t.Run("returns error when executable path cannot be quoted in GOFLAGS", func(t *testing.T) {
		originalExe := executablePath
		t.Cleanup(func() { executablePath = originalExe })
		executablePath = func() (string, error) {
			return `/home/it's "here"/otelc`, nil
		}

		enableErr := EnableNestedToolexec()
		require.Error(t, enableErr)
	})
}

// splitQuoted mirrors cmd/internal/quoted.Split, which is what cmd/go uses for
// both GOFLAGS and the -toolexec value: whitespace-separated, with `'` or `"`
// grouping, and no unescaping inside a quoted run. Reimplemented here rather
// than reusing the production helpers so the tests below check the round trip
// against the real format instead of against otelc's own encoder.
func splitQuoted(t *testing.T, s string) []string {
	t.Helper()
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
	var out []string
	for len(s) > 0 {
		for len(s) > 0 && isSpace(s[0]) {
			s = s[1:]
		}
		if len(s) == 0 {
			break
		}
		if q := s[0]; q == '"' || q == '\'' {
			s = s[1:]
			end := strings.IndexByte(s, q)
			require.GreaterOrEqual(t, end, 0, "unterminated %c-quoted run in %q", q, s)
			out = append(out, s[:end])
			s = s[end+1:]
			continue
		}
		end := 0
		for end < len(s) && !isSpace(s[end]) {
			end++
		}
		out = append(out, s[:end])
		s = s[end:]
	}
	return out
}

// parseToolexecFromGoflags walks the same two split layers cmd/go does and
// returns the argv it would end up executing for -toolexec.
func parseToolexecFromGoflags(t *testing.T, goflags string) []string {
	t.Helper()
	flags := splitQuoted(t, goflags)
	require.Len(t, flags, 1, "expected a lone -toolexec flag in %q", goflags)
	value, ok := strings.CutPrefix(flags[0], "-toolexec=")
	require.True(t, ok, "flag %q is not a -toolexec entry", flags[0])
	return splitQuoted(t, value)
}

// versionMarkerPattern documents the exact tool-ID shape the go build cache
// keys on, guarding against accidental format drift.
var versionMarkerPattern = regexp.MustCompile(`otelc@[^\s/]+(/[0-9a-f]{16})?`)

func TestToolVersionLineMatchesCachePattern(t *testing.T) {
	assert.Regexp(t, versionMarkerPattern, toolVersionLine("compile version go1.26.5", ""))
	assert.Regexp(t, versionMarkerPattern, toolVersionLine("compile version go1.26.5", "0123456789abcdef"))
}

func TestUserBuildTags(t *testing.T) {
	tests := []struct {
		name       string
		goflags    []string
		buildFlags []string
		expected   []string
	}{
		{name: "no tags", goflags: nil, buildFlags: []string{"-race"}, expected: nil},
		{name: "equals form", buildFlags: []string{"-tags=enterprise,beta"}, expected: []string{"enterprise", "beta"}},
		{name: "separate value", buildFlags: []string{"-tags", "enterprise"}, expected: []string{"enterprise"}},
		{name: "double dash", buildFlags: []string{"--tags=enterprise"}, expected: []string{"enterprise"}},
		{name: "space separated list", buildFlags: []string{"-tags", "a b"}, expected: []string{"a", "b"}},
		{name: "last flag wins", buildFlags: []string{"-tags=a", "-tags=b"}, expected: []string{"b"}},
		{name: "from GOFLAGS", goflags: []string{"-mod=mod", "-tags=enterprise"}, expected: []string{"enterprise"}},
		{
			name:       "command line overrides GOFLAGS",
			goflags:    []string{"-tags=fromenv"},
			buildFlags: []string{"-tags=fromcli"},
			expected:   []string{"fromcli"},
		},
		{name: "quoted GOFLAGS value", goflags: []string{`-tags="a,b"`}, expected: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, util.UserBuildTags(tt.goflags, tt.buildFlags))
		})
	}
}

func TestToolexecEmptyArgs(t *testing.T) {
	err := Toolexec(t.Context(), []string{}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tool command specified")
}

// callRuleWithImports returns a call rule whose JSON unmarshals cleanly; the
// selector must parse, so it names a real package function shape.
func callRuleWithImports(imports map[string]string) *rule.InstCallRule {
	return &rule.InstCallRule{
		InstBaseRule: rule.InstBaseRule{
			Name: "call-rule", Imports: imports,
		},
		FunctionCall: "example.com/b.New",
	}
}

// writeMatchedRules stores matched.json the way setup does, so the link phase
// can read the rule-added imports back.
func writeMatchedRules(t *testing.T, sets []*rule.InstRuleSet) {
	t.Helper()
	content, err := json.Marshal(sets)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), content, 0o600))
}

// linkArgs returns a minimal link command with importcfg at cfgPath.
func linkArgs(cfgPath string) []string {
	return []string{"link", "-o", "out.test", "-buildid=test", "-importcfg", cfgPath}
}

// chainLinkPackages returns stdlib packages for the depth-bound test below,
// ordered so that no one of them imports a later one, verified against go's
// own dependency graph: resolving any one of them never drags a later chain
// element into the importcfg, so each recovery pass adds exactly one hop.
func chainLinkPackages() []string {
	return []string{
		"cmp", "container/list", "unicode", "image/color", "unicode/utf16",
		"container/ring", "unicode/utf8", "math/bits", "image/color/palette",
		"math", "slices", "errors", "path", "strconv", "sort", "container/heap",
		"hash/maphash", "hash/adler32", "hash/crc32", "hash/crc64", "hash/fnv",
		"encoding/ascii85", "strings", "encoding/base32", "encoding/base64",
		"encoding/hex", "text/scanner", "net/url", "crypto/rc4", "crypto/md5",
		"crypto/des", "crypto/aes", "crypto/sha512", "crypto/sha256",
	}
}

// chainLinkSets returns the rule sets that chain the first hops packages
// into a d-hop chain: the set of package i declares an import of package
// i+1, so a recovery pass resolves package i+1 only after package i joined
// the importcfg, one hop per pass.
func chainLinkSets(hops int) []*rule.InstRuleSet {
	packages := chainLinkPackages()
	sets := make([]*rule.InstRuleSet, hops)
	for i := range sets {
		sets[i] = &rule.InstRuleSet{
			PackageName: "dep" + strconv.Itoa(i),
			ModulePath:  packages[i],
			FuncRules: map[string][]*rule.InstFuncRule{
				"dep.go": {{InstBaseRule: rule.InstBaseRule{
					Name:   "chain-rule",
					Target: rule.NewTarget(packages[i]),
					Imports: map[string]string{
						"next": packages[i+1],
					},
				}}},
			},
		}
	}
	return sets
}

// linkToolPath returns a path that util.IsLinkCommandWithArgs accepts as the
// link tool.
func linkToolPath() string {
	name := "link"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(string(filepath.Separator)+"go", "pkg", "tool", name)
}

func TestUpdateImportConfigTestMainError(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte("packagefile testing=/t.a\n"), 0o644))

	ip := &instrumentPhase{
		logger:           slog.Default(),
		importConfigPath: cfgPath,
		compileArgs:      []string{"compile", "-p", "main", "/work/b001/_testmain.go"},
		importConfig:     imports.ImportConfig{PackageFile: map[string]string{"testing": "/t.a"}},
	}
	err := ip.updateImportConfig(map[string]string{"extra": "example.com/extra"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"example.com/extra"`)
	assert.Contains(t, err.Error(), "testmain")
	assert.NotContains(t, err.Error(), "-overlay", "no overlay can add imports to the generated testmain")
}

func TestUpdateImportConfigAcceptsImportMap(t *testing.T) {
	ip := &instrumentPhase{
		logger:           slog.Default(),
		importConfigPath: filepath.Join(t.TempDir(), "importcfg"),
		importConfig: imports.ImportConfig{
			ImportMap:   map[string]string{"example.com/b": "example.com/b [example.com/a.test]"},
			PackageFile: map[string]string{"example.com/b [example.com/a.test]": "/b.a"},
		},
	}
	require.NoError(t, ip.updateImportConfig(map[string]string{"b": "example.com/b"}))
}
