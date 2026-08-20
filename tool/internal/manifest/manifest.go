// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"

	"go.opentelemetry.io/otelc/tool/data"
	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/util"
)

// Entry describes a distinct instrumentation module, target package, and
// minimum version bound.
type Entry struct {
	ModulePath string `json:"modulePath"`
	Target     string `json:"target"`
	// VersionRange is a minimum version bound. Empty means all versions.
	VersionRange string `json:"versionRange,omitempty"`
}

type Manifest []Entry

type yamlRule struct {
	Target       string `yaml:"target"`
	VersionRange string `yaml:"version"`
}

func Generate(instrumentationRoot string) (Manifest, error) {
	manifest := make(Manifest, 0)
	err := filepath.WalkDir(instrumentationRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "go.mod" || filepath.Dir(path) == instrumentationRoot {
			return nil
		}

		modulePath, parseErr := loadModulePath(path)
		if parseErr != nil {
			return parseErr
		}
		entries, loadErr := loadModuleEntries(filepath.Dir(path), modulePath)
		if loadErr != nil {
			return loadErr
		}
		manifest = append(manifest, entries...)
		return nil
	})
	if err != nil {
		return nil, ex.Wrapf(err, "generating manifest from %s", instrumentationRoot)
	}

	slices.SortFunc(manifest, func(a, b Entry) int {
		if cmp := strings.Compare(a.ModulePath, b.ModulePath); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(a.Target, b.Target); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.VersionRange, b.VersionRange)
	})
	manifest = slices.Compact(manifest)
	return manifest, nil
}

func Load() (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data.GetManifestJSON(), &manifest); err != nil {
		return nil, ex.Wrapf(err, "loading embedded instrumentation manifest")
	}
	return manifest, nil
}

func loadModulePath(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", ex.Wrapf(err, "reading %s", path)
	}
	file, err := modfile.Parse(path, content, nil)
	if err != nil {
		return "", ex.Wrapf(err, "parsing %s", path)
	}
	if file.Module == nil || file.Module.Mod.Path == "" {
		return "", ex.Newf("%s has no module directive", path)
	}
	return file.Module.Mod.Path, nil
}

func loadModuleEntries(moduleDir, modulePath string) (Manifest, error) {
	entries := make(Manifest, 0)
	root, err := os.OpenRoot(moduleDir)
	if err != nil {
		return nil, ex.Wrapf(err, "opening module root %s", moduleDir)
	}
	defer root.Close()

	rootFS := root.FS()
	err = fs.WalkDir(rootFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." {
				if _, statErr := fs.Stat(rootFS, path+"/go.mod"); statErr == nil {
					return fs.SkipDir
				} else if !errors.Is(statErr, fs.ErrNotExist) {
					return statErr
				}
			}
			return nil
		}
		if !util.IsRuleFile(d.Name()) {
			return nil
		}

		content, readErr := fs.ReadFile(rootFS, path)
		if readErr != nil {
			return ex.Wrapf(readErr, "reading rule file %s", path)
		}
		var rules map[string]yamlRule
		if unmarshalErr := yaml.Unmarshal(content, &rules); unmarshalErr != nil {
			return ex.Wrapf(unmarshalErr, "parsing rule file %s", path)
		}
		for _, rule := range rules {
			if rule.Target == "" {
				continue
			}
			entries = append(entries, Entry{
				ModulePath:   modulePath,
				Target:       rule.Target,
				VersionRange: rule.VersionRange,
			})
		}
		return nil
	})
	if err != nil {
		return nil, ex.Wrapf(err, "loading rules for module %s", modulePath)
	}
	return entries, nil
}
