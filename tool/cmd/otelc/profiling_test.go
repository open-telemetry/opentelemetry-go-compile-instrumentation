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
