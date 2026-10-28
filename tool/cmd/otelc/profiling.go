// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

//nolint:gochecknoglobals // Profiling session is shared between Before/After hooks.
var activeSession *profileSession

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
	inside, err := isSubPath(profilePath, buildTemp)
	if err != nil {
		return ctx, ex.Wrapf(err, "verify profile-path %q is outside the build temp directory", profilePath)
	}
	if inside {
		return ctx, ex.Newf(
			"profile-path %q must not be inside the build temp directory %q",
			profilePath, buildTemp,
		)
	}

	// Parse and validate profile types before touching the filesystem.
	joined := strings.Join(profiles, ",")
	types, err := parseProfileTypes(joined)
	if err != nil {
		return ctx, err
	}

	// Set env vars BEFORE starting profiling so that os.Environ() in
	// BuildWithToolexec (setup.go) propagates them to child processes.
	if setErr := os.Setenv(envProfilePath, profilePath); setErr != nil {
		return ctx, ex.Wrapf(setErr, "set %s", envProfilePath)
	}
	if setErr := os.Setenv(envEnabledProfiles, joined); setErr != nil {
		return ctx, ex.Wrapf(setErr, "set %s", envEnabledProfiles)
	}

	session, err := startProfileSession(profilePath, types)
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

	stopErr := activeSession.stop()
	activeSession = nil

	if !cmd.Bool("profile-summary") {
		return stopErr
	}

	// Summary mode: merge all per-process files into one file per type.
	// Use the absolute path stored in env (set by initProfiling).
	profileDir := os.Getenv(envProfilePath)
	if profileDir == "" {
		return ex.New("profile path not set")
	}

	rawTypes := os.Getenv(envEnabledProfiles)
	types, parseErr := parseProfileTypes(rawTypes)
	if parseErr != nil || len(types) == 0 {
		return ex.Join(stopErr, parseErr)
	}

	logger.InfoContext(ctx, "merging profile files", "dir", profileDir)
	mergeErr := mergeProfiles(ctx, profileDir, types)
	return ex.Join(stopErr, mergeErr)
}

// isSubPath reports whether target is equal to base or located inside base.
//
// Resolves symbolic links in each path's deepest existing ancestor before
// comparing, so two different spellings of the same real directory agree.
// On macOS, /var is a firmlink to /private/var: os.Getwd() (which feeds
// base here, via GetBuildTempDir) returns the unresolved /var form, but an
// absolute --profile-path the user passes can spell the same real directory
// through /private/var. Comparing the raw strings (or even filepath.Rel on
// them, as this used to do) would then miss a profile-path that genuinely
// is inside base, letting it through only for Cleanup to silently delete
// the profile output afterward with no error at any point.
//
// target usually doesn't exist on disk yet at this point (its directory is
// created later, by profile.Start's MkdirAll), so this can't just call
// filepath.EvalSymlinks(target) directly: EvalSymlinks requires every
// component to exist. resolveExistingAncestor instead resolves the deepest
// existing ancestor and rejoins the nonexistent suffix unresolved, which is
// exactly what it would resolve to once created, since a path that doesn't
// exist yet can't itself be a symlink.
//
// Returns an error if either path's deepest existing ancestor can't be
// resolved for a reason other than not existing (e.g. a permission error, or
// a path component that isn't a directory). Reporting that error lets the
// caller fail with a clear cause instead of this silently treating target as
// outside base when containment couldn't actually be verified.
func isSubPath(target, base string) (bool, error) {
	resolvedTarget, err := resolveExistingAncestor(target)
	if err != nil {
		return false, ex.Wrapf(err, "resolve %q", target)
	}
	resolvedBase, err := resolveExistingAncestor(base)
	if err != nil {
		return false, ex.Wrapf(err, "resolve %q", base)
	}
	rel, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil {
		return false, nil
	}
	return rel == "." || !strings.HasPrefix(rel, ".."), nil
}

// resolveExistingAncestor resolves symbolic links in path's deepest existing
// ancestor and rejoins the remaining suffix (which may not exist) onto the
// result. For a path that exists in full, this is equivalent to
// filepath.EvalSymlinks(path). It walks up to the filesystem root if no
// prefix of path exists, at which point it returns path unchanged (the root
// itself always exists and is never a symlink).
func resolveExistingAncestor(path string) (string, error) {
	suffix := "."
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			return filepath.Join(resolved, suffix), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			// Reached the root (or ran out of components for a relative
			// path) without finding anything that exists; nothing left to
			// resolve.
			return path, nil
		}
		suffix = filepath.Join(filepath.Base(path), suffix)
		path = parent
	}
}
