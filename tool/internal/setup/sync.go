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
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
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
	err = os.WriteFile(gomod, data, 0o644) //nolint:gosec // 0644 is ok
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

func (sp *SetupPhase) warnVersion(goModPath string, before versionSnapshot) error {
	after, err := parseGoMod(goModPath)
	if err != nil {
		return ex.Wrapf(err, "unable to check for version bumps after go mod tidy")
	}

	// Go directives use Go toolchain syntax ("1.21"), not module semver.
	if after.Go != nil && before.goVersion != "" {
		if goversion.Compare("go"+after.Go.Version, "go"+before.goVersion) > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "Bumped go version (%s -> %s)\n", before.goVersion, after.Go.Version)
			sp.Warn("bumped go version", "old", before.goVersion, "new", after.Go.Version)
		}
	}

	for _, req := range after.Require {
		if oldVer, tracked := before.deps[req.Mod.Path]; tracked {
			if semver.Compare(req.Mod.Version, oldVer) > 0 {
				_, _ = fmt.Fprintf(
					os.Stdout,
					"Bumped dependency %s (%s -> %s)\n",
					req.Mod.Path,
					oldVer,
					req.Mod.Version,
				)
				sp.Warn("bumped dependency",
					"module", req.Mod.Path,
					"old", oldVer,
					"new", req.Mod.Version)
			}
		}
	}
	return nil
}

func (sp *SetupPhase) syncDeps(ctx context.Context, matched []*rule.InstRuleSet, moduleDir string) error {
	funcRules := []*rule.InstFuncRule{}
	fileRules := []*rule.InstFileRule{}
	for _, m := range matched {
		funcRules = append(funcRules, m.AllFuncRules()...)
		fileRules = append(fileRules, m.FileRules...)
	}
	if len(funcRules) == 0 && len(fileRules) == 0 {
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
	replaces := make(map[string]string)
	for _, m := range funcRules {
		if path, isEmbedded := strings.CutPrefix(m.ModulePath, util.OtelcInstRoot+"/"); isEmbedded {
			replaces[m.ModulePath] = filepath.Join(util.GetBuildTempDir(), unzippedInstDir, path)
		}
	}
	for _, m := range fileRules {
		if path, isEmbedded := strings.CutPrefix(m.ModulePath, util.OtelcInstRoot+"/"); isEmbedded {
			replaces[m.ModulePath] = filepath.Join(util.GetBuildTempDir(), unzippedInstDir, path)
		}
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

	// Okay, now add all the replace directives to go.mod
	changed := false
	for oldPath, newPath := range replaces {
		added, addErr := addReplace(modfile, oldPath, newPath)
		if addErr != nil {
			return addErr
		}
		changed = changed || added
		if added {
			sp.Info("Replace dependency", "old", oldPath, "new", newPath)
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
		err = sp.warnVersion(goModFile, before)
		if err != nil {
			return err
		}
		sp.keepForDebug(goModFile)
	}
	return nil
}
