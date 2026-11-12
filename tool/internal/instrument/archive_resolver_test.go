// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

func TestFindGoBinary(t *testing.T) {
	t.Run("empty tool path resolves go from PATH", func(t *testing.T) {
		bin, err := findGoBinary("")
		require.NoError(t, err)
		assert.Contains(t, bin, "go")
	})

	t.Run("non-existent tool path returns error", func(t *testing.T) {
		_, err := findGoBinary("/non/existent/tool/compile")
		require.Error(t, err)
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

		resolved, err := findGoBinary(compileTool)
		require.NoError(t, err)
		assert.Equal(t, goBin, resolved)
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

	t.Run("returns empty string if _pkg_.a does not exist to reject compiler-only artifact", func(t *testing.T) {
		missingExport := filepath.Join(exportDir, "b003.a")
		got := toLinkerArchive(tempDir, missingExport)
		assert.Empty(t, got)
	})

	t.Run("keeps cached archive unchanged", func(t *testing.T) {
		cached := "/home/user/.cache/go-build/ab/abcdef-d"
		got := toLinkerArchive(tempDir, cached)
		assert.Equal(t, cached, got)
	})

	t.Run("does not match directory with export prefix but different name", func(t *testing.T) {
		exportOtherDir := filepath.Join(tempDir, "export_other")
		require.NoError(t, os.MkdirAll(exportOtherDir, 0o755))
		otherArchive := filepath.Join(exportOtherDir, "b002.a")
		require.NoError(t, os.WriteFile(otherArchive, []byte("other"), 0o644))
		got := toLinkerArchive(tempDir, otherArchive)
		assert.Equal(t, otherArchive, got)
	})
}

func TestCollectArchivesFromWorkDir(t *testing.T) {
	t.Run("collects compiler and linker archives from b001 and other actions", func(t *testing.T) {
		workDir := t.TempDir()
		b001Dir := filepath.Join(workDir, "b001")
		b002Dir := filepath.Join(workDir, "b002")
		require.NoError(t, os.MkdirAll(b001Dir, 0o755))
		require.NoError(t, os.MkdirAll(b002Dir, 0o755))

		compPath := filepath.Join(workDir, "comp.a")
		linkPath := filepath.Join(workDir, "link.a")
		depComp := filepath.Join(workDir, "dep_comp.a")
		depLink := filepath.Join(workDir, "dep_link.a")
		require.NoError(t, os.WriteFile(compPath, []byte("comp"), 0o644))
		require.NoError(t, os.WriteFile(linkPath, []byte("link"), 0o644))
		require.NoError(t, os.WriteFile(depComp, []byte("depcomp"), 0o644))
		require.NoError(t, os.WriteFile(depLink, []byte("deplink"), 0o644))

		b001LinkCfg := "packagefile example.com/pkg=" + linkPath + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(b001Dir, "importcfg.link"), []byte(b001LinkCfg), 0o644))

		b001CompCfg := "packagefile example.com/pkg=" + compPath + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(b001Dir, "importcfg"), []byte(b001CompCfg), 0o644))

		b002LinkCfg := "packagefile example.com/dep=" + depLink + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(b002Dir, "importcfg.link"), []byte(b002LinkCfg), 0o644))

		b002CompCfg := "packagefile example.com/dep=" + depComp + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(b002Dir, "importcfg"), []byte(b002CompCfg), 0o644))

		compileArchives, linkArchives, err := collectArchivesFromWorkDir(workDir)
		require.NoError(t, err)

		assert.Equal(t, compPath, compileArchives["example.com/pkg"])
		assert.Equal(t, linkPath, linkArchives["example.com/pkg"])
		assert.Equal(t, depComp, compileArchives["example.com/dep"])
		assert.Equal(t, depLink, linkArchives["example.com/dep"])
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

	const cfgHash = "testcfg1"

	aComp := filepath.Join(tempDir, "a.a")
	aLink := filepath.Join(tempDir, "a_pkg.a")
	bComp := filepath.Join(tempDir, "b.a")
	bLink := filepath.Join(tempDir, "b_pkg.a")
	require.NoError(t, os.WriteFile(aComp, []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(aLink, []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(bComp, []byte("b"), 0o644))
	require.NoError(t, os.WriteFile(bLink, []byte("b"), 0o644))

	cache := loadArchiveCache(cfgHash)
	assert.Empty(t, cache.Compile)
	assert.Empty(t, cache.Link)

	require.NoError(t, saveArchiveCache(
		cfgHash,
		map[string]string{"pkg/a": aComp},
		map[string]string{"pkg/a": aLink},
	))

	loaded := loadArchiveCache(cfgHash)
	assert.Equal(t, aComp, loaded.Compile["pkg/a"])
	assert.Equal(t, aLink, loaded.Link["pkg/a"])

	require.NoError(t, saveArchiveCache(
		cfgHash,
		map[string]string{"pkg/b": bComp},
		map[string]string{"pkg/b": bLink},
	))

	updated := loadArchiveCache(cfgHash)
	assert.Equal(t, aComp, updated.Compile["pkg/a"])
	assert.Equal(t, bComp, updated.Compile["pkg/b"])
	assert.Equal(t, aLink, updated.Link["pkg/a"])
	assert.Equal(t, bLink, updated.Link["pkg/b"])
}

func TestArchiveCache_ConfigurationIsolation(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	aComp := filepath.Join(tempDir, "a_comp.a")
	aLink := filepath.Join(tempDir, "a_link.a")
	bComp := filepath.Join(tempDir, "b_comp.a")
	bLink := filepath.Join(tempDir, "b_link.a")
	require.NoError(t, os.WriteFile(aComp, []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(aLink, []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(bComp, []byte("b"), 0o644))
	require.NoError(t, os.WriteFile(bLink, []byte("b"), 0o644))

	// Save under config A
	require.NoError(t, saveArchiveCache(
		"configA",
		map[string]string{"pkg/common": aComp},
		map[string]string{"pkg/common": aLink},
	))

	// Load under config B -> must be empty
	cacheB := loadArchiveCache("configB")
	assert.Empty(t, cacheB.Compile)
	assert.Empty(t, cacheB.Link)

	// Save under config B with different artifacts
	require.NoError(t, saveArchiveCache(
		"configB",
		map[string]string{"pkg/common": bComp},
		map[string]string{"pkg/common": bLink},
	))

	// Config A and Config B must remain isolated
	loadedA := loadArchiveCache("configA")
	assert.Equal(t, aComp, loadedA.Compile["pkg/common"])
	assert.Equal(t, aLink, loadedA.Link["pkg/common"])

	loadedB := loadArchiveCache("configB")
	assert.Equal(t, bComp, loadedB.Compile["pkg/common"])
	assert.Equal(t, bLink, loadedB.Link["pkg/common"])
}

func TestArchiveCache_PrunesMissingFiles(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	const cfgHash = "testcfg2"

	require.NoError(t, saveArchiveCache(
		cfgHash,
		map[string]string{"pkg/missing": "/nonexistent/compile.a"},
		map[string]string{"pkg/missing": "/nonexistent/link.a"},
	))

	cache := loadArchiveCache(cfgHash)
	assert.Empty(t, cache.Compile)
	assert.Empty(t, cache.Link)
}

func TestArchiveCache_ConcurrentGoroutineSimulation(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	const cfgHash = "concurrentcfg"
	const numGoroutines = 10
	var wg sync.WaitGroup

	for i := range numGoroutines {
		pkgName := fmt.Sprintf("pkg/%d", i)
		cFile := filepath.Join(tempDir, fmt.Sprintf("comp_%d.a", i))
		lFile := filepath.Join(tempDir, fmt.Sprintf("link_%d.a", i))
		require.NoError(t, os.WriteFile(cFile, []byte("c"), 0o644))
		require.NoError(t, os.WriteFile(lFile, []byte("l"), 0o644))

		wg.Add(1)
		go func(pkg, c, l string) {
			defer wg.Done()
			_ = saveArchiveCache(cfgHash, map[string]string{pkg: c}, map[string]string{pkg: l})
		}(pkgName, cFile, lFile)
	}

	wg.Wait()

	cache := loadArchiveCache(cfgHash)
	for i := range numGoroutines {
		pkgName := fmt.Sprintf("pkg/%d", i)
		assert.NotEmpty(t, cache.Compile[pkgName], "package %s should be present in compile cache", pkgName)
		assert.NotEmpty(t, cache.Link[pkgName], "package %s should be present in link cache", pkgName)
	}
}

func TestArchiveCache_IgnoresMalformedJSON(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	const cfgHash = "malformedcfg"
	cacheDir := filepath.Join(util.GetBuildTempDir(), resolvedArchivesDirName, cfgHash)
	require.NoError(t, os.MkdirAll(cacheDir, 0o750))
	badFile := filepath.Join(cacheDir, "probe_corrupt.json")
	require.NoError(t, os.WriteFile(badFile, []byte("{ malformed json"), 0o600))

	cache := loadArchiveCache(cfgHash)
	assert.Empty(t, cache.Compile)
}

func TestArchiveCache_SaveErrorsObservable(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	// Create a file blocking directory creation
	const cfgHash = "unwritablecfg"
	require.NoError(t, os.MkdirAll(util.GetBuildTempDir(), 0o750))
	blocker := filepath.Join(util.GetBuildTempDir(), resolvedArchivesDirName)
	require.NoError(t, os.WriteFile(blocker, []byte("blocking-file"), 0o644))

	err := saveArchiveCache(cfgHash, map[string]string{"pkg/x": "/some/path"}, map[string]string{"pkg/x": "/some/path"})
	require.Error(t, err)
}

func TestRecordAndCleanupProbeWorkDirs(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	workBase := filepath.Join(util.GetBuildTempDir(), probeWorkDirName)
	cacheBase := filepath.Join(util.GetBuildTempDir(), resolvedArchivesDirName)
	directBase := filepath.Join(util.GetBuildTempDir(), directArchivesDirName)
	require.NoError(t, os.MkdirAll(workBase, 0o750))
	require.NoError(t, os.MkdirAll(cacheBase, 0o750))
	require.NoError(t, os.MkdirAll(directBase, 0o750))

	assert.True(t, util.PathExists(workBase))
	assert.True(t, util.PathExists(cacheBase))
	assert.True(t, util.PathExists(directBase))

	CleanupProbeWorkDirs()

	assert.False(t, util.PathExists(workBase))
	assert.False(t, util.PathExists(cacheBase))
	assert.False(t, util.PathExists(directBase))
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

func TestComputeConfigHash(t *testing.T) {
	hash1 := computeConfigHash("", []string{"-tags", "foo"})
	hash2 := computeConfigHash("", []string{"-tags", "foo"})
	assert.Equal(t, hash1, hash2, "identical configurations must produce identical hash")

	hashDiffTags := computeConfigHash("", []string{"-tags", "bar"})
	assert.NotEqual(t, hash1, hashDiffTags, "different tags must produce different hash")

	hashOrder1 := computeConfigHash("", []string{"-tags=a", "-tags=b"})
	hashOrder2 := computeConfigHash("", []string{"-tags=b", "-tags=a"})
	assert.NotEqual(t, hashOrder1, hashOrder2, "reordered flags must produce different hash")

	hashDiffPGO := computeConfigHash("", []string{"-tags", "foo", "-pgo=some.pgo"})
	assert.NotEqual(t, hash1, hashDiffPGO, "different PGO setting must produce different hash")

	altGOOS := "darwin"
	if runtime.GOOS == "darwin" {
		altGOOS = "linux"
	}
	t.Setenv("GOOS", altGOOS)
	hashAltOS := computeConfigHash("", []string{"-tags", "foo"})
	assert.NotEqual(t, hash1, hashAltOS, "different GOOS must produce different hash")

	t.Setenv("GOAMD64", "v3")
	hashAMD64v3 := computeConfigHash("", []string{"-tags", "foo"})
	t.Setenv("GOAMD64", "v1")
	hashAMD64v1 := computeConfigHash("", []string{"-tags", "foo"})
	assert.NotEqual(t, hashAMD64v3, hashAMD64v1, "different architecture feature levels must produce different hash")
}

func TestComputeConfigHash_CompilerAndLinkerIdentical(t *testing.T) {
	tempDir := t.TempDir()
	toolDir := filepath.Join(tempDir, "pkg", "tool", runtime.GOOS+"_"+runtime.GOARCH)
	binDir := filepath.Join(tempDir, "bin")
	require.NoError(t, os.MkdirAll(toolDir, 0o755))
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	var exeSuffix string
	if runtime.GOOS == "windows" {
		exeSuffix = ".exe"
	}

	compileTool := filepath.Join(toolDir, "compile"+exeSuffix)
	linkTool := filepath.Join(toolDir, "link"+exeSuffix)
	goBin := filepath.Join(binDir, "go"+exeSuffix)

	require.NoError(t, os.WriteFile(compileTool, []byte("mock-compile"), 0o755))
	require.NoError(t, os.WriteFile(linkTool, []byte("mock-link"), 0o755))
	require.NoError(t, os.WriteFile(goBin, []byte("mock-go"), 0o755))

	flags := []string{"-tags", "foo"}
	compileHash := computeConfigHash(compileTool, flags)
	linkHash := computeConfigHash(linkTool, flags)

	assert.Equal(
		t,
		compileHash,
		linkHash,
		"compiler and linker in the same toolchain must share the same config hash so linker can hit compiler cache",
	)
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

func TestParseActionConfigs_Deterministic(t *testing.T) {
	workDir := t.TempDir()
	b001Dir := filepath.Join(workDir, "b001")
	b002Dir := filepath.Join(workDir, "b002")
	require.NoError(t, os.MkdirAll(b001Dir, 0o755))
	require.NoError(t, os.MkdirAll(b002Dir, 0o755))

	b001Comp := filepath.Join(workDir, "b001.a")
	b002Comp := filepath.Join(workDir, "b002.a")
	require.NoError(t, os.WriteFile(b001Comp, []byte("b001"), 0o644))
	require.NoError(t, os.WriteFile(b002Comp, []byte("b002"), 0o644))

	// b001 and b002 both define example.com/conflict
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(b001Dir, "importcfg"),
			[]byte("packagefile example.com/conflict="+b001Comp+"\n"),
			0o644,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(b002Dir, "importcfg"),
			[]byte("packagefile example.com/conflict="+b002Comp+"\n"),
			0o644,
		),
	)

	entries, err := os.ReadDir(workDir)
	require.NoError(t, err)

	compileArchives := make(map[string]string)
	linkArchives := make(map[string]string)
	parseActionConfigs(workDir, entries, compileArchives, linkArchives)

	// b001 must win deterministically
	assert.Equal(t, b001Comp, compileArchives["example.com/conflict"])
}

func TestDirectProbeArchiveUniqueness(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	ctx := t.Context()
	modDir := filepath.Join(tempDir, "testmod")
	secretDir := filepath.Join(modDir, "internal", "secret")
	require.NoError(t, os.MkdirAll(secretDir, 0o755))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(modDir, "go.mod"), []byte("module example.com/testmod\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(secretDir, "secret.go"),
			[]byte("package secret\nfunc Key() string { return \"abc\" }\n"),
			0o644,
		),
	)

	// Run from modDir
	t.Setenv(util.EnvOtelcWorkDir, modDir)

	configA := "cfgA"
	configB := "cfgB"

	c1, l1, err1 := runDirectProbeBuild(ctx, "", "example.com/testmod/internal/secret", nil, configA)
	require.NoError(t, err1)
	c2, l2, err2 := runDirectProbeBuild(ctx, "", "example.com/testmod/internal/secret", nil, configB)
	require.NoError(t, err2)

	assert.NotEmpty(t, c1["example.com/testmod/internal/secret"])
	assert.NotEmpty(t, c2["example.com/testmod/internal/secret"])
	assert.NotEqual(
		t,
		c1["example.com/testmod/internal/secret"],
		c2["example.com/testmod/internal/secret"],
		"different configurations must produce separate archive paths",
	)
	assert.Equal(t, c1["example.com/testmod/internal/secret"], l1["example.com/testmod/internal/secret"])
	assert.Equal(t, c2["example.com/testmod/internal/secret"], l2["example.com/testmod/internal/secret"])
	assert.True(t, util.PathExists(c1["example.com/testmod/internal/secret"]))
	assert.True(t, util.PathExists(c2["example.com/testmod/internal/secret"]))
}
