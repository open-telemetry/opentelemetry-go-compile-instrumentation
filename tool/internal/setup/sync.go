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

func localModulePath(modulePath string) string {
	relPath := strings.TrimPrefix(modulePath, util.OtelcRoot)
	relPath = strings.TrimPrefix(relPath, "/")
	return filepath.Join(util.GetBuildTempDir(), filepath.FromSlash(relPath))
}

func localModuleReplaces(modulePaths ...string) ([]*replaceDirective, error) {
	replaces := make([]*replaceDirective, 0, len(modulePaths))
	seen := make(map[string]bool, len(modulePaths))
	queue := append([]string(nil), modulePaths...)

	for len(queue) > 0 {
		modulePath := queue[0]
		queue = queue[1:]

		if seen[modulePath] || !strings.HasPrefix(modulePath, util.OtelcRoot+"/pkg") {
			continue
		}
		seen[modulePath] = true

		replaces = append(replaces, &replaceDirective{
			oldPath: modulePath,
			newPath: localModulePath(modulePath),
		})

		goModFile := filepath.Join(localModulePath(modulePath), "go.mod")
		if _, err := os.Stat(goModFile); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, ex.Wrapf(err, "checking local module go.mod at %s", goModFile)
		}

		modfile, err := parseGoMod(goModFile)
		if err != nil {
			return nil, err
		}
		for _, req := range modfile.Require {
			if strings.HasPrefix(req.Mod.Path, util.OtelcRoot+"/pkg") {
				queue = append(queue, req.Mod.Path)
			}
		}
	}

	return replaces, nil
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
	rules := make([]*rule.InstFuncRule, 0, len(matched))
	for _, m := range matched {
		funcRules := m.AllFuncRules()
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
	modulePaths := make([]string, 0, len(rules)+2)
	for _, m := range rules {
		util.Assert(strings.HasPrefix(m.Path, util.OtelcRoot), "sanity check")
		modulePaths = append(modulePaths, m.Path)
	}

	// Add replace directive for special pkg module
	// TODO: Since we haven't published the instrumentation packages yet,
	// we need to add the replace directive to the local path.
	// Once the instrumentation packages are published, we can remove this.
	modulePaths = append(modulePaths, util.OtelcRoot+"/pkg")

	// Add replace directive for special shared module
	// shared module initializes the OpenTelemetry SDK. It is required by all
	// hook code to be present.
	modulePaths = append(modulePaths, util.OtelcRoot+"/pkg/instrumentation/shared")

	replaces, err := localModuleReplaces(modulePaths...)
	if err != nil {
		return err
	}

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
			return ex.Wrapf(err, "writing updated go.mod at %s", goModFile)
		}
		err = runModTidy(ctx, moduleDir)
		if err != nil {
			return ex.Wrapf(err, "running go mod tidy in %s", moduleDir)
		}
		sp.keepForDebug(goModFile)
	}
	return nil
}
