// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"encoding/json"
	"os"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/rule"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/util"
	"golang.org/x/tools/go/packages"
)

// resolveRulePaths resolves the import paths referenced by function and file rules
// to absolute filesystem paths.
//
// This must be done during the setup phase because the instrument phase no longer
// has enough context to resolve import paths (module directories). The resolved paths
// are embedded into the rules and consumed directly during instrumentation.
func resolveRulePaths(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs map[string]bool) error {
	cache := make(map[string]string)

	resolve := func(goPath string) (string, error) {
		if dir, ok := cache[goPath]; ok {
			return dir, nil
		}

		var lastErr error
		for moduleDir := range moduleDirs {
			pkgs, err := packages.Load(&packages.Config{
				Mode:    packages.NeedFiles,
				Context: ctx,
				Dir:     moduleDir,
			}, goPath)
			if err != nil {
				lastErr = err
				continue
			}
			if len(pkgs) == 0 {
				lastErr = ex.New("no packages found")
				continue
			}
			if len(pkgs[0].Errors) > 0 {
				lastErr = pkgs[0].Errors[0]
				continue
			}
			if len(pkgs) > 1 {
				return "", ex.Newf("import path %q resolved to %d packages", goPath, len(pkgs))
			}

			cache[goPath] = pkgs[0].Dir
			return pkgs[0].Dir, nil
		}

		return "", ex.Wrapf(lastErr, "failed to resolve import path %q", goPath)
	}

	for _, ruleset := range matched {
		for _, fileRule := range ruleset.FileRules {
			dir, err := resolve(fileRule.Path)
			if err != nil {
				return err
			}
			fileRule.ResolvedPath = dir
		}

		for _, funcRule := range ruleset.AllFuncRules() {
			dir, err := resolve(funcRule.Path)
			if err != nil {
				return err
			}
			funcRule.ResolvedPath = dir
		}
	}

	return nil
}

// store stores the matched rules to the file
// It's the pair of the InstrumentPhase.load
func (sp *SetupPhase) store(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs map[string]bool) error {
	if err := resolveRulePaths(ctx, matched, moduleDirs); err != nil {
		return ex.Wrapf(err, "resolving rule paths")
	}

	f := util.GetMatchedRuleFile()
	file, err := os.Create(f)
	if err != nil {
		return ex.Wrapf(err, "failed to create file %s", f)
	}
	defer file.Close()

	bs, err := json.Marshal(matched)
	if err != nil {
		return ex.Wrapf(err, "failed to marshal rules to JSON")
	}

	_, err = file.Write(bs)
	if err != nil {
		return ex.Wrapf(err, "failed to write JSON to file %s", f)
	}
	sp.Info("Stored matched sets", "path", f)
	return nil
}
