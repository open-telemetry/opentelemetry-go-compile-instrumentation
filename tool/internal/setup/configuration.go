// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package setup

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/configuration"
	"go.opentelemetry.io/otelc/tool/internal/rule"
)

const instrumentationMetadataFilename = "metadata.yaml"

// validateSelectedConfigurationMetadata resolves hook packages from the rules
// that matched this build, reads metadata.yaml once per owning module when it
// exists, then resolves and validates declarative definitions. Discovery does
// not select instrumentation: only paths already present in matched rules are
// considered.
func (sp *setupPhase) validateSelectedConfigurationMetadata(matched []*rule.InstRuleSet) error {
	packageDirs := selectedHookPackageDirs(matched)
	moduleRootsSet := make(map[string]struct{}, len(packageDirs))
	for _, packageDir := range packageDirs {
		moduleRoot, err := moduleRootForPackage(packageDir)
		if err != nil {
			return ex.Wrapf(err, "resolving module for selected instrumentation package %s", packageDir)
		}
		moduleRootsSet[moduleRoot] = struct{}{}
	}

	moduleRoots := make([]string, 0, len(moduleRootsSet))
	for moduleRoot := range moduleRootsSet {
		moduleRoots = append(moduleRoots, moduleRoot)
	}
	slices.Sort(moduleRoots)

	var metadataFiles []configuration.Metadata
	for _, moduleRoot := range moduleRoots {
		metadataPath := filepath.Join(moduleRoot, instrumentationMetadataFilename)
		data, err := os.ReadFile(metadataPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return ex.Wrapf(err, "read selected instrumentation metadata %s", metadataPath)
		}
		metadata, err := configuration.ParseMetadata(data, metadataPath)
		if err != nil {
			return err
		}
		metadataFiles = append(metadataFiles, metadata)
	}
	if len(metadataFiles) == 0 {
		return nil
	}

	definitions, err := configuration.CatalogDefinitions()
	if err != nil {
		return ex.Wrapf(err, "load pinned configuration catalog")
	}
	resolved, err := configuration.ResolveMetadata(metadataFiles, definitions)
	if err != nil {
		return err
	}
	sp.Info(
		"Validated selected instrumentation configuration metadata",
		"modules",
		len(metadataFiles),
		"configurations",
		len(resolved),
	)
	return nil
}

func moduleRootForPackage(packageDir string) (string, error) {
	for dir := filepath.Clean(packageDir); ; dir = filepath.Dir(dir) {
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
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
