// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	goversion "go/version"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

func addReplace(modfile *modfile.File, oldPath, newPath string) (bool, error) {
	hasReplace := false
	for _, r := range modfile.Replace {
		if r.Old.Path == oldPath {
			hasReplace = true
			break
		}
	}
	if !hasReplace {
		err := modfile.AddReplace(oldPath, "", newPath, "")
		if err != nil {
			return false, ex.Wrapf(err, "failed to add replace directive")
		}
		return true, nil
	}
	return false, nil
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

	// Go directives use Go toolchain syntax ("1.21"), not module semver.
	if after.Go != nil && before.goVersion != "" {
		if goversion.Compare("go"+after.Go.Version, "go"+before.goVersion) > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "Bumped go version (%s -> %s)\n", before.goVersion, after.Go.Version)
			logger.WarnContext(ctx, "bumped go version", "old", before.goVersion, "new", after.Go.Version)
		}
	}

	for _, req := range after.Require {
		if oldVer, tracked := before.deps[req.Mod.Path]; tracked {
			if semver.Compare(req.Mod.Version, oldVer) > 0 {
				_, _ = fmt.Fprintf(os.Stdout, "Bumped dependency %s (%s -> %s)\n",
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

// discoverNestedModuleReplaces walks dir for go.mod files nested inside it
// (excluding dir's own go.mod), returning a map of module path to directory.
// This picks up local helper modules that live inside an instrumentation
// module's tree but carry no otelc.yaml of their own — e.g. a package shared
// between several versioned copies of one instrumentation — so they never
// match as an instrumentation import on their own. Without a replace
// directive for them here, a downstream consumer's "go mod tidy" tries to
// fetch them from a real module proxy and fails.
//
// The walk can pick up modules the consumer never actually requires — e.g.
// a matched v1 directory that nests v2/v3 copies inside it, or, with the
// parent-directory walk in syncDeps, sibling instrumentations under a
// shared parent. That's intentional and harmless: "go mod tidy" ignores a
// replace directive for a module that isn't in the build list.
func discoverNestedModuleReplaces(dir string) (map[string]string, error) {
	nested := make(map[string]string)
	topGoMod := filepath.Join(dir, "go.mod")

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return ex.Wrap(err)
		}
		if d.IsDir() {
			name := d.Name()
			if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" || filepath.Clean(path) == filepath.Clean(topGoMod) {
			return nil
		}

		modFile, parseErr := parseGoMod(path)
		if parseErr != nil {
			return ex.Wrapf(parseErr, "loading %s", path)
		}

		nested[modFile.Module.Mod.Path] = filepath.Dir(path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return nested, nil
}

func syncDeps(ctx context.Context, modPaths map[string]bool, moduleDir string) error {
	if len(modPaths) == 0 {
		return nil
	}

	logger := util.LoggerFromContext(ctx)

	goModFile := filepath.Join(moduleDir, "go.mod")
	modfile, err := parseGoMod(goModFile)
	if err != nil {
		return err
	}

	before := snapshotVersion(modfile)

	// Add replace directives for modules imported to otel.instrumentation.go
	replaces := make(map[string]string, len(modPaths))
	for m := range modPaths {
		if path, isEmbedded := strings.CutPrefix(m, util.OtelcInstRoot+"/"); isEmbedded {
			replaces[m] = filepath.Join(util.GetBuildTempDir(), unzippedInstDir, path)
		}
	}

	// Some matched instrumentation modules have their own nested local
	// modules (see discoverNestedModuleReplaces) — either inside the matched
	// module's own directory, or as a sibling shared by several versioned
	// copies of one instrumentation (e.g. openai-go's v1/v2/v3 all sharing
	// openai-go/internal/streaming, which sits under v1's directory, a
	// sibling of v2/ and v3/ rather than a descendant of either). Walking
	// each matched directory's parent in addition to the directory itself
	// catches both shapes. The parent walk can also re-discover sibling
	// version directories or unrelated instrumentations under a shared
	// parent; a replace directive for a module the consumer doesn't
	// actually require is harmless, since "go mod tidy" ignores it.
	// Capacity hint: each matched dir contributes itself plus its parent.
	const dirsPerMatch = 2
	walkDirs := make(map[string]bool, len(replaces)*dirsPerMatch)
	for _, dir := range replaces {
		walkDirs[dir] = true
		walkDirs[filepath.Dir(dir)] = true
	}
	for dir := range walkDirs {
		nested, nestedErr := discoverNestedModuleReplaces(dir)
		if nestedErr != nil {
			return ex.Wrapf(nestedErr, "discovering nested modules under %s", dir)
		}
		maps.Copy(replaces, nested)
	}

	// Add replace directive for special pkg module
	// TODO: Since we haven't published the instrumentation packages yet,
	// we need to add the replace directive to the local path.
	// Once the instrumentation packages are published, we can remove this.
	replaces[util.OtelcPkgRoot] = filepath.Join(util.GetBuildTempDir(), unzippedPkgDir)

	// Add replace directive for special runtime module
	// runtime module initializes the OpenTelemetry SDK. It is required by all
	// hook code to be present.
	replaces[util.OtelcPkgRoot+"/runtime"] = filepath.Join(util.GetBuildTempDir(), unzippedPkgDir, "runtime")

	// Add replace directive for instrumentation module
	// instrumentation module contains shared semconv packages.
	replaces[util.OtelcInstRoot] = filepath.Join(util.GetBuildTempDir(), unzippedInstDir)

	// Okay, now add all the replace directives to go.mod in deterministic sorted order
	changed := false
	for _, oldPath := range slices.Sorted(maps.Keys(replaces)) {
		newPath := replaces[oldPath]
		added, addErr := addReplace(modfile, oldPath, newPath)
		if addErr != nil {
			return addErr
		}
		changed = changed || added
		if added {
			logger.InfoContext(ctx, "Replace dependency", "old", oldPath, "new", newPath)
		}
	}

	// Check if any replace directive is added, if so, write to go.mod
	if changed {
		if err = writeGoMod(goModFile, modfile); err != nil {
			return ex.Wrapf(err, "writing updated go.mod at %s", goModFile)
		}
	}

	// Run "go mod tidy" to sync the dependencies regardless of
	// the replace directives. We may have added new dependencies
	// to otel.instrumentation.go that need to be pinned.
	if err = runModTidy(ctx, moduleDir); err != nil {
		return ex.Wrapf(err, "running go mod tidy in %s", moduleDir)
	}

	// Compare after tidy because MVS may raise existing consumer versions.
	if err = warnVersion(ctx, goModFile, before); err != nil {
		return err
	}

	// Keep the file for debugging
	keepForDebug(ctx, goModFile)

	return nil
}
