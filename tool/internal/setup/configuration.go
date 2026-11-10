// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/configuration"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

const instrumentationMetadataFilename = "metadata.yaml"

// selectedConfigurationMetadata is the metadata handoff to application
// configuration validation. Module roots are absolute paths and sorted.
type selectedConfigurationMetadata struct {
	Definitions            []configuration.Entry
	ModulesWithoutMetadata []string
}

// validateSelectedConfigurationMetadata reads module-local metadata.yaml files
// only for instrumentation modules whose rules matched this build. Missing
// metadata is returned so application-configuration validation can decide
// whether it is needed.
func (sp *setupPhase) validateSelectedConfigurationMetadata(
	matched []*rule.InstRuleSet,
) (selectedConfigurationMetadata, error) {
	return sp.validateSelectedConfigurationMetadataWithCatalog(matched, configuration.CatalogDefinitions)
}

func (sp *setupPhase) validateSelectedConfigurationMetadataWithCatalog(
	matched []*rule.InstRuleSet,
	loadCatalog func() (map[string]configuration.Entry, error),
) (selectedConfigurationMetadata, error) {
	sortedModuleRoots := sp.selectedConfigurationModuleRoots(matched)
	result := selectedConfigurationMetadata{}
	var metadataFiles []configuration.Metadata
	for _, moduleRoot := range sortedModuleRoots {
		metadataPath := filepath.Join(moduleRoot, instrumentationMetadataFilename)
		data, err := os.ReadFile(metadataPath)
		if errors.Is(err, os.ErrNotExist) {
			result.ModulesWithoutMetadata = append(result.ModulesWithoutMetadata, moduleRoot)
			continue
		}
		if err != nil {
			return selectedConfigurationMetadata{}, ex.Wrapf(
				err,
				"read selected instrumentation metadata %s",
				metadataPath,
			)
		}
		metadata, err := configuration.ParseMetadata(data, metadataPath)
		if err != nil {
			return selectedConfigurationMetadata{}, err
		}
		metadataFiles = append(metadataFiles, metadata)
	}
	slices.Sort(result.ModulesWithoutMetadata)
	result.ModulesWithoutMetadata = slices.Compact(result.ModulesWithoutMetadata)

	if len(metadataFiles) == 0 {
		sp.Info(
			"Discovered selected instrumentation configuration metadata",
			"modules",
			len(sortedModuleRoots),
			"configurations",
			0,
		)
		return result, nil
	}

	definitions, err := loadCatalog()
	if err != nil {
		return selectedConfigurationMetadata{}, ex.Wrapf(err, "load pinned configuration catalog")
	}
	resolved, err := configuration.ResolveMetadata(metadataFiles, definitions)
	if err != nil {
		return selectedConfigurationMetadata{}, err
	}
	result.Definitions = resolved
	sp.Info(
		"Validated selected instrumentation configuration metadata",
		"modules",
		len(sortedModuleRoots)-len(result.ModulesWithoutMetadata),
		"configurations",
		len(resolved),
	)
	return result, nil
}

func (sp *setupPhase) selectedConfigurationModuleRoots(matched []*rule.InstRuleSet) []string {
	moduleRoots := make(map[string]struct{})
	for _, ruleset := range matched {
		if ruleset == nil {
			continue
		}
		addRuleMapModuleRoots(moduleRoots, sp.ruleModuleRoots, ruleset.RawRules)
		addRuleMapModuleRoots(moduleRoots, sp.ruleModuleRoots, ruleset.FuncRules)
		addRuleMapModuleRoots(moduleRoots, sp.ruleModuleRoots, ruleset.StructRules)
		addRuleMapModuleRoots(moduleRoots, sp.ruleModuleRoots, ruleset.CallRules)
		addRuleMapModuleRoots(moduleRoots, sp.ruleModuleRoots, ruleset.LitRules)
		addRuleMapModuleRoots(moduleRoots, sp.ruleModuleRoots, ruleset.DirectiveRules)
		addRuleMapModuleRoots(moduleRoots, sp.ruleModuleRoots, ruleset.DeclRules)
		addRuleSliceModuleRoots(moduleRoots, sp.ruleModuleRoots, ruleset.FileRules)
	}
	return slices.Sorted(maps.Keys(moduleRoots))
}

func addRuleMapModuleRoots[T rule.InstRule](
	moduleRoots map[string]struct{},
	ruleModuleRoots map[rule.InstRule]string,
	rulesByFile map[string][]T,
) {
	for _, rules := range rulesByFile {
		addRuleSliceModuleRoots(moduleRoots, ruleModuleRoots, rules)
	}
}

func addRuleSliceModuleRoots[T rule.InstRule](
	moduleRoots map[string]struct{},
	ruleModuleRoots map[rule.InstRule]string,
	rules []T,
) {
	for _, instRule := range rules {
		if moduleRoot, ok := ruleModuleRoots[instRule]; ok {
			moduleRoots[moduleRoot] = struct{}{}
		}
	}
}

func moduleRootForPackage(packageDir string) (string, error) {
	for dir := filepath.Clean(packageDir); ; dir = filepath.Dir(dir) {
		_, err := os.Stat(filepath.Join(dir, goModFileName))
		switch {
		case err == nil:
			return dir, nil
		case !errors.Is(err, os.ErrNotExist):
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ex.New("no parent go.mod found")
		}
	}
}

func selectedHookPackageDirs(matched []*rule.InstRuleSet) []string {
	seen := make(map[string]struct{})
	var packageDirs []string
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		packageDirs = append(packageDirs, path)
	}
	for _, ruleset := range matched {
		if ruleset == nil {
			continue
		}
		for _, fileRule := range ruleset.FileRules {
			add(fileRule.ResolvedPath)
		}
		for _, funcRule := range ruleset.AllFuncRules() {
			add(funcRule.ResolvedPath)
		}
	}
	slices.Sort(packageDirs)
	return packageDirs
}
