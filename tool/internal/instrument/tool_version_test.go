// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otelc/tool/util"
)

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
}

func TestEnableNestedToolexec(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	t.Run("appends to existing GOFLAGS and sets the nested marker", func(t *testing.T) {
		t.Setenv("GOFLAGS", "-mod=mod")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		goflags := os.Getenv("GOFLAGS")
		assert.Contains(t, goflags, "-mod=mod", "existing flags are preserved")
		assert.Contains(t, goflags, "'-toolexec="+exe+" toolexec'", "otelc is added as the toolexec")
		assert.Equal(t, "1", os.Getenv(util.EnvOtelcNestedToolexec))
	})

	t.Run("handles empty GOFLAGS without leading whitespace", func(t *testing.T) {
		t.Setenv("GOFLAGS", "")
		t.Setenv(util.EnvOtelcNestedToolexec, "")

		require.NoError(t, EnableNestedToolexec())

		assert.Equal(t, "'-toolexec="+exe+" toolexec'", os.Getenv("GOFLAGS"))
	})
}

func TestLoadMissingMatchedRules(t *testing.T) {
	// Point the work dir at an empty directory: matched.json does not exist,
	// which is what a bare -toolexec build sees when setup never ran.
	t.Setenv(util.EnvOtelcWorkDir, t.TempDir())

	ip := &InstrumentPhase{logger: slog.Default()}
	_, err := ip.load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "otelc setup")
}

// versionMarkerPattern documents the exact tool-ID shape the go build cache
// keys on, guarding against accidental format drift.
var versionMarkerPattern = regexp.MustCompile(`otelc@[^\s/]+(/[0-9a-f]{16})?`)

func TestToolVersionLineMatchesCachePattern(t *testing.T) {
	assert.Regexp(t, versionMarkerPattern, toolVersionLine("compile version go1.26.5", ""))
	assert.Regexp(t, versionMarkerPattern, toolVersionLine("compile version go1.26.5", "0123456789abcdef"))
}
