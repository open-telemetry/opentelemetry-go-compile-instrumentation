// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"encoding/json"
	"slices"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

// resolveRulePaths resolves the import paths referenced by function and file rules
// to absolute filesystem paths.
func resolveRulePaths(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs []string) error {
	dirs := normalizeModuleDirs(moduleDirs)

	var pending []string
	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			pending = append(pending, fileRule.Path)
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			pending = append(pending, funcRule.Path)
		}
	}
	slices.Sort(pending)
	pending = slices.Compact(pending)

	resolved := make(map[string]string, len(pending))
	var lastErr error

	// findPackage returns the single package in pkgs matching importPath, nil if
	// none matched (the caller retries it against the next module dir), or an
	// error if importPath ambiguously matched more than one package.
	findPackage := func(pkgs []*packages.Package, importPath string) (*packages.Package, error) {
		var found *packages.Package
		for _, pkg := range pkgs {
			if pkg.PkgPath != importPath || len(pkg.Errors) > 0 || pkg.Dir == "" {
				continue
			}
			if found != nil {
				return nil, ex.Newf("import path %q resolved to multiple packages", importPath)
			}
			found = pkg
		}
		return found, nil
	}

	for _, moduleDir := range dirs {
		if len(pending) == 0 {
			break
		}

		// otelc changed go.mod before this load, so the load uses the -mod
		// flag of the build (see modBuildFlags).
		pkgs, loadErr := packages.Load(&packages.Config{
			Mode:       packages.NeedName | packages.NeedFiles,
			Context:    ctx,
			Dir:        moduleDir,
			BuildFlags: modBuildFlags(ctx),
		}, pending...)
		if loadErr != nil {
			lastErr = loadErr
			continue
		}

		next := pending[:0]
		for _, p := range pending {
			pkg, err := findPackage(pkgs, p)
			switch {
			case err != nil:
				return err
			case pkg == nil:
				next = append(next, p)
			default:
				resolved[p] = rulePackageDir(pkg)
			}
		}
		pending = next
	}

	if len(pending) > 0 {
		return ex.Wrapf(lastErr, "failed to resolve import path %q", pending[0])
	}

	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			fileRule.ResolvedPath = resolved[fileRule.Path]
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			funcRule.ResolvedPath = resolved[funcRule.Path]
		}
	}

	return nil
}

// rulePackageDir returns the directory of the hook package pkg. In vendor
// mode, the go command gives the directory in vendor/. "go mod vendor" does not
// copy all the files (for example the //go:build ignore files that file rules
// add), so for an otelc package the directory of the extracted bundle is used.
func rulePackageDir(pkg *packages.Package) string {
	if isInVendorDir(pkg.Dir) {
		if dir, ok := embeddedSourceDir(pkg.PkgPath); ok {
			return dir
		}
	}
	return pkg.Dir
}

// store stores the matched rules to the file.
// It's the pair of the InstrumentPhase.load.
// Rule paths must already be resolved via resolveRulePaths.
func (sp *setupPhase) store(matched []*rule.InstRuleSet) error {
	bs, err := json.Marshal(matched)
	if err != nil {
		return ex.Wrapf(err, "failed to marshal rules to JSON")
	}

	f := util.GetMatchedRuleFile()
	err = util.WriteFileAtomic(f, bs)
	if err != nil {
		return ex.Wrapf(err, "failed to write matched rules to file %s", f)
	}
	sp.Info("Stored matched sets", "path", f)

	if err = sp.storeResolvedNames(); err != nil {
		return err
	}
	return sp.storePGOProfile()
}

// storePGOProfile records the PGO profile the build applies, for the nested
// builds toolexec runs. It always writes the file, empty when there is no
// profile, so a profile recorded by an earlier setup never applies to a later
// build.
func (sp *setupPhase) storePGOProfile() error {
	profile, ambiguous := pgoProfile(sp.pgo, sp.buildPackages)
	if ambiguous {
		sp.Warn("cannot pick a PGO profile for nested builds: main packages have different default.pgo files")
	}
	f := util.GetPGOProfileFile()
	if err := util.WriteFileAtomic(f, []byte(profile)); err != nil {
		return ex.Wrapf(err, "failed to write PGO profile to file %s", f)
	}
	sp.Info("Stored PGO profile", "path", f, "profile", profile)
	return nil
}

// storeResolvedNames persists the import name table built during
// setup, so later toolexec processes need not guess a package name
// from its import path.
func (sp *setupPhase) storeResolvedNames() error {
	bs, err := json.Marshal(sp.resolvedNames)
	if err != nil {
		return ex.Wrapf(err, "failed to marshal import names to JSON")
	}

	f := util.GetImportNamesFile()
	if writeErr := util.WriteFileAtomic(f, bs); writeErr != nil {
		return ex.Wrapf(writeErr, "failed to write import names to file %s", f)
	}
	sp.Info("Stored resolved import names", "path", f, "count", len(sp.resolvedNames))
	return nil
}
