// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/profile"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

//nolint:gochecknoglobals // Shared between Before/After hooks; cli/v3 After cannot receive modified context.
var activeSession *profile.Session

// initProfiling starts profiling if --profile-path and --profile flags are set.
// It calls os.Setenv so child processes spawned via -toolexec inherit the
// profiling configuration through os.Environ() in BuildWithToolexec.
func initProfiling(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	profilePath := cmd.String("profile-path")
	profiles := cmd.StringSlice("profile")

	// Allow --profile-path without --profile: useful for scripts that conditionally
	// append --profile flags. No profiles requested → no-op.
	if len(profiles) == 0 {
		return ctx, nil
	}

	// --profile requires --profile-path.
	if profilePath == "" {
		return ctx, ex.Newf("--profile-path is required when --profile is set")
	}

	// Resolve to absolute path so child processes with a different CWD can find it.
	var err error
	profilePath, err = filepath.Abs(profilePath)
	if err != nil {
		return ctx, ex.Wrapf(err, "resolve profile path")
	}

	// Guard against placing profiles inside .otelc-build/ which Cleanup removes.
	buildTemp := util.GetBuildTempDir()
	if strings.HasPrefix(profilePath, buildTemp) {
		return ctx, ex.Newf(
			"profile-path %q must not be inside the build temp directory %q",
			profilePath, buildTemp,
		)
	}

	// Parse and validate profile types before touching the filesystem.
	joined := strings.Join(profiles, ",")
	types, err := profile.ParseTypes(joined)
	if err != nil {
		return ctx, err
	}

	// Set env vars BEFORE starting profiling so that os.Environ() in
	// BuildWithToolexec (setup.go) propagates them to child processes.
	if setErr := os.Setenv(profile.EnvProfilePath, profilePath); setErr != nil {
		return ctx, ex.Wrapf(setErr, "set %s", profile.EnvProfilePath)
	}
	if setErr := os.Setenv(profile.EnvEnabledProfiles, joined); setErr != nil {
		return ctx, ex.Wrapf(setErr, "set %s", profile.EnvEnabledProfiles)
	}

	session, err := profile.Start(profilePath, types)
	if err != nil {
		return ctx, err
	}

	activeSession = session

	logger := util.LoggerFromContext(ctx)
	logger.InfoContext(ctx, "profiling started", "path", profilePath, "profiles", joined)

	return ctx, nil
}

// stopProfiling stops the active profiling session. When --profile-summary is set,
// it merges all per-process profile files into a single file per type.
// Called from the root command's After hook; runs even when the build fails.
func stopProfiling(ctx context.Context, cmd *cli.Command) error {
	if activeSession == nil {
		return nil
	}

	logger := util.LoggerFromContext(ctx)
	logger.InfoContext(ctx, "stopping profiling")

	stopErr := activeSession.Stop()
	activeSession = nil

	if !cmd.Bool("profile-summary") {
		return stopErr
	}

	// Summary mode: merge all per-process files into one file per type.
	// Use the absolute path stored in env (set by initProfiling).
	profileDir := os.Getenv(profile.EnvProfilePath)
	if profileDir == "" {
		return ex.New("profile path not set")
	}

	rawTypes := os.Getenv(profile.EnvEnabledProfiles)
	types, parseErr := profile.ParseTypes(rawTypes)
	if parseErr != nil || len(types) == 0 {
		return ex.Join(stopErr, parseErr)
	}

	logger.InfoContext(ctx, "merging profile files", "dir", profileDir)
	mergeErr := profile.Merge(ctx, profileDir, types)
	return ex.Join(stopErr, mergeErr)
}
