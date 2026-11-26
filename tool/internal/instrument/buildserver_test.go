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

func fakeArchivePath(cacheDir, imp string) string {
	return filepath.Join(cacheDir, filepath.FromSlash(imp)+".a")
}

func writeFakeArchive(t *testing.T, cacheDir, imp string) string {
	t.Helper()
	p := fakeArchivePath(cacheDir, imp)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte("archive"), 0o600))
	return p
}

func fakeBatchResolver(t *testing.T, cacheDir string, onCall func()) buildserver.BatchResolverFunc {
	t.Helper()
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
			out[imp] = map[string]string{imp: writeFakeArchive(t, cacheDir, imp)}
		}
		return out, nil, nil
	}
}

func TestBuildServerUpdateImportConfig(t *testing.T) {
	workDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))

	rset := rule.NewInstRuleSet("example.com/app")
	rset.AddFuncRule(filepath.Join(workDir, "app.go"), &rule.InstFuncRule{
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
		ResolveBatch: fakeBatchResolver(t, cacheDir, func() { batchCalls.Add(1) }),
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
	assert.Equal(t, fakeArchivePath(cacheDir, "example.com/hook/a"), ip.importConfig.PackageFile["example.com/hook/a"])
	assert.Equal(t, fakeArchivePath(cacheDir, "example.com/hook/b"), ip.importConfig.PackageFile["example.com/hook/b"])

	// Even when EnvOtelcNestedToolexec is set in-process (as EnableNestedToolexec
	// does at toolexec startup), an outer compile (nested=false) must send
	// Nested=false so per-package batching still runs.
	t.Setenv(util.EnvOtelcNestedToolexec, "1")
	var batchRan atomic.Bool
	srv2, err := buildserver.Start(t.Context(), buildserver.Config{
		ToolID:       "outer-nested-flag",
		RuleSets:     []*rule.InstRuleSet{rset},
		ResolveBatch: fakeBatchResolver(t, cacheDir, func() { batchRan.Store(true) }),
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
	cacheDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, workDir)
	t.Setenv(util.EnvOtelcNestedToolexec, "1")

	rset := rule.NewInstRuleSet("example.com/app")
	rset.SetPackageName("app")
	rset.AddFuncRule(filepath.Join(workDir, "app.go"), &rule.InstFuncRule{
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
		ResolveBatch: fakeBatchResolver(t, cacheDir, func() { batchCalls.Add(1) }),
		ResolveSingle: func(_ context.Context, imp string, _ []string, _ ...string) (map[string]string, error) {
			singleCalls.Add(1)
			return map[string]string{imp: writeFakeArchive(t, cacheDir, imp)}, nil
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
	assert.Equal(t, fakeArchivePath(cacheDir, "example.com/hook/a"), linkConfig.PackageFile["example.com/hook/a"])
	assert.Equal(t, fakeArchivePath(cacheDir, "example.com/hook/b"), linkConfig.PackageFile["example.com/hook/b"])
}

func TestRulesDigestFromDiskForFlags(t *testing.T) {
	t.Run("with rules_digest file", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		t.Setenv("GOFLAGS", "")
		t.Setenv(util.EnvOtelcBuildFlags, "")
		require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
		require.NoError(t, os.WriteFile(util.GetRulesDigestFile(), []byte("basedigest123456"), 0o644))

		flags := []string{"-tags=enterprise"}
		forFlags := RulesDigestFromDiskForFlags(flags)
		assert.NotEqual(t, RulesDigestFromDisk(), forFlags)

		t.Setenv(util.EnvOtelcBuildFlags, util.EncodeBuildFlags(flags))
		assert.Equal(t, RulesDigestFromDisk(), forFlags)
	})

	t.Run("with matched rules fallback and GOFLAGS", func(t *testing.T) {
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		t.Setenv("GOFLAGS", "-tags=base")
		t.Setenv(util.EnvOtelcBuildFlags, "")
		require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o755))
		require.NoError(t, os.WriteFile(util.GetMatchedRuleFile(), []byte(`[]`), 0o644))

		flags := []string{"-tags=enterprise"}
		forFlags := RulesDigestFromDiskForFlags(flags)
		assert.NotEqual(t, RulesDigestFromDisk(), forFlags)

		t.Setenv(util.EnvOtelcBuildFlags, util.EncodeBuildFlags(flags))
		assert.Equal(t, RulesDigestFromDisk(), forFlags)
	})
}

func TestBuildServerExportClosureValidation(t *testing.T) {
	t.Run("rejects archive conflict and leaves importcfg untouched", func(t *testing.T) {
		cacheDir := t.TempDir()
		existingShared := writeFakeArchive(t, filepath.Join(cacheDir, "caller"), "example.com/shared")
		serverShared := writeFakeArchive(t, filepath.Join(cacheDir, "server"), "example.com/shared")
		hookA := writeFakeArchive(t, cacheDir, "example.com/hook/a")

		srv, err := buildserver.Start(t.Context(), buildserver.Config{
			ToolID: "conflict",
			ResolveSingle: func(_ context.Context, imp string, _ []string, _ ...string) (map[string]string, error) {
				return map[string]string{
					imp:                  hookA,
					"example.com/shared": serverShared,
				}, nil
			},
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = srv.Close(context.Background()) })
		t.Setenv(util.EnvOtelcBuildServer, srv.Addr())
		t.Setenv(util.EnvOtelcBuildServerAuth, srv.Token())

		ip := &instrumentPhase{
			logger:      slog.Default(),
			compileArgs: []string{"compile", "-p", "example.com/app"},
			importConfig: imports.ImportConfig{
				PackageFile: map[string]string{"example.com/shared": existingShared},
			},
		}
		var memoHits, goListCalls int
		updated, err := ip.resolveImportsViaServer(
			t.Context(), []string{"example.com/hook/a"}, false, &memoHits, &goListCalls,
		)
		require.ErrorIs(t, err, buildserver.ErrUnavailable)
		assert.False(t, updated)
		assert.Equal(t, map[string]string{"example.com/shared": existingShared}, ip.importConfig.PackageFile)

		client := buildserver.ClientFromEnv()
		require.NotNil(t, client)
		_, err = resolveLinkExportsViaServer(
			t.Context(), client, []string{"example.com/hook/a"}, false, ip.importConfig.PackageFile,
		)
		require.ErrorIs(t, err, buildserver.ErrUnavailable)
	})

	t.Run("rejects missing archive file on disk", func(t *testing.T) {
		missingArchive := filepath.Join(t.TempDir(), "gone.a")
		srv, err := buildserver.Start(t.Context(), buildserver.Config{
			ToolID: "missing-file",
			ResolveSingle: func(_ context.Context, imp string, _ []string, _ ...string) (map[string]string, error) {
				return map[string]string{imp: missingArchive}, nil
			},
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = srv.Close(context.Background()) })
		t.Setenv(util.EnvOtelcBuildServer, srv.Addr())
		t.Setenv(util.EnvOtelcBuildServerAuth, srv.Token())

		ip := &instrumentPhase{
			logger:       slog.Default(),
			compileArgs:  []string{"compile", "-p", "example.com/app"},
			importConfig: imports.ImportConfig{PackageFile: map[string]string{}},
		}
		var memoHits, goListCalls int
		updated, err := ip.resolveImportsViaServer(
			t.Context(), []string{"example.com/hook/a"}, false, &memoHits, &goListCalls,
		)
		require.ErrorIs(t, err, buildserver.ErrUnavailable)
		assert.False(t, updated)
		assert.Empty(t, ip.importConfig.PackageFile)

		client := buildserver.ClientFromEnv()
		require.NotNil(t, client)
		_, err = resolveLinkExportsViaServer(
			t.Context(), client, []string{"example.com/hook/a"}, false, ip.importConfig.PackageFile,
		)
		require.ErrorIs(t, err, buildserver.ErrUnavailable)
	})

	t.Run("fallbacks and server-reported failures", func(t *testing.T) {
		ip := &instrumentPhase{
			logger:       slog.Default(),
			compileArgs:  []string{"compile", "-p", "example.com/app"},
			importConfig: imports.ImportConfig{PackageFile: map[string]string{}},
		}
		assert.Nil(t, ip.loadCompileStateFromServer(t.Context(), ""))

		t.Setenv(util.EnvOtelcBuildServer, "")
		t.Setenv(util.EnvOtelcBuildServerAuth, "")
		var memoHits, goListCalls int
		_, err := ip.resolveImportsViaServer(
			t.Context(), []string{"example.com/hook/a"}, false, &memoHits, &goListCalls,
		)
		require.ErrorIs(t, err, buildserver.ErrUnavailable)

		ip.resolveAddedImport = func(context.Context, string, ...string) (map[string]string, error) {
			return map[string]string{}, nil
		}
		_, err = ip.resolveImportsViaServer(
			t.Context(), []string{"example.com/hook/a"}, false, &memoHits, &goListCalls,
		)
		require.ErrorIs(t, err, buildserver.ErrUnavailable)
		ip.resolveAddedImport = nil

		srv, err := buildserver.Start(t.Context(), buildserver.Config{
			ToolID:   "errors",
			RuleSets: []*rule.InstRuleSet{rule.NewInstRuleSet("example.com/app")},
			ResolveSingle: func(_ context.Context, _ string, _ []string, _ ...string) (map[string]string, error) {
				return nil, os.ErrNotExist
			},
		})
		require.NoError(t, err)
		t.Setenv(util.EnvOtelcBuildServer, srv.Addr())
		t.Setenv(util.EnvOtelcBuildServerAuth, srv.Token())

		resp := ip.loadCompileStateFromServer(t.Context(), "example.com/app")
		require.NotNil(t, resp)
		assert.NotNil(t, ip.importNames)

		_, err = ip.resolveImportsViaServer(
			t.Context(), []string{"example.com/hook/a"}, false, &memoHits, &goListCalls,
		)
		require.ErrorContains(t, err, "resolving")

		client := buildserver.ClientFromEnv()
		require.NotNil(t, client)
		linkRes, err := resolveLinkExportsViaServer(t.Context(), client, []string{"example.com/hook/a"}, false, nil)
		require.NoError(t, err)
		assert.Contains(t, linkRes.failed, "example.com/hook/a")

		require.NoError(t, srv.Close(context.Background()))
		assert.Nil(t, ip.loadCompileStateFromServer(t.Context(), "example.com/app"))
		_, err = ip.resolveImportsViaServer(
			t.Context(), []string{"example.com/hook/a"}, false, &memoHits, &goListCalls,
		)
		require.ErrorIs(t, err, buildserver.ErrUnavailable)
		_, err = resolveLinkExportsViaServer(t.Context(), client, []string{"example.com/hook/a"}, false, nil)
		require.ErrorIs(t, err, buildserver.ErrUnavailable)
	})
}
