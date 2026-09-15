// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/internal/profile"
	"go.opentelemetry.io/otelc/tool/util"
)

// profilingFlags returns the subset of root flags the profiling hooks read.
func profilingFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "profile-path"},
		&cli.StringSliceFlag{Name: "profile"},
		&cli.BoolFlag{Name: "profile-summary"},
	}
}

// runInitProfiling parses args and invokes initProfiling as the Before hook,
// returning any error it produced.
func runInitProfiling(t *testing.T, args ...string) error {
	t.Helper()
	app := &cli.Command{
		Flags:  profilingFlags(),
		Before: initProfiling,
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	return app.Run(context.Background(), append([]string{"otelc"}, args...))
}

func TestInitProfiling(t *testing.T) {
	t.Run("no profile flag is a no-op", func(t *testing.T) {
		activeSession = nil
		require.NoError(t, runInitProfiling(t))
		assert.Nil(t, activeSession)
	})

	t.Run("profile without profile-path errors", func(t *testing.T) {
		activeSession = nil
		err := runInitProfiling(t, "--profile", "cpu")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "profile-path")
	})

	t.Run("invalid profile type errors", func(t *testing.T) {
		activeSession = nil
		dir := t.TempDir()
		err := runInitProfiling(t, "--profile", "bogus", "--profile-path", dir)
		require.Error(t, err)
	})

	t.Run("profile-path inside build temp errors", func(t *testing.T) {
		activeSession = nil
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		inside := filepath.Join(util.GetBuildTempDir(), "prof")
		err := runInitProfiling(t, "--profile", "cpu", "--profile-path", inside)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build temp")
	})

	t.Run("profile-path equal to build temp errors", func(t *testing.T) {
		activeSession = nil
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		buildTemp := util.GetBuildTempDir()
		err := runInitProfiling(t, "--profile", "cpu", "--profile-path", buildTemp)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build temp")
	})

	t.Run("profile-path sharing prefix with build temp is allowed", func(t *testing.T) {
		activeSession = nil
		workDir := t.TempDir()
		t.Setenv(util.EnvOtelcWorkDir, workDir)
		t.Setenv(profile.EnvProfilePath, "")
		t.Setenv(profile.EnvEnabledProfiles, "")
		sibling := util.GetBuildTempDir() + "-profiles"

		require.NoError(t, runInitProfiling(t, "--profile", "cpu", "--profile-path", sibling))
		require.NotNil(t, activeSession)

		require.NoError(t, activeSession.Stop())
		activeSession = nil
	})

	t.Run("valid profile starts a session and sets env", func(t *testing.T) {
		activeSession = nil
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		t.Setenv(profile.EnvProfilePath, "")
		t.Setenv(profile.EnvEnabledProfiles, "")
		profDir := filepath.Join(t.TempDir(), "profiles")

		require.NoError(t, runInitProfiling(t, "--profile", "cpu", "--profile-path", profDir))
		require.NotNil(t, activeSession)

		// Clean up the session started above so it does not leak into other tests.
		require.NoError(t, activeSession.Stop())
		activeSession = nil
	})
}

func TestStopProfiling(t *testing.T) {
	t.Run("no active session is a no-op", func(t *testing.T) {
		activeSession = nil
		app := &cli.Command{
			Flags:  profilingFlags(),
			Action: stopProfiling,
		}
		require.NoError(t, app.Run(context.Background(), []string{"otelc"}))
	})

	t.Run("stops an active session without summary", func(t *testing.T) {
		activeSession = nil
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		profDir := filepath.Join(t.TempDir(), "profiles")
		require.NoError(t, runInitProfiling(t, "--profile", "cpu", "--profile-path", profDir))
		require.NotNil(t, activeSession)

		app := &cli.Command{
			Flags:  profilingFlags(),
			Action: stopProfiling,
		}
		require.NoError(t, app.Run(context.Background(), []string{"otelc"}))
		assert.Nil(t, activeSession, "session must be cleared after stop")
	})

	t.Run("stops and merges with summary", func(t *testing.T) {
		activeSession = nil
		t.Setenv(util.EnvOtelcWorkDir, t.TempDir())
		profDir := filepath.Join(t.TempDir(), "profiles")
		require.NoError(t, runInitProfiling(t, "--profile", "cpu", "--profile-path", profDir))
		require.NotNil(t, activeSession)

		app := &cli.Command{
			Flags:  profilingFlags(),
			Action: stopProfiling,
		}
		require.NoError(t, app.Run(context.Background(), []string{"otelc", "--profile-summary"}))
		assert.Nil(t, activeSession)
	})
}

func TestIsSubPath(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "path", "to", ".otelc-build")

	tests := []struct {
		name     string
		target   string
		base     string
		expected bool
	}{
		{
			name:     "exact base path",
			target:   base,
			base:     base,
			expected: true,
		},
		{
			name:     "child path inside base",
			target:   filepath.Join(base, "sub", "dir"),
			base:     base,
			expected: true,
		},
		{
			name:     "sibling path with shared prefix",
			target:   base + "-profiles",
			base:     base,
			expected: false,
		},
		{
			name:     "nested path under sibling with shared prefix",
			target:   filepath.Join(base+"-profiles", "sub", "dir"),
			base:     base,
			expected: false,
		},
		{
			name:     "target with trailing separator equal to base",
			target:   base + string(filepath.Separator),
			base:     base,
			expected: true,
		},
		{
			name:     "parent directory",
			target:   filepath.Dir(base),
			base:     base,
			expected: false,
		},
		{
			name:     "completely different path",
			target:   filepath.Join(string(filepath.Separator), "other", "dir"),
			base:     base,
			expected: false,
		},
		{
			name:     "incompatible relative path error path",
			target:   "relative/path",
			base:     base,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isSubPath(tt.target, tt.base))
		})
	}
}
