// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"os"
	"path/filepath"
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

		linkerArchive := filepath.Join(b002Dir, "_pkg_.a")
		require.NoError(t, os.WriteFile(linkerArchive, []byte("archive"), 0o644))

		b001Cfg := "packagefile example.com/cached=/cache/ab/123-d\n" +
			"packagefile example.com/fresh=" + filepath.Join(exportDir, "b002.a") + "\n"
		require.NoError(t, os.WriteFile(filepath.Join(b001Dir, "importcfg"), []byte(b001Cfg), 0o644))

		b002Cfg := "packagefile example.com/dep=/cache/cd/456-d\n"
		require.NoError(t, os.WriteFile(filepath.Join(b002Dir, "importcfg"), []byte(b002Cfg), 0o644))

		compileArchives, linkArchives, err := collectArchivesFromWorkDir(workDir)
		require.NoError(t, err)

		assert.Equal(t, "/cache/ab/123-d", compileArchives["example.com/cached"])
		assert.Equal(t, filepath.Join(exportDir, "b002.a"), compileArchives["example.com/fresh"])
		assert.Equal(t, "/cache/cd/456-d", compileArchives["example.com/dep"])

		assert.Equal(t, "/cache/ab/123-d", linkArchives["example.com/cached"])
		assert.Equal(t, linkerArchive, linkArchives["example.com/fresh"])
		assert.Equal(t, "/cache/cd/456-d", linkArchives["example.com/dep"])
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

	cache, err := loadArchiveCache()
	require.NoError(t, err)
	assert.Empty(t, cache.Compile)
	assert.Empty(t, cache.Link)

	err = saveArchiveCache(
		map[string]string{"pkg/a": "a.a"},
		map[string]string{"pkg/a": "a_pkg.a"},
	)
	require.NoError(t, err)

	loaded, err := loadArchiveCache()
	require.NoError(t, err)
	assert.Equal(t, "a.a", loaded.Compile["pkg/a"])
	assert.Equal(t, "a_pkg.a", loaded.Link["pkg/a"])

	err = saveArchiveCache(
		map[string]string{"pkg/b": "b.a"},
		map[string]string{"pkg/b": "b_pkg.a"},
	)
	require.NoError(t, err)

	updated, err := loadArchiveCache()
	require.NoError(t, err)
	assert.Equal(t, "a.a", updated.Compile["pkg/a"])
	assert.Equal(t, "b.a", updated.Compile["pkg/b"])
	assert.Equal(t, "a_pkg.a", updated.Link["pkg/a"])
	assert.Equal(t, "b_pkg.a", updated.Link["pkg/b"])
}

func TestRecordAndCleanupProbeWorkDirs(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv(util.EnvOtelcWorkDir, tempDir)

	dir1 := filepath.Join(tempDir, "work1")
	dir2 := filepath.Join(tempDir, "work2")
	require.NoError(t, os.MkdirAll(dir1, 0o755))
	require.NoError(t, os.MkdirAll(dir2, 0o755))

	recordProbeWorkDir(dir1)
	recordProbeWorkDir(dir2)

	assert.True(t, util.PathExists(dir1))
	assert.True(t, util.PathExists(dir2))

	CleanupProbeWorkDirs()

	assert.False(t, util.PathExists(dir1))
	assert.False(t, util.PathExists(dir2))

	trackingFile := filepath.Join(util.GetBuildTempDir(), probeWorkDirsFileName)
	assert.False(t, util.PathExists(trackingFile))
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
