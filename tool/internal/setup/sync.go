// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"os"
	"path"
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

func runModTidy(ctx context.Context) error {
	return util.RunCmd(ctx, "go", "mod", "tidy")
}

func addReplace(modfile *modfile.File, path, rpath string) (bool, error) {
	hasReplace := false
	for _, r := range modfile.Replace {
		if r.Old.Path == path {
			hasReplace = true
			break
		}
	}
	if !hasReplace {
		err := modfile.AddReplace(path, "", rpath, "")
		if err != nil {
			return false, ex.Wrapf(err, "failed to add replace directive")
		}
		return true, nil
	}
	return false, nil
}

// discoverParentModules finds parent modules that need replace directives
func discoverParentModules(modulePath string) map[string]string {
	parentModules := make(map[string]string)
	pathParts := strings.Split(strings.TrimPrefix(modulePath, util.OtelRoot+"/"), "/")
	if len(pathParts) <= 1 {
		return parentModules
	}

	// Check for parent instrumentation modules
	for i := len(pathParts) - 1; i > 0; i-- {
		parentPath := util.OtelRoot + "/" + path.Join(pathParts[:i]...)
		parentLocalPath := filepath.Join(util.GetBuildTempDir(), filepath.Join(pathParts[:i]...))
		// Check if parent directory has a go.mod file
		parentGoMod := filepath.Join(parentLocalPath, "go.mod")
		if _, statErr := os.Stat(parentGoMod); statErr == nil {
			parentModules[parentPath] = parentLocalPath
		}
	}

	// Also check for shared module (common dependency for instrumentation)
	sharedPath := util.OtelRoot + "/pkg/instrumentation/shared"
	sharedLocalPath := filepath.Join(util.GetBuildTempDir(), "pkg/instrumentation/shared")
	sharedGoMod := filepath.Join(sharedLocalPath, "go.mod")
	if _, statErr := os.Stat(sharedGoMod); statErr == nil {
		parentModules[sharedPath] = sharedLocalPath
	}

	return parentModules
}

// addModuleReplaces adds replace directives for the given modules
func (sp *SetupPhase) addModuleReplaces(modfile *modfile.File, modules map[string]string) (bool, error) {
	changed := false
	for oldPath, newPath := range modules {
		added, addErr := addReplace(modfile, oldPath, newPath)
		if addErr != nil {
			return false, addErr
		}
		if added {
			sp.Info("Replace parent dependency", "old", oldPath, "new", newPath)
			changed = true
		}
	}
	return changed, nil
}

func (sp *SetupPhase) syncDeps(ctx context.Context, matched []*rule.InstRuleSet) error {
	rules := make([]*rule.InstFuncRule, 0)
	for _, m := range matched {
		funcRules := m.GetFuncRules()
		rules = append(rules, funcRules...)
	}
	if len(rules) == 0 {
		return nil
	}

	// In a matching rule, such as InstFuncRule, the hook code is defined in a
	// separate module. Since this module is local, we need to add a replace
	// directive in go.mod to point the module name to its local path.
	const goModFile = "go.mod"
	modfile, err := parseGoMod(goModFile)
	if err != nil {
		return err
	}
	changed := false
	// Track parent modules that need replace directives
	allParentModules := make(map[string]string)

	// Add matched dependencies to go.mod
	for _, m := range rules {
		util.Assert(strings.HasPrefix(m.Path, util.OtelRoot), "sanity check")
		// TODO: Since we haven't published the instrumentation packages yet,
		// we need to add the replace directive to the local path.
		// Once the instrumentation packages are published, we can remove this.
		oldPath := m.Path
		newPath := strings.TrimPrefix(oldPath, util.OtelRoot)
		newPath = filepath.Join(util.GetBuildTempDir(), newPath)
		added, addErr := addReplace(modfile, oldPath, newPath)
		if addErr != nil {
			return addErr
		}
		changed = changed || added
		if added {
			sp.Info("Replace dependency", "old", oldPath, "new", newPath)
		}

		// Check if this module has parent modules that also need replace directives
		parentModules := discoverParentModules(m.Path)
		for k, v := range parentModules {
			allParentModules[k] = v
		}
	}

	// Add replace directives for parent modules
	parentChanged, err := sp.addModuleReplaces(modfile, allParentModules)
	if err != nil {
		return err
	}
	changed = changed || parentChanged

	// TODO: Since we haven't published the pkg packages yet, we need to add the
	// replace directive to the local path. Once the pkg packages are published,
	// we can remove this.
	// Add special pkg module to go.mod
	oldPath := util.OtelRoot + "/pkg"
	newPath := filepath.Join(util.GetBuildTempDir(), unzippedPkgDir)
	added, addErr := addReplace(modfile, oldPath, newPath)
	if addErr != nil {
		return addErr
	}
	changed = changed || added
	if changed {
		sp.Info("Replace dependency", "old", oldPath, "new", newPath)
	}
	if changed {
		err = writeGoMod(goModFile, modfile)
		if err != nil {
			return err
		}
		err = runModTidy(ctx)
		if err != nil {
			return err
		}
		sp.keepForDebug(goModFile)
	}
	return nil
}
