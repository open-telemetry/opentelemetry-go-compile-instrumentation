// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"go.opentelemetry.io/otelc/tool/ex"
	"go.opentelemetry.io/otelc/tool/internal/manifest"
	"go.opentelemetry.io/otelc/tool/internal/rule"
	"go.opentelemetry.io/otelc/tool/util"
)

type (
	Entry    = manifest.Entry
	Manifest = manifest.Manifest
)

type yamlRule struct {
	Target       rule.Target `yaml:"target"`
	VersionRange string      `yaml:"version"`
}

func Generate(instrumentationRoot string, version ...string) (Manifest, error) {
	if resolved, evalErr := filepath.EvalSymlinks(instrumentationRoot); evalErr == nil {
		instrumentationRoot = resolved
	}
	var ver string
	if len(version) > 0 {
		ver = version[0]
	}
	repoRoot, tags := loadCommitTags(instrumentationRoot)

	manifest := make(Manifest, 0)
	err := filepath.WalkDir(instrumentationRoot, func(entryPath string, d os.DirEntry, err error) error {
		if err != nil {
			return ex.Wrap(err)
		}
		if d.IsDir() || d.Name() != "go.mod" || filepath.Dir(entryPath) == instrumentationRoot {
			return nil
		}

		modulePath, parseErr := loadModulePath(entryPath)
		if parseErr != nil {
			return parseErr
		}

		modDir := filepath.Dir(entryPath)
		modVer := ver
		if repoRoot != "" {
			if rel, relErr := filepath.Rel(repoRoot, modDir); relErr == nil {
				cleanRel := path.Clean(filepath.ToSlash(rel))
				if tagVer, ok := tags[cleanRel]; ok && tagVer != "" {
					modVer = tagVer
				}
			}
		}

		entries, loadErr := loadModuleEntries(modDir, modulePath, modVer)
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
		if cmp := strings.Compare(a.VersionRange, b.VersionRange); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Version, b.Version)
	})
	manifest = slices.Compact(manifest)
	return manifest, nil
}

//nolint:revive // multiple string return types are clear in context
func parseTag(tag string) (string, string, bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", "", false
	}
	idx := strings.LastIndex(tag, "/")
	if idx == -1 {
		if semver.IsValid(tag) {
			return ".", tag, true
		}
		return "", "", false
	}
	dir := tag[:idx]
	ver := tag[idx+1:]
	if semver.IsValid(ver) {
		return path.Clean(dir), ver, true
	}
	return "", "", false
}

func loadCommitTags(dir string) (string, map[string]string) {
	tags := make(map[string]string)
	if resolved, evalErr := filepath.EvalSymlinks(dir); evalErr == nil {
		dir = resolved
	}
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", tags
	}
	repoRoot := filepath.Clean(strings.TrimSpace(string(out)))
	if resolved, evalErr := filepath.EvalSymlinks(repoRoot); evalErr == nil {
		repoRoot = resolved
	}

	cmd = exec.CommandContext(context.Background(), "git", "-C", dir, "tag", "--points-at", "HEAD")
	out, err = cmd.Output()
	if err != nil {
		return repoRoot, tags
	}

	for line := range strings.SplitSeq(string(out), "\n") {
		subDir, ver, ok := parseTag(line)
		if !ok {
			continue
		}
		if existing, exists := tags[subDir]; !exists || semver.Compare(ver, existing) > 0 {
			tags[subDir] = ver
		}
	}
	return repoRoot, tags
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

func loadModuleEntries(moduleDir, modulePath string, version ...string) (Manifest, error) {
	var ver string
	if len(version) > 0 {
		ver = version[0]
	}
	entries := make(Manifest, 0)
	root, err := os.OpenRoot(moduleDir)
	if err != nil {
		return nil, ex.Wrapf(err, "opening module root %s", moduleDir)
	}
	defer root.Close()

	rootFS := root.FS()
	err = fs.WalkDir(rootFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return ex.Wrap(err)
		}
		if d.IsDir() {
			if path == "." {
				return nil
			}
			if _, statErr := fs.Stat(rootFS, path+"/go.mod"); statErr == nil {
				return fs.SkipDir
			} else if !errors.Is(statErr, fs.ErrNotExist) {
				return ex.Wrapf(statErr, "stat %s/go.mod", path)
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
		ruleEntries, parseErr := parseRuleEntries(content, path, modulePath, ver)
		if parseErr != nil {
			return parseErr
		}
		entries = append(entries, ruleEntries...)
		return nil
	})
	if err != nil {
		return nil, ex.Wrapf(err, "loading rules for module %s", modulePath)
	}
	return entries, nil
}

func parseRuleEntries(content []byte, path, modulePath string, version ...string) (Manifest, error) {
	var ver string
	if len(version) > 0 {
		ver = version[0]
	}
	return parseRuleEntriesForVersion(content, path, modulePath, util.Version, ver)
}

func parseRuleEntriesForVersion(
	content []byte,
	path, modulePath, currentVersion string,
	version ...string,
) (Manifest, error) {
	var ver string
	if len(version) > 0 {
		ver = version[0]
	}
	doc, err := rule.ParseFile(content)
	if err != nil {
		return nil, ex.Wrapf(err, "parsing rule file %s", path)
	}
	if err = rule.CheckVersion(currentVersion, doc.MinimumVersion); err != nil {
		return nil, ex.Wrapf(err, "validating minimum otelc version in rule file %s", path)
	}

	entries := make(Manifest, 0, len(doc.Entries))
	for _, entry := range doc.Entries {
		var ruleConfig yamlRule
		if decodeErr := entry.Node.Decode(&ruleConfig); decodeErr != nil {
			return nil, ex.Wrapf(decodeErr, "parsing rule %q in %s", entry.Name, path)
		}
		if validateErr := util.ValidateVersionRange(ruleConfig.VersionRange); validateErr != nil {
			return nil, ex.Wrapf(validateErr, "validating version for rule %q in file %s", entry.Name, path)
		}
		if ruleConfig.Target.IsZero() {
			continue
		}
		if validateErr := ruleConfig.Target.Validate(); validateErr != nil {
			return nil, ex.Wrapf(validateErr, "validating target for rule %q in file %s", entry.Name, path)
		}
		// One entry per included pattern. Excluded patterns only narrow what a
		// rule applies to, so the manifest may list a package a rule skips.
		for _, target := range ruleConfig.Target.Include {
			entries = append(entries, Entry{
				ModulePath:   modulePath,
				Target:       target,
				VersionRange: ruleConfig.VersionRange,
				Version:      ver,
			})
		}
	}
	return entries, nil
}
