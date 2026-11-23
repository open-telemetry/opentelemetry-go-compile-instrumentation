// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	goversion "go/version"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

func parseGoMod(gomod string) (*modfile.File, error) {
	data, err := os.ReadFile(gomod)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to read go.mod file")
	}
	modFile, err := modfile.Parse(gomod, data, nil)
	if err != nil {
		return nil, ex.Wrapf(err, "failed to parse go.mod file")
	}
	return modFile, nil
}

func writeGoMod(gomod string, modfile *modfile.File) error {
	data, err := modfile.Format()
	if err != nil {
		return ex.Wrapf(err, "failed to format go.mod file")
	}
	const perm = 0o644
	return util.WriteFileAtomic(gomod, data, perm)
}

func runModTidy(ctx context.Context, moduleDir string) error {
	return util.RunCmdInDir(ctx, moduleDir, "go", "mod", "tidy")
}

// versionSnapshot records go directive and direct dep versions before tidy.
type versionSnapshot struct {
	goVersion string
	deps      map[string]string
}

func snapshotVersion(mf *modfile.File) versionSnapshot {
	snap := versionSnapshot{
		deps: make(map[string]string),
	}
	if mf.Go != nil {
		snap.goVersion = mf.Go.Version
	}
	for _, req := range mf.Require {
		if !req.Indirect {
			snap.deps[req.Mod.Path] = req.Mod.Version
		}
	}
	return snap
}

func warnVersion(ctx context.Context, goModPath string, before versionSnapshot) error {
	logger := util.LoggerFromContext(ctx)

	after, err := parseGoMod(goModPath)
	if err != nil {
		return ex.Wrapf(err, "unable to check for version bumps after go mod tidy")
	}

	// The notices go to stderr, like the go command's own diagnostics. Stdout
	// carries the output of the build, such as the JSON events of go test -json.
	//
	// Go directives use Go toolchain syntax ("1.21"), not module semver.
	if after.Go != nil && before.goVersion != "" {
		if goversion.Compare("go"+after.Go.Version, "go"+before.goVersion) > 0 {
			_, _ = fmt.Fprintf(os.Stderr, "Bumped go version (%s -> %s)\n", before.goVersion, after.Go.Version)
			logger.WarnContext(ctx, "bumped go version", "old", before.goVersion, "new", after.Go.Version)
		}
	}

	for _, req := range after.Require {
		if oldVer, tracked := before.deps[req.Mod.Path]; tracked {
			if semver.Compare(req.Mod.Version, oldVer) > 0 {
				_, _ = fmt.Fprintf(os.Stderr, "Bumped dependency %s (%s -> %s)\n",
					req.Mod.Path, oldVer, req.Mod.Version)
				logger.WarnContext(ctx, "bumped dependency",
					"module", req.Mod.Path,
					"old", oldVer,
					"new", req.Mod.Version)
			}
		}
	}
	return nil
}

func syncDeps(ctx context.Context, moduleDir string) error {
	goModFile := filepath.Join(moduleDir, goModFileName)
	modfile, err := parseGoMod(goModFile)
	if err != nil {
		return err
	}

	before := snapshotVersion(modfile)

	// Run "go mod tidy" to sync the dependencies. We may have added new dependencies
	// to otel.instrumentation.go that need to be pinned.
	if err = runModTidy(ctx, moduleDir); err != nil {
		return ex.Wrapf(err, "running go mod tidy in %s", moduleDir)
	}

	// Compare after tidy because MVS may raise existing consumer versions.
	if err = warnVersion(ctx, goModFile, before); err != nil {
		return err
	}

	// Keep the file for debugging
	keepForDebug(ctx, goModFile, "")

	return nil
}
