// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"fmt"
	"os"
	"os/exec"
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
	t.Run("overrides GOTMPDIR", func(t *testing.T) {
		t.Setenv("GOTMPDIR", "/old/temp")
		env := probeEnv("/new/temp", "")
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
	})

	t.Run("overrides GOCACHE when specified", func(t *testing.T) {
		t.Setenv("GOCACHE", "/old/cache")
		env := probeEnv("/new/temp", "/new/cache")
		gocacheCount := 0
		for _, kv := range env {
			switch kv {
			case "GOCACHE=/new/cache":
				gocacheCount++
			case "GOCACHE=/old/cache":
				t.Errorf("old GOCACHE was not removed")
			}
		}
		assert.Equal(t, 1, gocacheCount)
	})
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

func TestExtractDirFlag(t *testing.T) {
	tests := []struct {
		name         string
		flags        []string
		wantDirFlags []string
		wantOther    []string
	}{
		{
			name:         "empty",
			flags:        nil,
			wantDirFlags: nil,
			wantOther:    nil,
		},
		{
			name:         "no dir flags",
			flags:        []string{"-v", "-tags=foo", "-x"},
			wantDirFlags: nil,
			wantOther:    []string{"-v", "-tags=foo", "-x"},
		},
		{
			name:         "separated -C",
			flags:        []string{"-v", "-C", "/tmp/dir", "-x"},
			wantDirFlags: []string{"-C", "/tmp/dir"},
			wantOther:    []string{"-v", "-x"},
		},
		{
			name:         "separated --C",
			flags:        []string{"--C", "/tmp/dir", "-v"},
			wantDirFlags: []string{"--C", "/tmp/dir"},
			wantOther:    []string{"-v"},
		},
		{
			name:         "joined -C=",
			flags:        []string{"-tags=test", "-C=/path/with spaces/dir", "-v"},
			wantDirFlags: []string{"-C=/path/with spaces/dir"},
			wantOther:    []string{"-tags=test", "-v"},
		},
		{
			name:         "joined --C=",
			flags:        []string{"--C=/path/dir", "-v"},
			wantDirFlags: []string{"--C=/path/dir"},
			wantOther:    []string{"-v"},
		},
		{
			name:         "multiple -C flags",
			flags:        []string{"-C", "dir1", "-v", "-C=dir2"},
			wantDirFlags: []string{"-C", "dir1", "-C=dir2"},
			wantOther:    []string{"-v"},
		},
		{
			name:         "trailing -C without arg",
			flags:        []string{"-v", "-C"},
			wantDirFlags: []string{"-C"},
			wantOther:    []string{"-v"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDir, gotOther := extractDirFlag(tt.flags)
			assert.Equal(t, tt.wantDirFlags, gotDir)
			assert.Equal(t, tt.wantOther, gotOther)
		})
	}
}

func TestBuildProbeArgs(t *testing.T) {
	t.Run("no dir flags places build and extra flags before others", func(t *testing.T) {
		args := buildProbeArgs([]string{"-v", "-tags=foo"}, []string{"-work"}, "example.com/target")
		assert.Equal(t, []string{"build", "-work", "-v", "-tags=foo", "example.com/target"}, args)
	})

	t.Run("-C flag placed immediately after build", func(t *testing.T) {
		args := buildProbeArgs(
			[]string{"-v", "-C", "/some/dir", "-tags=foo"},
			[]string{"-work", "-o", "out.a"},
			"example.com/target",
		)
		assert.Equal(
			t,
			[]string{"build", "-C", "/some/dir", "-work", "-o", "out.a", "-v", "-tags=foo", "example.com/target"},
			args,
		)
	})

	t.Run("-C= flag placed immediately after build", func(t *testing.T) {
		args := buildProbeArgs([]string{"-C=/some/dir", "-v"}, []string{"-work"}, "example.com/target")
		assert.Equal(t, []string{"build", "-C=/some/dir", "-work", "-v", "example.com/target"}, args)
	})
}

func TestResolveProbeTarget(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	probeDir := filepath.Join(tempDir, util.BuildTempDir, "probe-123")

	t.Run("converts internal probe dir to relative path", func(t *testing.T) {
		got := resolveProbeTarget(nil, probeDir)
		expected := "." + string(filepath.Separator) + filepath.Join(util.BuildTempDir, "probe-123")
		assert.Equal(t, expected, got)
	})

	t.Run("resolves probe target with -C directory flag", func(t *testing.T) {
		subDir := filepath.Join(tempDir, "sub")
		require.NoError(t, os.MkdirAll(subDir, 0o755))
		got := resolveProbeTarget([]string{"-C", subDir}, probeDir)
		expected := filepath.Join("..", util.BuildTempDir, "probe-123")
		assert.Equal(t, expected, got)
	})

	t.Run("resolves probe target with symlinked working directory", func(t *testing.T) {
		realDir := filepath.Join(tempDir, "real_mod")
		symDir := filepath.Join(tempDir, "sym_mod")
		require.NoError(t, os.MkdirAll(realDir, 0o755))
		require.NoError(t, os.Symlink(realDir, symDir))

		t.Setenv(util.EnvOtelcWorkDir, symDir)
		realProbeDir := filepath.Join(realDir, util.BuildTempDir, "probe-456")
		got := resolveProbeTarget(nil, realProbeDir)
		expected := "." + string(filepath.Separator) + filepath.Join(util.BuildTempDir, "probe-456")
		assert.Equal(t, expected, got)
	})

	t.Run("keeps external probe dir unchanged", func(t *testing.T) {
		extDir := filepath.Join(t.TempDir(), "external", "probe")
		got := resolveProbeTarget(nil, extDir)
		assert.Equal(t, extDir, got)
	})
}

func TestDirectProbeBuild_TransitiveDependencies(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	ctx := t.Context()
	modDir := filepath.Join(tempDir, "testmod")
	secretDir := filepath.Join(modDir, "internal", "secret")
	helperDir := filepath.Join(secretDir, "helper")
	require.NoError(t, os.MkdirAll(helperDir, 0o755))

	require.NoError(
		t,
		os.WriteFile(filepath.Join(modDir, "go.mod"), []byte("module example.com/testmod\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(helperDir, "helper.go"),
			[]byte("package helper\nfunc Value() string { return \"helper_value\" }\n"),
			0o644,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(secretDir, "secret.go"),
			[]byte(
				"package secret\nimport \"example.com/testmod/internal/secret/helper\"\nfunc Secret() string { return helper.Value() }\n",
			),
			0o644,
		),
	)

	t.Setenv(util.EnvOtelcWorkDir, modDir)

	// Cold probe build
	cCold, lCold, errCold := runDirectProbeBuild(ctx, "", "example.com/testmod/internal/secret", nil, "cfg1")
	require.NoError(t, errCold)
	assert.NotEmpty(t, cCold["example.com/testmod/internal/secret"])
	assert.NotEmpty(t, lCold["example.com/testmod/internal/secret"])
	assert.NotEmpty(
		t,
		cCold["example.com/testmod/internal/secret/helper"],
		"transitive dependency must be collected in compileArchives",
	)
	assert.NotEmpty(
		t,
		lCold["example.com/testmod/internal/secret/helper"],
		"transitive dependency must be collected in linkArchives",
	)

	// Warm probe build (host GOCACHE warm): must still resolve both internal/secret and its transitive helper!
	cWarm, lWarm, errWarm := runDirectProbeBuild(ctx, "", "example.com/testmod/internal/secret", nil, "cfg2")
	require.NoError(t, errWarm)
	assert.NotEmpty(t, cWarm["example.com/testmod/internal/secret"])
	assert.NotEmpty(t, lWarm["example.com/testmod/internal/secret"])
	assert.NotEmpty(
		t,
		cWarm["example.com/testmod/internal/secret/helper"],
		"transitive dependency must be preserved during warm build",
	)
	assert.NotEmpty(
		t,
		lWarm["example.com/testmod/internal/secret/helper"],
		"transitive dependency must be preserved in linkArchives during warm build",
	)
}

func TestDirectProbeBuild_LeafPackage(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	CleanupProbeWorkDirs()

	ctx := t.Context()
	modDir := filepath.Join(tempDir, "leafmod")
	leafDir := filepath.Join(modDir, "internal", "leaf")
	require.NoError(t, os.MkdirAll(leafDir, 0o755))

	require.NoError(
		t,
		os.WriteFile(filepath.Join(modDir, "go.mod"), []byte("module example.com/leafmod\n\ngo 1.25.0\n"), 0o644),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(leafDir, "leaf.go"),
			[]byte("package leaf\n// zero imports\nfunc Zero() int { return 0 }\n"),
			0o644,
		),
	)

	t.Setenv(util.EnvOtelcWorkDir, modDir)

	c, l, err := runDirectProbeBuild(ctx, "", "example.com/leafmod/internal/leaf", nil, "leafcfg")
	require.NoError(t, err, "leaf package with 0 imports must succeed without 'no importcfg found' error")
	assert.NotEmpty(t, c["example.com/leafmod/internal/leaf"])
	assert.NotEmpty(t, l["example.com/leafmod/internal/leaf"])
}

func TestDirectProbeBuild_InvalidTarget(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)
	ctx := t.Context()

	_, _, err := runDirectProbeBuild(ctx, "", "nonexistent.example.com/package", nil, "invalidcfg")
	require.Error(t, err)
}

func TestResolveRegularPackages(t *testing.T) {
	ctx := t.Context()

	t.Run("empty slice returns empty maps", func(t *testing.T) {
		c, l, failed := resolveRegularPackages(ctx, "", nil, nil)
		assert.Empty(t, c)
		assert.Empty(t, l)
		assert.Empty(t, failed)
	})

	t.Run("single invalid package records error in failed map", func(t *testing.T) {
		c, l, failed := resolveRegularPackages(ctx, "", []string{"nonexistent.invalid/pkg"}, nil)
		assert.Empty(t, c)
		assert.Empty(t, l)
		require.Error(t, failed["nonexistent.invalid/pkg"])
	})

	t.Run("resolves standard library package", func(t *testing.T) {
		c, l, failed := resolveRegularPackages(ctx, "", []string{"fmt"}, nil)
		assert.Empty(t, failed)
		assert.NotEmpty(t, c["fmt"])
		assert.NotEmpty(t, l["fmt"])
	})

	t.Run("partial batch failure falls back to individual probes", func(t *testing.T) {
		c, l, failed := resolveRegularPackages(ctx, "", []string{"fmt", "nonexistent.invalid/pkg"}, nil)
		assert.NotEmpty(t, c["fmt"])
		assert.NotEmpty(t, l["fmt"])
		require.Error(t, failed["nonexistent.invalid/pkg"])
		assert.Empty(t, c["nonexistent.invalid/pkg"])
	})
}

func TestResolvePackageArchives_Go128Simulated(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)
	CleanupProbeWorkDirs()

	require.NoError(
		t,
		os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte("module example.com/simtest\n\ngo 1.25.0\n"), 0o644),
	)
	privDir := filepath.Join(tempDir, "internal", "priv")
	require.NoError(t, os.MkdirAll(privDir, 0o755))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(privDir, "priv.go"), []byte("package priv\nfunc V() int { return 42 }\n"), 0o644),
	)

	ctx := t.Context()

	// Locate host go binary
	realGo, err := exec.LookPath("go")
	require.NoError(t, err)

	// Create a mock Go 1.28 toolchain directory structure:
	// <root>/bin/go
	// <root>/pkg/tool/<GOOS>_<GOARCH>/compile
	// <root>/pkg/tool/<GOOS>_<GOARCH>/export (triggers hasExportTool == true)
	mockRoot := filepath.Join(tempDir, "mock_go128")
	binDir := filepath.Join(mockRoot, "bin")
	toolDir := filepath.Join(mockRoot, "pkg", "tool", fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH))
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.MkdirAll(toolDir, 0o755))

	mockGo := filepath.Join(binDir, "go"+filepath.Ext(realGo))
	if symlinkErr := os.Symlink(realGo, mockGo); symlinkErr != nil {
		data, readErr := os.ReadFile(realGo)
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(mockGo, data, 0o755))
	}

	compileTool := filepath.Join(toolDir, "compile"+filepath.Ext(realGo))
	exportTool := filepath.Join(toolDir, "export"+filepath.Ext(realGo))
	require.NoError(t, os.WriteFile(compileTool, []byte("mock-compile"), 0o755))
	require.NoError(t, os.WriteFile(exportTool, []byte("mock-export"), 0o755))

	require.True(t, hasExportTool(compileTool), "compileTool must satisfy hasExportTool")

	// Call resolvePackageArchives with both regular and internal packages
	compile1, link1, failed1, err1 := resolvePackageArchives(
		ctx,
		compileTool,
		[]string{"fmt", "example.com/simtest/internal/priv"},
		nil,
	)
	require.NoError(t, err1)
	assert.Empty(t, failed1)
	assert.NotEmpty(t, compile1["fmt"])
	assert.NotEmpty(t, link1["fmt"])
	assert.NotEmpty(t, compile1["example.com/simtest/internal/priv"])
	assert.NotEmpty(t, link1["example.com/simtest/internal/priv"])

	// Second call should hit in-memory cache directly
	compile2, link2, failed2, err2 := resolvePackageArchives(
		ctx,
		compileTool,
		[]string{"fmt", "example.com/simtest/internal/priv"},
		nil,
	)
	require.NoError(t, err2)
	assert.Empty(t, failed2)
	assert.Equal(t, compile1["fmt"], compile2["fmt"])
	assert.Equal(t, link1["fmt"], link2["fmt"])

	// Clear in-memory cache and verify it loads from on-disk JSON cache
	archiveCacheMu.Lock()
	inMemoryCache = make(map[string]*resolvedArchivesCache)
	archiveCacheMu.Unlock()

	compile3, link3, failed3, err3 := resolvePackageArchives(
		ctx,
		compileTool,
		[]string{"fmt", "example.com/simtest/internal/priv"},
		nil,
	)
	require.NoError(t, err3)
	assert.Empty(t, failed3)
	assert.Equal(t, compile1["fmt"], compile3["fmt"])
	assert.Equal(t, link1["fmt"], link3["fmt"])

	// Complete failure scenario returns first error
	_, _, failedFail, errFail := resolvePackageArchives(ctx, compileTool, []string{"nonexistent.invalid/pkg"}, nil)
	require.Error(t, errFail)
	require.Error(t, failedFail["nonexistent.invalid/pkg"])
}

func TestRunProbeBuild_SymlinkedWorkDir(t *testing.T) {
	tempDir := t.TempDir()
	realDir := filepath.Join(tempDir, "real_pkg")
	symDir := filepath.Join(tempDir, "sym_pkg")
	require.NoError(t, os.MkdirAll(realDir, 0o755))
	require.NoError(t, os.Symlink(realDir, symDir))

	require.NoError(
		t,
		os.WriteFile(filepath.Join(realDir, "go.mod"), []byte("module example.com/sympkg\n\ngo 1.25.0\n"), 0o644),
	)

	// Set OTELC_WORK_DIR to symlinked path (reproducing macOS /var -> /private/var divergence)
	t.Setenv(util.EnvOtelcWorkDir, symDir)
	CleanupProbeWorkDirs()

	ctx := t.Context()
	c, l, err := runProbeBuild(ctx, "", []string{"fmt"}, nil)
	require.NoError(t, err, "runProbeBuild must succeed when working directory contains symlinks")
	assert.NotEmpty(t, c["fmt"])
	assert.NotEmpty(t, l["fmt"])
}
