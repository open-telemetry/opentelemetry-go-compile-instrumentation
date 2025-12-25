// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

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

type replaceDirective struct {
	oldPath    string
	oldVersion string
	newPath    string
	newVersion string
}

func addReplace(modfile *modfile.File, replace *replaceDirective) (bool, error) {
	hasReplace := false
	for _, r := range modfile.Replace {
		if r.Old.Path == replace.oldPath {
			hasReplace = true
			break
		}
	}
	if !hasReplace {
		err := modfile.AddReplace(replace.oldPath, replace.oldVersion,
			replace.newPath, replace.newVersion)
		if err != nil {
			return false, ex.Wrapf(err, "failed to add replace directive")
		}
		return true, nil
	}
	return false, nil
}

func (sp *SetupPhase) syncDeps(ctx context.Context, matched []*rule.InstRuleSet, moduleDir string) error {
	rules := make([]*rule.InstFuncRule, 0)
	for _, m := range matched {
		funcRules := m.GetFuncRules()
		rules = append(rules, funcRules...)
	}
	if len(rules) == 0 {
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
	replaces := make([]*replaceDirective, 0)
	for _, m := range rules {
		util.Assert(strings.HasPrefix(m.Path, util.OtelRoot), "sanity check")
		oldPath := m.Path
		newPath := strings.TrimPrefix(oldPath, util.OtelRoot)
		newPath = filepath.Join(util.GetBuildTempDir(), newPath)
		replaces = append(replaces, &replaceDirective{
			oldPath:    oldPath,
			oldVersion: "",
			newPath:    newPath,
			newVersion: "",
		})
	}

	// Add replace directive for special pkg module
	// TODO: Since we haven't published the instrumentation packages yet,
	// we need to add the replace directive to the local path.
	// Once the instrumentation packages are published, we can remove this.
	replaces = append(replaces, &replaceDirective{
		oldPath:    util.OtelRoot + "/pkg",
		oldVersion: "",
		newPath:    filepath.Join(util.GetBuildTempDir(), unzippedPkgDir),
		newVersion: "",
	})

	// Add replace directive for special shared module
	// shared module initializes the OpenTelemetry SDK. It is required by all
	// hook code to be present.
	replaces = append(replaces, &replaceDirective{
		oldPath:    util.OtelRoot + "/pkg/instrumentation/shared",
		oldVersion: "",
		newPath:    filepath.Join(util.GetBuildTempDir(), "pkg/instrumentation/shared"),
		newVersion: "",
	})

	// Okay, now add all the replace directives to go.mod
	changed := false
	for _, replace := range replaces {
		added, addErr := addReplace(modfile, replace)
		if addErr != nil {
			return addErr
		}
		changed = changed || added
		if changed {
			sp.Info("Replace dependency", "old", replace.oldPath, "new", replace.newPath)
		}
	}

	// Check if any replace directive is added, if so, write go.mod and run mod tidy
	// to sync the changes to go.mod for build system to use.
	if changed {
		err = writeGoMod(goModFile, modfile)
		if err != nil {
			return err
		}
		err = runModTidy(ctx, moduleDir)
		if err != nil {
			return err
		}
		sp.keepForDebug(goModFile)
	}
	return nil
}
