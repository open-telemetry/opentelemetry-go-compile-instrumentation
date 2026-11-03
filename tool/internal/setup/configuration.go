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

// validateSelectedConfigurationMetadata reads metadata.yaml only for modules
// whose rules matched this build. Missing metadata is returned to the caller so
// application-configuration validation can decide whether it is needed.
func (sp *setupPhase) validateSelectedConfigurationMetadata(
	matched []*rule.InstRuleSet,
) (selectedConfigurationMetadata, error) {
	moduleMetadata := make(map[string]string)
	for _, ruleset := range matched {
		if ruleset == nil {
			continue
		}
		maps.Copy(moduleMetadata, ruleset.ConfigurationModuleMetadata())
	}

	moduleRoots := slices.Sorted(maps.Keys(moduleMetadata))
	result := selectedConfigurationMetadata{}
	metadataPaths := make(map[string]struct{})
	for _, moduleRoot := range moduleRoots {
		metadataPath := moduleMetadata[moduleRoot]
		if metadataPath == "" {
			result.ModulesWithoutMetadata = append(result.ModulesWithoutMetadata, moduleRoot)
			continue
		}
		metadataPaths[metadataPath] = struct{}{}
	}

	var metadataFiles []configuration.Metadata
	for _, metadataPath := range slices.Sorted(maps.Keys(metadataPaths)) {
		data, err := os.ReadFile(metadataPath)
		if errors.Is(err, os.ErrNotExist) {
			// The file may have disappeared after rule loading. Preserve the module
			// roots that referenced it as missing so the later config validator can
			// report the actionable error in context.
			for _, moduleRoot := range moduleRoots {
				if moduleMetadata[moduleRoot] == metadataPath {
					result.ModulesWithoutMetadata = append(result.ModulesWithoutMetadata, moduleRoot)
				}
			}
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
			len(moduleRoots),
			"configurations",
			0,
		)
		return result, nil
	}

	definitions, err := configuration.CatalogDefinitions()
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
		len(moduleRoots)-len(result.ModulesWithoutMetadata),
		"configurations",
		len(resolved),
	)
	return result, nil
}

// moduleRootForPackage returns the root directory of the module that owns the
// package in packageDir. In vendor/, "go mod vendor" does not copy the go.mod
// files of the dependencies, so the search must not leave vendor/: this would
// find the module of the application. For a package in vendor/, the owning
// module comes from vendor/modules.txt.
func moduleRootForPackage(packageDir string) (string, error) {
	for dir := filepath.Clean(packageDir); ; dir = filepath.Dir(dir) {
		_, err := os.Stat(filepath.Join(dir, goModFileName))
		switch {
		case err == nil:
			return dir, nil
		case !errors.Is(err, os.ErrNotExist):
			return "", err
		}
		if filepath.Base(dir) == vendorDirName {
			return vendorOwnerModuleRoot(dir, packageDir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ex.New("no parent go.mod found")
		}
	}
}

// vendorOwnerModuleRoot returns the directory of the module that owns the
// package in packageDir, using the module that lists it in vendor/modules.txt.
func vendorOwnerModuleRoot(vendorDir, packageDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(vendorDir, "modules.txt"))
	if err != nil {
		return "", ex.Wrapf(err, "reading the vendor/modules.txt that has package %s", packageDir)
	}
	modules, err := parseModulesTxt(data)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(vendorDir, packageDir)
	if err != nil {
		return "", ex.Wrapf(err, "locating package %s in %s", packageDir, vendorDir)
	}
	pkgPath := filepath.ToSlash(rel)
	// The module that vendors the package is the one that lists it in
	// modules.txt. A nested module without this package is not the owner.
	owner := modules.moduleOfPackage(pkgPath)
	if owner == nil {
		return "", ex.Newf("no module of vendor/modules.txt owns package %s", packageDir)
	}
	return filepath.Join(vendorDir, filepath.FromSlash(owner.Path)), nil
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
