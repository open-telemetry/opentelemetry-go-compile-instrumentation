// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"context"

	"github.com/dave/dst"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/ast"
	"go.opentelemetry.io/otelc/tool/internal/imports"
)

// updateImportConfigForFile ensures all imports in the given file's AST are present in the importcfg.
// This is used when adding a new file (e.g., via file rules) that has its own imports which may
// not be in the target package's importcfg.
func (ip *instrumentPhase) updateImportConfigForFile(ctx context.Context, root *dst.File, ruleName string) error {
	paths := imports.CollectPaths(ctx, root)

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
// This function validates that if a rule expects to use an import with a specific alias,
// and the file already imports the same package with a different alias (whether explicit or
// implicit), an error is returned. This prevents silent failures where injected code uses
// an alias that doesn't exist in the file.
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

	// Validate: check for alias mismatches that would break injected code
	for ruleAlias, importPath := range ruleImports {
		if ruleAlias == "." {
			// Dot-import conflict check
			if existingAlias, pathExists := resolution.ExistingAliases[importPath]; pathExists {
				if existingAlias != "." {
					return ex.Newf(
						"%s: dot-import conflict for %q - "+
							"file imports the path with alias %q but rule requires dot-import; "+
							"injected unqualified identifiers will not resolve; "+
							"either update the file to use dot-import or adjust the rule",
						ruleName, importPath, existingAlias)
				}
			}
			continue
		}
		if ruleAlias == "_" {
			continue // Blank imports are permissive
		}

		// Validate alias matches for all existing imports (both explicit and implicit).
		// When a file already imports a path, we won't add a duplicate, so injected code
		// must use the alias that actually exists in the file.
		if existingAlias, pathExists := resolution.ExistingAliases[importPath]; pathExists {
			if existingAlias != ruleAlias {
				return ex.Newf(
					"%s: import alias mismatch for %q - "+
						"file uses alias %q but rule expects %q; "+
						"injected code will fail to compile; "+
						"either update the file's import or adjust the rule's import alias",
					ruleName, importPath, existingAlias, ruleAlias)
			}
		}
	}

	if len(resolution.NewImports) == 0 {
		return nil
	}

	// Add import declarations to the AST
	if err := imports.AddToFile(ctx, root, resolution.NewImports); err != nil {
		return ex.Wrapf(err, "adding imports for %s", ruleName)
	}

	// Update importcfg for the build
	if err := ip.updateImportConfig(ctx, resolution.NewImports); err != nil {
		return ex.Wrapf(err, "updating import config for %s", ruleName)
	}

	return nil
}

// resolveImportOverrides computes root's current import aliases and the
// per-rule-alias overrides a rule needs when the file already imports one of
// ruleImports' paths under a different alias.
//
//nolint:revive // needed to balance confusing-results and nonamedreturns linters
func (ip *instrumentPhase) resolveImportOverrides(
	root *dst.File,
	ruleImports map[string]string,
) (map[string]string, map[string]string) {
	importAliases := ast.ImportAliasMap(root, ip.importNames)

	existingAliases := make(map[string]string, len(importAliases))
	for alias, path := range importAliases {
		existingAliases[path] = alias
	}
	aliasOverrides := resolveAliasOverrides(ruleImports, existingAliases)
	return importAliases, aliasOverrides
}

// resolveAliasOverrides reports the alias to substitute for each rule
// import already present in the target file under a different alias.
// Substituting the file's alias into generated code, instead of the
// rule's alias, avoids a build failure.
//
// existingAliases must resolve an unaliased import to its real name,
// not a guess. A guessed name can be illegal as a Go identifier.
//
// The dot alias and the blank alias are exempt from substitution.
func resolveAliasOverrides(ruleImports, existingAliases map[string]string) map[string]string {
	var overrides map[string]string
	for ruleAlias, importPath := range ruleImports {
		if ruleAlias == "." || ruleAlias == "_" {
			continue
		}
		existingAlias, ok := existingAliases[importPath]
		if !ok || existingAlias == ruleAlias || existingAlias == "." || existingAlias == "_" {
			continue
		}
		if overrides == nil {
			overrides = make(map[string]string, len(ruleImports))
		}
		overrides[ruleAlias] = existingAlias
	}
	return overrides
}

// replaceQualifierAliases rewrites a freshly generated expression to
// use the file's alias for an import, instead of the rule's alias.
// overrides maps each rule alias to the file's alias.
//
// replaceQualifierAliases only touches the node passed in. The rewrite
// cannot reach unrelated code that shares an identifier name.
func replaceQualifierAliases(node dst.Node, overrides map[string]string) {
	if len(overrides) == 0 {
		return
	}
	dst.Inspect(node, func(n dst.Node) bool {
		sel, ok := n.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*dst.Ident)
		if !ok {
			return true
		}
		if existingAlias, override := overrides[ident.Name]; override {
			ident.Name = existingAlias
		}
		return true
	})
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
	for alias, path := range ruleImports {
		if alias == "_" || alias == "." {
			used[alias] = path
		}
	}

	dst.Inspect(root, func(node dst.Node) bool {
		sel, ok := node.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, identOk := sel.X.(*dst.Ident)
		if !identOk {
			return true
		}
		if path, importOk := ruleImports[ident.Name]; importOk {
			used[ident.Name] = path
		}
		return true
	})

	return used
}
