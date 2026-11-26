// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/internal/buildserver"
	"go.opentelemetry.io/otelc/tool/internal/imports"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

func TestBuildServerRulesDigest(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
	require.NoError(t, os.WriteFile(util.GetRulesDigestFile(), []byte("diskdigest123456"), 0o644))

	srv, err := buildserver.Start(t.Context(), buildserver.Config{ToolID: "memdigest9876543"})
	require.NoError(t, err)
	t.Setenv(util.EnvOtelcBuildServer, srv.Addr())
	t.Setenv(util.EnvOtelcBuildServerAuth, srv.Token())

	assert.Equal(t, "memdigest9876543", RulesDigest())

	// After closing the server, RulesDigest falls back transparently to disk.
	require.NoError(t, srv.Close(t.Context()))
	assert.Equal(t, "diskdigest123456", RulesDigest())
}

func TestBuildServerInterceptCompile(t *testing.T) {
	t.Run("unmatched skips disk reads", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		// Deliberately do NOT create matched.json on disk.

		srv, err := buildserver.Start(t.Context(), buildserver.Config{
			ToolID:   "test",
			RuleSets: []*rule.InstRuleSet{rule.NewInstRuleSet("example.com/other")},
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = srv.Close(context.Background()) })
		t.Setenv(util.EnvOtelcBuildServer, srv.Addr())
		t.Setenv(util.EnvOtelcBuildServerAuth, srv.Token())

		args := []string{"compile", "-o", filepath.Join(t.TempDir(), "out.a"), "-p", "net/http"}
		ctx := util.ContextWithLogger(t.Context(), slog.Default())
		got, err := interceptCompile(ctx, args)
		require.NoError(t, err)
		assert.Equal(t, args, got)
	})

	t.Run("matched uses server rule sets without matched.json", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

		srcDir := t.TempDir()
		srcFile := filepath.Join(srcDir, "app.go")
		require.NoError(t, os.WriteFile(srcFile, []byte("package app\n\nfunc Hello() {}\n"), 0o644))

		rset := rule.NewInstRuleSet("example.com/app")
		rset.SetPackageName("app")
		rset.AddRawRule(srcFile, &rule.InstRawRule{
			InstBaseRule: rule.InstBaseRule{Name: "raw-hello"},
			Func:         "Hello",
			Raw:          "_ = 1",
		})

		srv, err := buildserver.Start(t.Context(), buildserver.Config{
			ToolID:      "test",
			RuleSets:    []*rule.InstRuleSet{rset},
			ImportNames: map[string]string{},
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = srv.Close(context.Background()) })
		t.Setenv(util.EnvOtelcBuildServer, srv.Addr())
		t.Setenv(util.EnvOtelcBuildServerAuth, srv.Token())

		outDir := t.TempDir()
		args := []string{"compile", "-o", filepath.Join(outDir, "_pkg_.a"), "-p", "example.com/app", srcFile}
		ctx := util.ContextWithLogger(t.Context(), slog.Default())
		got, err := interceptCompile(ctx, args)
		require.NoError(t, err)
		assert.NotEqual(t, args, got, "matched package should have rewritten source arg")
	})

	t.Run("falls back to disk when server is closed", func(t *testing.T) {
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)

		srv, err := buildserver.Start(t.Context(), buildserver.Config{ToolID: "closed"})
		require.NoError(t, err)
		t.Setenv(util.EnvOtelcBuildServer, srv.Addr())
		t.Setenv(util.EnvOtelcBuildServerAuth, srv.Token())
		require.NoError(t, srv.Close(t.Context()))

		args := []string{"compile", "-o", filepath.Join(t.TempDir(), "out.a"), "-p", "net/http"}
		ctx := util.ContextWithLogger(t.Context(), slog.Default())
		_, err = interceptCompile(ctx, args)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "otelc setup")
	})
}

func fakeBatchResolver(onCall func()) buildserver.BatchResolverFunc {
	return func(
		_ context.Context,
		importPaths, _ []string,
		_ ...string,
	) (map[string]map[string]string, map[string]error, error) {
		if onCall != nil {
			onCall()
		}
		out := make(map[string]map[string]string, len(importPaths))
		for _, imp := range importPaths {
			out[imp] = map[string]string{imp: "/cache/" + imp + ".a"}
		}
		return out, nil, nil
	}
}

func TestBuildServerUpdateImportConfig(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	rset := rule.NewInstRuleSet("example.com/app")
	rset.AddFuncRule("/src/app.go", &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "hook",
			Imports: map[string]string{"a": "example.com/hook/a", "b": "example.com/hook/b"},
		},
		Func: "F",
	})

	var batchCalls atomic.Int32
	srv, err := buildserver.Start(t.Context(), buildserver.Config{
		ToolID:       "imp",
		RuleSets:     []*rule.InstRuleSet{rset},
		ResolveBatch: fakeBatchResolver(func() { batchCalls.Add(1) }),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	t.Setenv(util.EnvOtelcBuildServer, srv.Addr())
	t.Setenv(util.EnvOtelcBuildServerAuth, srv.Token())

	cfgPath := filepath.Join(t.TempDir(), "importcfg")
	require.NoError(t, os.WriteFile(cfgPath, []byte("# import config\n"), 0o600))

	ip := &instrumentPhase{
		logger:           slog.Default(),
		workDir:          t.TempDir(),
		compileArgs:      []string{"compile", "-p", "example.com/app", "-o", filepath.Join(t.TempDir(), "out.a")},
		importConfigPath: cfgPath,
		importConfig:     imports.ImportConfig{PackageFile: map[string]string{}},
	}

	err = ip.updateImportConfig(t.Context(), map[string]string{
		"a": "example.com/hook/a",
		"b": "example.com/hook/b",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), batchCalls.Load())
	assert.Equal(t, "/cache/example.com/hook/a.a", ip.importConfig.PackageFile["example.com/hook/a"])
	assert.Equal(t, "/cache/example.com/hook/b.a", ip.importConfig.PackageFile["example.com/hook/b"])

	// Even when EnvOtelcNestedToolexec is set in-process (as EnableNestedToolexec
	// does at toolexec startup), an outer compile (nested=false) must send
	// Nested=false so per-package batching still runs.
	t.Setenv(util.EnvOtelcNestedToolexec, "1")
	var batchRan atomic.Bool
	srv2, err := buildserver.Start(t.Context(), buildserver.Config{
		ToolID:       "outer-nested-flag",
		RuleSets:     []*rule.InstRuleSet{rset},
		ResolveBatch: fakeBatchResolver(func() { batchRan.Store(true) }),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv2.Close(context.Background()) })
	t.Setenv(util.EnvOtelcBuildServer, srv2.Addr())
	t.Setenv(util.EnvOtelcBuildServerAuth, srv2.Token())
	ip2 := &instrumentPhase{
		logger:           slog.Default(),
		workDir:          t.TempDir(),
		compileArgs:      []string{"compile", "-p", "example.com/app", "-o", filepath.Join(t.TempDir(), "out.a")},
		importConfigPath: cfgPath,
		importConfig:     imports.ImportConfig{PackageFile: map[string]string{}},
	}
	require.NoError(t, ip2.updateImportConfig(t.Context(), map[string]string{"a": "example.com/hook/a"}))
	assert.True(t, batchRan.Load(), "outer compile must trigger batch resolver even when EnvOtelcNestedToolexec=1")

	// Cycle check must still reject an import already in EnvOtelcNestedResolving.
	t.Setenv(util.EnvOtelcNestedResolving, "example.com/hook/c>example.com/app")
	err = ip.updateImportConfig(t.Context(), map[string]string{"c": "example.com/hook/c"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import cycle")
}

func TestBuildServerResolveRuleAddedImportsLink(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcNestedToolexec, "1")

	rset := rule.NewInstRuleSet("example.com/app")
	rset.SetPackageName("app")
	rset.AddFuncRule("/src/app.go", &rule.InstFuncRule{
		InstBaseRule: rule.InstBaseRule{
			Name:    "hook",
			Imports: map[string]string{"a": "example.com/hook/a", "b": "example.com/hook/b"},
		},
		Func: "F",
	})
	writeMatchedRules(t, []*rule.InstRuleSet{rset})

	var batchCalls atomic.Int32
	var singleCalls atomic.Int32
	srv, err := buildserver.Start(t.Context(), buildserver.Config{
		ToolID:       "link-batch",
		RuleSets:     []*rule.InstRuleSet{rset},
		ResolveBatch: fakeBatchResolver(func() { batchCalls.Add(1) }),
		ResolveSingle: func(_ context.Context, imp string, _ []string, _ ...string) (map[string]string, error) {
			singleCalls.Add(1)
			return map[string]string{imp: "/cache/" + imp + ".a"}, nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close(context.Background()) })
	t.Setenv(util.EnvOtelcBuildServer, srv.Addr())
	t.Setenv(util.EnvOtelcBuildServerAuth, srv.Token())

	linkConfig := imports.ImportConfig{
		PackageFile: map[string]string{
			"example.com/app": "/unused/app.a",
		},
	}
	updated, err := resolveRuleAddedImports(t.Context(), nil, &linkConfig)
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Equal(
		t,
		int32(1),
		batchCalls.Load(),
		"link phase must use batch resolver even when EnvOtelcNestedToolexec=1",
	)
	assert.Equal(t, int32(0), singleCalls.Load())
	assert.Equal(t, "/cache/example.com/hook/a.a", linkConfig.PackageFile["example.com/hook/a"])
	assert.Equal(t, "/cache/example.com/hook/b.a", linkConfig.PackageFile["example.com/hook/b"])
}
