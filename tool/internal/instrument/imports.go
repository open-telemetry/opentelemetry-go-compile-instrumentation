// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrument

import (
	"github.com/dave/dst"

	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/ex"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/tool/internal/imports"
)

// updateImportConfigForFile ensures all imports in the given file's AST are present in the importcfg.
// This is used when adding a new file (e.g., via file rules) that has its own imports which may
// not be in the target package's importcfg.
func (ip *InstrumentPhase) updateImportConfigForFile(root *dst.File, ruleName string) error {
	paths := imports.CollectPaths(ip.ctx, root)

	if len(paths) == 0 {
		return nil
	}

	if err := ip.updateImportConfig(paths); err != nil {
		return ex.Wrapf(err, "updating import config for file imports in %s", ruleName)
	}

	return nil
}

// handleRuleImports processes imports for a rule and updates the import config.
// ruleType is used for error messages (e.g., "file rule", "func rule").
//
// This function validates that if a rule expects to use an import with a specific alias,
// and the file already imports the same package with a different explicit alias, an error is returned.
// This prevents silent failures where injected code uses an alias that doesn't exist in the file.
func (ip *InstrumentPhase) handleRuleImports(
	root *dst.File,
	ruleImports map[string]string,
	ruleName, ruleType string,
) error {
	if len(ruleImports) == 0 {
		return nil
	}

	resolution := imports.FindNew(ip.ctx, root, ruleImports)

	// Validate: check for alias mismatches that would break injected code
	for ruleAlias, importPath := range ruleImports {
		if ruleAlias == "." {
			// Dot-import conflict check
			if existingAlias, pathExists := resolution.ExistingAliases[importPath]; pathExists {
				if existingAlias != "." {
					return ex.Newf(
						"%s %s: dot-import conflict for %q - "+
							"file imports the path with alias %q but rule requires dot-import; "+
							"injected unqualified identifiers will not resolve; "+
							"either update the file to use dot-import or adjust the rule",
						ruleType, ruleName, importPath, existingAlias)
				}
			}
			continue
		}
		if ruleAlias == "_" {
			continue // Blank imports are permissive
		}

		// Only validate if the file has an explicit alias for this import
		if !resolution.ExplicitAliases[importPath] {
			continue
		}

		if existingAlias, pathExists := resolution.ExistingAliases[importPath]; pathExists {
			if existingAlias != ruleAlias {
				return ex.Newf(
					"%s %s: import alias mismatch for %q - "+
						"file uses alias %q but rule expects %q; "+
						"injected code will fail to compile; "+
						"either update the file's import or adjust the rule's import alias",
					ruleType, ruleName, importPath, existingAlias, ruleAlias)
			}
		}
	}

	if len(resolution.NewImports) == 0 {
		return nil
	}

	// Add import declarations to the AST
	if err := imports.AddToFile(ip.ctx, root, resolution.NewImports); err != nil {
		return ex.Wrapf(err, "adding imports for %s %s", ruleType, ruleName)
	}

	// Update importcfg for the build
	if err := ip.updateImportConfig(resolution.NewImports); err != nil {
		return ex.Wrapf(err, "updating import config for %s %s", ruleType, ruleName)
	}

	return nil
}
