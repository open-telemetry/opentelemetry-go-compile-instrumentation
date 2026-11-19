// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

func (sp *setupPhase) loadApplicationConfiguration() error {
	sp.applicationConfiguration = configuration.ApplicationConfiguration{}
	if sp.instrumentationConfigPath == "" {
		return nil
	}
	data, err := os.ReadFile(sp.instrumentationConfigPath)
	if err != nil {
		return ex.Wrapf(err, "read application configuration %s", sp.instrumentationConfigPath)
	}
	application, err := configuration.ParseApplicationConfiguration(data, sp.instrumentationConfigPath)
	if err != nil {
		return ex.Wrapf(err, "parse application configuration")
	}
	sp.applicationConfiguration = application
	return nil
}

func (sp *setupPhase) validateAndStoreApplicationConfiguration() error {
	sp.validatedApplicationConfig = configuration.ValidatedApplicationConfiguration{}
	if sp.instrumentationConfigPath == "" {
		return nil
	}
	if sp.applicationConfiguration.Document == nil {
		return ex.New("application configuration was not loaded before setup validation")
	}
	validated, err := configuration.ValidateApplicationOptions(
		sp.applicationConfiguration,
		sp.configurationMetadata.Definitions,
		sp.configurationMetadata.ModulesWithoutMetadata,
	)
	if err != nil {
		return ex.Wrapf(err, "validate application instrumentation configuration")
	}
	for _, warning := range validated.Warnings {
		_, _ = fmt.Fprintf(
			os.Stderr,
			"Warning: %s:%d:%d: %s (%s)\n",
			warning.Location.File,
			warning.Location.Line,
			warning.Location.Column,
			warning.Message,
			warning.Path,
		)
		sp.Warn(
			warning.Message,
			"configuration", warning.Path,
			"file", warning.Location.File,
			"line", warning.Location.Line,
		)
	}
	sp.validatedApplicationConfig = validated
	sp.Info("Validated application instrumentation configuration", "options", len(validated.Values))
	return nil
}

func (sp *setupPhase) selectedConfigurationModuleRoots(matched []*rule.InstRuleSet) []string {
	moduleRoots := make(map[string]struct{})
	for _, ruleset := range matched {
		if ruleset == nil {
			continue
		}
		for instRule := range ruleset.AllRules() {
			if moduleRoot, ok := sp.ruleModuleRoots[instRule]; ok {
				moduleRoots[moduleRoot] = struct{}{}
			}
		}
	}
	return slices.Sorted(maps.Keys(moduleRoots))
}

// moduleRootForPackage returns the root directory of the module that owns the
// package in packageDir. In vendor/, go mod vendor does not copy dependency
// go.mod files, so the owning module comes from vendor/modules.txt.
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
	rel, err := filepath.Rel(vendorDir, packageDir)
	if err != nil {
		return "", ex.Wrapf(err, "locating package %s in %s", packageDir, vendorDir)
	}
	pkgPath := filepath.ToSlash(rel)
	modulePath := ""
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if moduleLine, ok := strings.CutPrefix(line, "# "); ok {
			fields := strings.Fields(moduleLine)
			if len(fields) > 0 {
				modulePath = fields[0]
			}
			continue
		}
		if line == pkgPath {
			return filepath.Join(vendorDir, filepath.FromSlash(modulePath)), nil
		}
	}
	return "", ex.Newf("no module of vendor/modules.txt owns package %s", packageDir)
}
