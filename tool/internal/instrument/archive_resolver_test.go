// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestFindGoBinary(t *testing.T) {
	t.Run("empty tool path falls back to go", func(t *testing.T) {
		assert.Equal(t, "go", findGoBinary(""))
	})

	t.Run("non-existent tool path falls back to go", func(t *testing.T) {
		assert.Equal(t, "go", findGoBinary("/non/existent/tool/compile"))
	})

	t.Run("valid tool path resolves matching go binary", func(t *testing.T) {
		tempDir := t.TempDir()
		toolDir := filepath.Join(tempDir, "pkg", "tool", "linux_amd64")
		binDir := filepath.Join(tempDir, "bin")
		require.NoError(t, os.MkdirAll(toolDir, 0o755))
		require.NoError(t, os.MkdirAll(binDir, 0o755))

		compileTool := filepath.Join(toolDir, "compile")
		goBin := filepath.Join(binDir, "go")
		require.NoError(t, os.WriteFile(compileTool, []byte("compile"), 0o755))
		require.NoError(t, os.WriteFile(goBin, []byte("go"), 0o755))

		assert.Equal(t, goBin, findGoBinary(compileTool))
	})
}

func TestParseWorkDir(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		expected string
	}{
		{
			name:     "empty stderr",
			stderr:   "",
			expected: "",
		},
		{
			name:     "standard go build work line",
			stderr:   "WORK=/tmp/go-build123456789\n# main\n",
			expected: "/tmp/go-build123456789",
		},
		{
			name:     "windows path work line",
			stderr:   "WORK=C:\\Users\\user\\AppData\\Local\\Temp\\go-build123\n",
			expected: "C:\\Users\\user\\AppData\\Local\\Temp\\go-build123",
		},
		{
			name:     "no work line in stderr",
			stderr:   "cannot find package\nsome error\n",
			expected: "",
		},
		{
			name:     "work line surrounded by whitespace",
			stderr:   "  \n  WORK=/var/tmp/go-build999  \n",
			expected: "/var/tmp/go-build999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseWorkDir(tt.stderr))
		})
	}
}

func TestToLinkerArchive(t *testing.T) {
	tempDir := t.TempDir()
	exportDir := filepath.Join(tempDir, "export")
	b002Dir := filepath.Join(tempDir, "b002")
	require.NoError(t, os.MkdirAll(exportDir, 0o755))
	require.NoError(t, os.MkdirAll(b002Dir, 0o755))

	exportArchive := filepath.Join(exportDir, "b002.a")
	linkerArchive := filepath.Join(b002Dir, "_pkg_.a")
	require.NoError(t, os.WriteFile(exportArchive, []byte("export"), 0o644))
	require.NoError(t, os.WriteFile(linkerArchive, []byte("linker"), 0o644))

	t.Run("converts export archive to linker archive when present", func(t *testing.T) {
		got := toLinkerArchive(tempDir, exportArchive)
		assert.Equal(t, linkerArchive, got)
	})

	t.Run("keeps export archive if _pkg_.a does not exist", func(t *testing.T) {
		missingExport := filepath.Join(exportDir, "b003.a")
		got := toLinkerArchive(tempDir, missingExport)
		assert.Equal(t, missingExport, got)
	})

	t.Run("keeps cached archive unchanged", func(t *testing.T) {
		cached := "/home/user/.cache/go-build/ab/abcdef-d"
		got := toLinkerArchive(tempDir, cached)
		assert.Equal(t, cached, got)
	})
}

func TestCollectArchivesFromWorkDir(t *testing.T) {
	t.Run("collects and transforms archives from work dir", func(t *testing.T) {
		workDir := t.TempDir()
		b001Dir := filepath.Join(workDir, "b001")
		b002Dir := filepath.Join(workDir, "b002")
		exportDir := filepath.Join(workDir, "export")
		require.NoError(t, os.MkdirAll(b001Dir, 0o755))
		require.NoError(t, os.MkdirAll(b002Dir, 0o755))
		require.NoError(t, os.MkdirAll(exportDir, 0o755))

		cachedFile := filepath.Join(workDir, "cached-d")
		require.NoError(t, os.WriteFile(cachedFile, []byte("cached"), 0o644))

		linkerArchive := filepath.Join(b002Dir, "_pkg_.a")
		require.NoError(t, os.WriteFile(linkerArchive, []byte("archive"), 0o644))

		b001Cfg := "packagefile example.com/cached=" + cachedFile + "\n" +
			"packagefile example.com/fresh=" + filepath.Join(exportDir, "b002.a") + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(b001Dir, "importcfg"), []byte(b001Cfg), 0o644))

		b002Cfg := "packagefile example.com/dep=" + cachedFile + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(b002Dir, "importcfg"), []byte(b002Cfg), 0o644))

		compileArchives, linkArchives, err := collectArchivesFromWorkDir(workDir)
		require.NoError(t, err)

		assert.Equal(t, cachedFile, compileArchives["example.com/cached"])
		assert.Equal(t, filepath.Join(exportDir, "b002.a"), compileArchives["example.com/fresh"])
		assert.Equal(t, cachedFile, compileArchives["example.com/dep"])

		assert.Equal(t, cachedFile, linkArchives["example.com/cached"])
		assert.Equal(t, linkerArchive, linkArchives["example.com/fresh"])
		assert.Equal(t, cachedFile, linkArchives["example.com/dep"])
	})

	t.Run("collects linker archives from importcfg.link when present", func(t *testing.T) {
		workDir := t.TempDir()
		b001Dir := filepath.Join(workDir, "b001")
		require.NoError(t, os.MkdirAll(b001Dir, 0o755))

		linkCfg := "packagefile example.com/pkg=/link/path/pkg.a\n"
		require.NoError(t, os.WriteFile(filepath.Join(b001Dir, "importcfg.link"), []byte(linkCfg), 0o644))

		compCfg := "packagefile example.com/pkg=/comp/path/pkg.a\n"
		require.NoError(t, os.WriteFile(filepath.Join(b001Dir, "importcfg"), []byte(compCfg), 0o644))

		compileArchives, linkArchives, err := collectArchivesFromWorkDir(workDir)
		require.NoError(t, err)
		assert.Equal(t, "/comp/path/pkg.a", compileArchives["example.com/pkg"])
		assert.Equal(t, "/link/path/pkg.a", linkArchives["example.com/pkg"])
	})

	t.Run("fails on non-existent work dir", func(t *testing.T) {
		_, _, err := collectArchivesFromWorkDir("/non/existent/work/dir")
		require.Error(t, err)
	})

	t.Run("fails when no importcfg found", func(t *testing.T) {
		workDir := t.TempDir()
		_, _, err := collectArchivesFromWorkDir(workDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no importcfg found")
	})
}

func TestArchiveCache_LoadSave(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	// Create dummy artifact files on disk to satisfy validity checks
	aComp := filepath.Join(tempDir, "a.a")
	aLink := filepath.Join(tempDir, "a_pkg.a")
	bComp := filepath.Join(tempDir, "b.a")
	bLink := filepath.Join(tempDir, "b_pkg.a")
	require.NoError(t, os.WriteFile(aComp, []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(aLink, []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(bComp, []byte("b"), 0o644))
	require.NoError(t, os.WriteFile(bLink, []byte("b"), 0o644))

	cache := loadArchiveCache()
	assert.Empty(t, cache.Compile)
	assert.Empty(t, cache.Link)

	saveArchiveCache(
		map[string]string{"pkg/a": aComp},
		map[string]string{"pkg/a": aLink},
	)

	loaded := loadArchiveCache()
	assert.Equal(t, aComp, loaded.Compile["pkg/a"])
	assert.Equal(t, aLink, loaded.Link["pkg/a"])

	saveArchiveCache(
		map[string]string{"pkg/b": bComp},
		map[string]string{"pkg/b": bLink},
	)

	updated := loadArchiveCache()
	assert.Equal(t, aComp, updated.Compile["pkg/a"])
	assert.Equal(t, bComp, updated.Compile["pkg/b"])
	assert.Equal(t, aLink, updated.Link["pkg/a"])
	assert.Equal(t, bLink, updated.Link["pkg/b"])
}

func TestArchiveCache_PrunesMissingFiles(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	// Write cache with paths to nonexistent files
	saveArchiveCache(
		map[string]string{"pkg/missing": "/nonexistent/compile.a"},
		map[string]string{"pkg/missing": "/nonexistent/link.a"},
	)

	// loadArchiveCache must prune nonexistent files
	cache := loadArchiveCache()
	assert.Empty(t, cache.Compile)
	assert.Empty(t, cache.Link)
}

func TestArchiveCache_ConcurrentMultiProcessSimulation(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	const numProcesses = 10
	var wg sync.WaitGroup

	// Create real artifact files for each process
	for i := range numProcesses {
		pkgName := fmt.Sprintf("pkg/%d", i)
		cFile := filepath.Join(tempDir, fmt.Sprintf("comp_%d.a", i))
		lFile := filepath.Join(tempDir, fmt.Sprintf("link_%d.a", i))
		require.NoError(t, os.WriteFile(cFile, []byte("c"), 0o644))
		require.NoError(t, os.WriteFile(lFile, []byte("l"), 0o644))

		wg.Add(1)
		go func(pkg, c, l string) {
			defer wg.Done()
			saveArchiveCache(map[string]string{pkg: c}, map[string]string{pkg: l})
		}(pkgName, cFile, lFile)
	}

	wg.Wait()

	// loadArchiveCache must merge all distinct process records without losing any
	cache := loadArchiveCache()
	for i := range numProcesses {
		pkgName := fmt.Sprintf("pkg/%d", i)
		assert.NotEmpty(t, cache.Compile[pkgName], "package %s should be present in compile cache", pkgName)
		assert.NotEmpty(t, cache.Link[pkgName], "package %s should be present in link cache", pkgName)
	}
}

func TestArchiveCache_IgnoresMalformedJSON(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	cacheDir := filepath.Join(util.GetBuildTempDir(), resolvedArchivesDirName)
	require.NoError(t, os.MkdirAll(cacheDir, 0o750))
	badFile := filepath.Join(cacheDir, "probe_corrupt.json")
	require.NoError(t, os.WriteFile(badFile, []byte("{ malformed json"), 0o600))

	// Should not panic or error
	cache := loadArchiveCache()
	assert.Empty(t, cache.Compile)
}

func TestRecordAndCleanupProbeWorkDirs(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	workBase := filepath.Join(util.GetBuildTempDir(), probeWorkDirName)
	cacheBase := filepath.Join(util.GetBuildTempDir(), resolvedArchivesDirName)
	require.NoError(t, os.MkdirAll(workBase, 0o750))
	require.NoError(t, os.MkdirAll(cacheBase, 0o750))

	assert.True(t, util.PathExists(workBase))
	assert.True(t, util.PathExists(cacheBase))

	CleanupProbeWorkDirs()

	assert.False(t, util.PathExists(workBase))
	assert.False(t, util.PathExists(cacheBase))
}

func TestProbeEnv(t *testing.T) {
	t.Setenv("GOTMPDIR", "/old/temp")
	env := probeEnv("/new/temp")
	gotTmpCount := 0
	for _, kv := range env {
		switch kv {
		case "GOTMPDIR=/new/temp":
			gotTmpCount++
		case "GOTMPDIR=/old/temp":
			t.Errorf("old GOTMPDIR was not removed")
		}
	}
	assert.Equal(t, 1, gotTmpCount)
}

func TestIsInternalPackage(t *testing.T) {
	tests := []struct {
		pkg      string
		expected bool
	}{
		{"internal", true},
		{"internal/foo", true},
		{"example.com/lib/internal/bar", true},
		{"example.com/lib/internal", true},
		{"example.com/internalizing", false},
		{"example.com/lib/notinternal/foo", false},
		{"fmt", false},
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			assert.Equal(t, tt.expected, isInternalPackage(tt.pkg))
		})
	}
}

func TestResolvePackageArchives_GoLess128(t *testing.T) {
	ctx := t.Context()

	t.Run("empty import paths", func(t *testing.T) {
		compile, link, failed, err := resolvePackageArchives(ctx, "", nil, nil)
		require.NoError(t, err)
		assert.Empty(t, failed)
		assert.Empty(t, compile)
		assert.Empty(t, link)
	})

	t.Run("fallback on tool without cmd/export", func(t *testing.T) {
		compile, link, failed, err := resolvePackageArchives(ctx, "", []string{"fmt"}, nil)
		require.NoError(t, err)
		assert.Empty(t, failed)
		assert.NotEmpty(t, compile["fmt"])
		assert.Equal(t, compile["fmt"], link["fmt"])
	})
}

func TestNestedResolutionLIFORestoration(t *testing.T) {
	t.Setenv(util.EnvOtelcNestedResolving, "initial_val")

	ip := &instrumentPhase{
		compileArgs: []string{"compile", "-p", "example.com/testpkg"},
	}

	r1, err1 := ip.enterNestedResolution("example.com/dep1")
	require.NoError(t, err1)
	assert.Contains(t, os.Getenv(util.EnvOtelcNestedResolving), "dep1")

	r2, err2 := ip.enterNestedResolution("example.com/dep2")
	require.NoError(t, err2)
	assert.Contains(t, os.Getenv(util.EnvOtelcNestedResolving), "dep2")

	// Execute restores in LIFO order
	restores := []func(){r1, r2}
	for _, restore := range slices.Backward(restores) {
		restore()
	}

	assert.Equal(t, "initial_val", os.Getenv(util.EnvOtelcNestedResolving))
}
