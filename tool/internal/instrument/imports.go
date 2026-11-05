// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"
	"slices"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/imports"
)

// updateImportConfigForFile ensures all imports in the given file's AST are present in the importcfg.
// This is used when adding a new file (e.g., via file rules) that has its own imports which may
// not be in the target package's importcfg.
func (ip *instrumentPhase) updateImportConfigForFile(ctx context.Context, root *dst.File, ruleName string) error {
	paths := imports.CollectImportPaths(root)
	if len(paths) == 0 {
		return nil
	}
	if err := ip.updateImportConfig(ctx, paths); err != nil {
		return ex.Wrapf(err, "updating import config for file imports in %s", ruleName)
	}
	return nil
}

// addRuleImports processes imports for a rule and updates the import config.
//
// A file may already import a rule's path under a different alias. The rule's
// alias is then added as a second import, so injected code keeps the alias the
// rule wrote. Dot imports are the exception: the file must dot-import the path
// itself, because injected unqualified identifiers cannot resolve otherwise.
func (ip *instrumentPhase) addRuleImports(
	ctx context.Context,
	root *dst.File,
	ruleImports map[string]string,
	ruleName string,
) error {
	if len(ruleImports) == 0 {
		return nil
	}

	resolution := imports.FindNew(ctx, root, ruleImports)
	for ruleAlias, importPath := range ruleImports {
		if ruleAlias != "." {
			continue
		}
		if aliases, pathExists := resolution.ExistingAliasSets[importPath]; pathExists &&
			!slices.Contains(aliases, ".") {
			return ex.Newf(
				"%s: dot-import conflict for %q - file imports the path with aliases %v but rule requires dot-import; "+
					"injected unqualified identifiers will not resolve; either update the file to use dot-import or adjust the rule",
				ruleName, importPath, aliases)
		}
	}

	if len(resolution.NewImports) == 0 {
		return nil
	}
	if err := imports.AddToFile(ctx, root, resolution.NewImports); err != nil {
		return ex.Wrapf(err, "adding imports for %s", ruleName)
	}
	if err := ip.updateImportConfig(ctx, resolution.NewImports); err != nil {
		return ex.Wrapf(err, "updating import config for %s", ruleName)
	}
	return nil
}

// usedRuleImports returns the subset of ruleImports whose alias is actually
// referenced somewhere in root. It must be called after the rule's generated
// code has already been spliced into root.
//
// Blank ("_") and dot (".") aliases are always kept.
func usedRuleImports(root *dst.File, ruleImports map[string]string) map[string]string {
	if len(ruleImports) == 0 {
		return nil
	}

	used := make(map[string]string, len(ruleImports))
	unresolved := make(map[string]string, len(ruleImports))
	for alias, path := range ruleImports {
		if alias == "_" || alias == "." {
			used[alias] = path
		} else {
			unresolved[alias] = path
		}
	}
	if len(unresolved) == 0 {
		return used
	}

	dst.Inspect(root, func(node dst.Node) bool {
		sel, ok := node.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if ok {
			if path, found := unresolved[ident.Name]; found {
				used[ident.Name] = path
			}
		}
		return true
	})
	return used
}
