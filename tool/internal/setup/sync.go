// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"fmt"
	goversion "go/version"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
)

const localReplaceVersion = "v0.0.0-00010101000000-000000000000"

func localVersionForPath(modulePath string) string {
	idx := strings.LastIndex(modulePath, "/v")
	if idx != -1 && idx < len(modulePath)-2 {
		suffix := modulePath[idx+2:]
		allDigits := true
		for _, r := range suffix {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && len(suffix) > 0 {
			return "v" + suffix + ".0.0-00010101000000-000000000000"
		}
	}
	return localReplaceVersion
}

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
	err = util.WriteFileAtomic(gomod, data, perm)
	if err != nil {
		return ex.Wrapf(err, "failed to write go.mod file")
	}
	return nil
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

func addRequire(modfile *modfile.File, modulePath string) (bool, error) {
	hasRequire := false
	for _, req := range modfile.Require {
		if req.Mod.Path == modulePath {
			hasRequire = true
			break
		}
	}
	if !hasRequire {
		version := localVersionForPath(modulePath)
		if err := modfile.AddRequire(modulePath, version); err != nil {
			return false, ex.Wrapf(err, "failed to add require directive")
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

func syncDeps(ctx context.Context, modPaths map[string]bool, moduleDir string) error {
	logger := util.LoggerFromContext(ctx)
	if len(modPaths) == 0 {
		return nil
	}

	// Add replace directives for matched dependencies
	// In a matching rule, such as InstFuncRule, the hook code is defined in a
	// separate module. Since this module is local, we need to add a replace
	// directive in go.mod to point the module name to its local path.
	goModFile := filepath.Join(moduleDir, "go.mod")
	modfile, err := parseGoMod(goModFile)
	if err != nil {
		return err
	}

	before := snapshotVersion(modfile)
	modules := make(map[string]string, len(modPaths))
	for m := range modPaths {
		if path, isEmbeddedPkg := strings.CutPrefix(m, util.OtelcPkgRoot+"/"); isEmbeddedPkg {
			modules[m] = filepath.Join(util.GetBuildTempDir(), unzippedPkgDir, path)
		} else if instPath, isEmbeddedInst := strings.CutPrefix(m, util.OtelcRoot+"/instrumentation/"); isEmbeddedInst {
			modules[m] = filepath.Join(util.GetBuildTempDir(), unzippedInstDir, instPath)
		} else {
			modules[m] = ""
		}
	}

	// Add replace directive for special pkg module
	// TODO: Since we haven't published the instrumentation packages yet,
	// we need to add the replace directive to the local path.
	// Once the instrumentation packages are published, we can remove this.
	modules[util.OtelcPkgRoot] = filepath.Join(util.GetBuildTempDir(), unzippedPkgDir)

	// Add replace directive for special runtime module
	// runtime module initializes the OpenTelemetry SDK. It is required by all
	// hook code to be present.
	modules[util.OtelcPkgRoot+"/runtime"] = filepath.Join(util.GetBuildTempDir(), unzippedPkgDir, "runtime")

	// Add replace directive for instrumentation module
	// instrumentation module contains shared semconv packages.
	modules[util.OtelcRoot+"/instrumentation"] = filepath.Join(util.GetBuildTempDir(), unzippedInstDir)

	// Okay, now add all the replace directives to go.mod
	changed := false
	for oldPath, newPath := range modules {
		// If newPath is empty, it means the module is not embedded and we don't need to add replace directive for it.
		if newPath == "" {
			continue
		}

		required, reqErr := addRequire(modfile, oldPath)
		if reqErr != nil {
			return reqErr
		}
		changed = changed || required
		if required {
			version := localVersionForPath(oldPath)
			logger.InfoContext(ctx, "Require dependency", "module", oldPath, "version", version)
		}

		added, addErr := addReplace(modfile, oldPath, newPath)
		if addErr != nil {
			return addErr
		}
		changed = changed || added
		if added {
			logger.InfoContext(ctx, "Replace dependency", "old", oldPath, "new", newPath)
		}
	}

	// Check if any replace directive is added, if so, write go.mod and run mod tidy
	// to sync the changes to go.mod for build system to use.
	if changed {
		err = writeGoMod(goModFile, modfile)
		if err != nil {
			return ex.Wrapf(err, "writing updated go.mod at %s", goModFile)
		}
		err = runModTidy(ctx, moduleDir)
		if err != nil {
			return ex.Wrapf(err, "running go mod tidy in %s", moduleDir)
		}
		// Compare after tidy because MVS may raise existing consumer versions.
		err = warnVersion(ctx, goModFile, before)
		if err != nil {
			return err
		}

		// Keep the file for debugging
		keepForDebug(ctx, goModFile)
	}
	return nil
}
