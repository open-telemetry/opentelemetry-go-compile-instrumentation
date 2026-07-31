// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"context"
	"encoding/json"
	"os"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
	"golang.org/x/tools/go/packages"
)

// ruleGoPathResolver resolves import paths referenced by rules to absolute
// filesystem paths, caching lookups across the whole matched rule set.
type ruleGoPathResolver struct {
	ctx        context.Context
	moduleDirs map[string]bool
	cache      map[string]string
}

func newRuleGoPathResolver(ctx context.Context, moduleDirs map[string]bool) *ruleGoPathResolver {
	return &ruleGoPathResolver{ctx: ctx, moduleDirs: moduleDirs, cache: make(map[string]string)}
}

func (r *ruleGoPathResolver) resolve(goPath string) (string, error) {
	if dir, ok := r.cache[goPath]; ok {
		return dir, nil
	}

	var lastErr error
	for moduleDir := range r.moduleDirs {
		pkgs, err := packages.Load(&packages.Config{
			Mode:    packages.NeedFiles,
			Context: r.ctx,
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

		r.cache[goPath] = pkgs[0].Dir
		return pkgs[0].Dir, nil
	}

	return "", ex.Wrapf(lastErr, "failed to resolve import path %q", goPath)
}

// resolveRuleSet resolves and stores ResolvedPath for every file, function, and
// call rule in ruleset that references an import path. Call rules only resolve
// when Path is set, since it is optional for them.
func (r *ruleGoPathResolver) resolveRuleSet(ruleset *rule.InstRuleSet) error {
	for _, fileRule := range ruleset.FileRules {
		dir, err := r.resolve(fileRule.Path)
		if err != nil {
			return err
		}
		fileRule.ResolvedPath = dir
	}

	for _, funcRule := range ruleset.AllFuncRules() {
		dir, err := r.resolve(funcRule.Path)
		if err != nil {
			return err
		}
		funcRule.ResolvedPath = dir
	}

	for _, callRule := range ruleset.AllCallRules() {
		if callRule.Path == "" {
			continue
		}
		dir, err := r.resolve(callRule.Path)
		if err != nil {
			return err
		}
		callRule.ResolvedPath = dir
	}

	return nil
}

// resolveRulePaths resolves the import paths referenced by function, file, and
// call rules to absolute filesystem paths.
//
// This must be done during the setup phase because the instrument phase no longer
// has enough context to resolve import paths (module directories). The resolved paths
// are embedded into the rules and consumed directly during instrumentation.
func resolveRulePaths(ctx context.Context, matched []*rule.InstRuleSet, moduleDirs map[string]bool) error {
	resolver := newRuleGoPathResolver(ctx, moduleDirs)
	for _, ruleset := range matched {
		if err := resolver.resolveRuleSet(ruleset); err != nil {
			return err
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
