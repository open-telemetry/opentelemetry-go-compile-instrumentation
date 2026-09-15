// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsRuleFile(t *testing.T) {
	tests := map[string]bool{
		"otelc.yaml":        true,
		"otelc.yml":         true,
		"client.otelc.yaml": true,
		"server.otelc.yml":  true,
		"rules.yaml":        false,
		"rules.yml":         false,
		"otelc.client.yaml": false,
		"otelc":             false,
		"otelc.txt":         false,
		"otelc.yaml.bak":    false,
	}

	for filename, expected := range tests {
		t.Run(filename, func(t *testing.T) {
			assert.Equal(t, expected, IsRuleFile(filename))
		})
	}
}

func TestVersionInRange(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		versionRange   string
		expectedResult bool
	}{
		{
			name:           "no version range specified - always matches",
			version:        "v1.5.0",
			versionRange:   "",
			expectedResult: true,
		},
		{
			name:           "version exactly at start of range",
			version:        "v1.0.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "version in middle of range",
			version:        "v1.5.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "version just before end of range",
			version:        "v1.9.9",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "version exactly at end of range - excluded",
			version:        "v2.0.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name:           "version after end of range",
			version:        "v2.1.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name:           "version before start of range",
			version:        "v0.9.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name:           "pre-release version in range",
			version:        "v1.5.0-alpha",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "patch version in range",
			version:        "v1.5.3",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "major version jump",
			version:        "v3.0.0",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name:           "zero major version",
			version:        "v0.5.0",
			versionRange:   "v0.1.0,v1.0.0",
			expectedResult: true,
		},
		{
			name:           "narrow version range",
			version:        "v1.2.3",
			versionRange:   "v1.2.0,v1.3.0",
			expectedResult: true,
		},
		{
			name:           "version with build metadata",
			version:        "v1.5.0+build123",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: true,
		},
		{
			name:           "minimal version only - good",
			version:        "v1.2.3",
			versionRange:   "v1.2.3",
			expectedResult: true,
		},
		{
			name:           "minimal version only - bad",
			version:        "v1.2.3",
			versionRange:   "v1.2.4",
			expectedResult: false,
		},
		{
			name:           "empty version with range - not in range",
			version:        "",
			versionRange:   "v1.0.0",
			expectedResult: false,
		},
		{
			name:           "empty version with bounded range - not in range",
			version:        "",
			versionRange:   "v1.0.0,v2.0.0",
			expectedResult: false,
		},
		{
			name:           "empty version with empty range - always matches",
			version:        "",
			versionRange:   "",
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VersionInRange(tt.version, tt.versionRange)
			if result != tt.expectedResult {
				t.Errorf("VersionInRange() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestValidateVersionRange(t *testing.T) {
	tests := []struct {
		name         string
		versionRange string
		wantErr      bool
	}{
		{name: "empty range", versionRange: "", wantErr: false},
		{name: "single lower bound", versionRange: "v1.2.3", wantErr: false},
		{name: "bounded range", versionRange: "v1.0.0,v2.0.0", wantErr: false},
		{name: "trailing comma", versionRange: "v1.0.0,", wantErr: true},
		{name: "missing lower bound", versionRange: ",v2.0.0", wantErr: true},
		{name: "extra comma", versionRange: "v1.0.0,v2.0.0,v3.0.0", wantErr: true},
		{name: "invalid single version", versionRange: "not-a-version", wantErr: true},
		{name: "invalid start bound", versionRange: "not-a-version,v2.0.0", wantErr: true},
		{name: "invalid end bound", versionRange: "v1.0.0,not-a-version", wantErr: true},
		{name: "reversed range", versionRange: "v2.0.0,v1.0.0", wantErr: true},
		{name: "equal bounds", versionRange: "v1.0.0,v1.0.0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVersionRange(tt.versionRange)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateVersionRange(%q) = nil, want error", tt.versionRange)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateVersionRange(%q) = %v, want nil", tt.versionRange, err)
			}
		})
	}
}

func TestGetOtelcWorkDir(t *testing.T) {
	t.Run("uses OTELC_WORK_DIR when set", func(t *testing.T) {
		t.Setenv(EnvOtelcWorkDir, "/tmp/otelc-work")
		assert.Equal(t, "/tmp/otelc-work", GetOtelcWorkDir())
	})
	t.Run("falls back to cwd when unset", func(t *testing.T) {
		t.Setenv(EnvOtelcWorkDir, "")
		cwd, err := os.Getwd()
		require.NoError(t, err)
		assert.Equal(t, cwd, GetOtelcWorkDir())
	})
}

func TestGetBuildTempPaths(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv(EnvOtelcWorkDir, workDir)

	assert.Equal(t, filepath.Join(workDir, BuildTempDir), GetBuildTempDir())
	assert.Equal(t, filepath.Join(workDir, BuildTempDir, "foo.txt"), GetBuildTemp("foo.txt"))
	assert.Equal(t, filepath.Join(workDir, BuildTempDir, "matched.json"), GetMatchedRuleFile())
	assert.Equal(t, filepath.Join(workDir, BuildTempDir, "added_imports.*.json"), GetAddedImportsPattern())

	// The per-process import file embeds the current PID.
	want := filepath.Join(workDir, BuildTempDir, fmt.Sprintf("added_imports.%d.json", os.Getpid()))
	assert.Equal(t, want, GetAddedImportsFileForProcess())
}

func TestEncodeBuildFlagsEmpty(t *testing.T) {
	assert.Empty(t, EncodeBuildFlags(nil))
	assert.Empty(t, EncodeBuildFlags([]string{}))
}

func TestEncodeBuildFlagsRoundTrip(t *testing.T) {
	flags := []string{"-tags", "foo bar", "-race"}
	encoded := EncodeBuildFlags(flags)
	assert.NotEmpty(t, encoded)
	// The encoding must be valid JSON that preserves spaces in tokens.
	assert.True(t, strings.HasPrefix(encoded, "["))
	assert.Contains(t, encoded, "foo bar")
}

func TestDiscoverWorkDir(t *testing.T) {
	newDir := func(t *testing.T, parts ...string) string {
		t.Helper()
		dir := filepath.Join(parts...)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	touch := func(t *testing.T, path string) {
		t.Helper()
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("found in current directory", func(t *testing.T) {
		module := newDir(t, t.TempDir(), "module")
		newDir(t, module, BuildTempDir)
		touch(t, filepath.Join(module, "go.mod"))

		if got := DiscoverWorkDir(module); got != module {
			t.Errorf("DiscoverWorkDir() = %q, want %q", got, module)
		}
	})

	t.Run("found walking up from a subdirectory", func(t *testing.T) {
		module := newDir(t, t.TempDir(), "module")
		newDir(t, module, BuildTempDir)
		touch(t, filepath.Join(module, "go.mod"))
		sub := newDir(t, module, "internal", "app")

		if got := DiscoverWorkDir(sub); got != module {
			t.Errorf("DiscoverWorkDir() = %q, want %q", got, module)
		}
	})

	t.Run("stops at go.mod when no work dir exists", func(t *testing.T) {
		// .otelc-build exists above the module boundary and must not be found.
		root := t.TempDir()
		newDir(t, root, BuildTempDir)
		module := newDir(t, root, "module")
		touch(t, filepath.Join(module, "go.mod"))
		sub := newDir(t, module, "internal")

		if got := DiscoverWorkDir(sub); got != "" {
			t.Errorf("DiscoverWorkDir() = %q, want empty", got)
		}
	})
}
